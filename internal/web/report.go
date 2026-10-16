// Package web serves local aggregate reports without accessing input devices.
package web

import (
	"errors"
	"time"

	"keycountr/internal/storage"
)

type Query struct {
	Start, End  time.Time
	Location    *time.Location
	Granularity string
}

func ParseQuery(start, end, zone, granularity string, now time.Time) (Query, error) {
	loc := time.Local
	var err error
	if zone != "" {
		loc, err = time.LoadLocation(zone)
		if err != nil {
			return Query{}, errors.New("unknown timezone")
		}
	}
	today := now.In(loc)
	midnight := time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, loc)
	if start == "" {
		start = midnight.AddDate(0, 0, -6).Format("2006-01-02")
	}
	if end == "" {
		end = midnight.AddDate(0, 0, 1).Format("2006-01-02")
	}
	from, e1 := time.ParseInLocation("2006-01-02", start, loc)
	to, e2 := time.ParseInLocation("2006-01-02", end, loc)
	if e1 != nil || e2 != nil || from.Format("2006-01-02") != start || to.Format("2006-01-02") != end || !from.Before(to) {
		return Query{}, errors.New("use YYYY-MM-DD dates with start before exclusive end")
	}
	if granularity == "" {
		granularity = "day"
	}
	if granularity != "hour" && granularity != "day" && granularity != "week" {
		return Query{}, errors.New("granularity must be hour, day or week")
	}
	// Minute storage cannot resolve historical timezone offsets with seconds.
	if from.Unix()%60 != 0 || to.Unix()%60 != 0 || from.Year() < 1970 || to.Year() > 2100 {
		return Query{}, errors.New("supported dates are 1970–2099 with minute-aligned timezone boundaries")
	}
	limit := 367 * 24 * time.Hour
	if granularity == "hour" {
		limit = 32 * 24 * time.Hour
	}
	if to.Sub(from) > limit {
		return Query{}, errors.New("range limit: 31 calendar days for hours, 366 for days or weeks")
	}
	// Calendar limit, independent of DST elapsed duration.
	calendarStart, _ := time.Parse("2006-01-02", start)
	calendarEnd, _ := time.Parse("2006-01-02", end)
	days := int(calendarEnd.Sub(calendarStart) / (24 * time.Hour))
	if days > 366 || (granularity == "hour" && days > 31) {
		return Query{}, errors.New("range limit: 31 calendar days for hours, 366 for days or weeks")
	}
	return Query{from, to, loc, granularity}, nil
}

type Bucket struct {
	Start      time.Time `json:"start"`
	End        time.Time `json:"end"`
	Label      string    `json:"label"`
	Count      uint64    `json:"count"`
	ExpectedMS int64     `json:"expected_ms"`
	RunningMS  int64     `json:"running_ms"`
	HealthyMS  int64     `json:"healthy_ms"`
	Issues     int64     `json:"issues"`
	Status     string    `json:"status"`
}

type Report struct {
	Start         time.Time             `json:"start"`
	End           time.Time             `json:"end"`
	Timezone      string                `json:"timezone"`
	Granularity   string                `json:"granularity"`
	RefreshedAt   time.Time             `json:"refreshed_at"`
	LatestMinute  *time.Time            `json:"latest_saved_minute"`
	Total         uint64                `json:"total"`
	LifetimeTotal uint64                `json:"lifetime_total"`
	Buckets       []Bucket              `json:"buckets"`
	Activity      storage.ActivityStats `json:"activity"`
}

func label(t time.Time, q Query) string {
	local := t.In(q.Location)
	switch q.Granularity {
	case "hour":
		return local.Format("2006-01-02 15:00 -07:00")
	case "week":
		monday := local.AddDate(0, 0, -(int(local.Weekday())+6)%7)
		return "Week of " + monday.Format("2006-01-02")
	default:
		return local.Format("2006-01-02")
	}
}

func Aggregate(q Query, snapshot storage.Snapshot, now time.Time) Report {
	out := Report{Start: q.Start, End: q.End, Timezone: q.Location.String(), Granularity: q.Granularity, RefreshedAt: now, LifetimeTotal: snapshot.Total, Buckets: []Bucket{}}
	out.Activity = snapshot.Activity
	if snapshot.LatestMinute != nil {
		t := time.Unix(*snapshot.LatestMinute, 0).In(q.Location)
		out.LatestMinute = &t
	}
	index := 0
	// Walk UTC minutes so skipped/repeated hours and even half-hour DST changes
	// follow actual elapsed time, while labels follow the reporting calendar.
	for t := q.Start; t.Before(q.End); t = t.Add(time.Minute) {
		name := label(t, q)
		if len(out.Buckets) == 0 || out.Buckets[len(out.Buckets)-1].Label != name {
			out.Buckets = append(out.Buckets, Bucket{Start: t, Label: name})
		}
		b := &out.Buckets[len(out.Buckets)-1]
		b.End = t.Add(time.Minute)
		expected := max(int64(0), min(int64(60000), now.Sub(t).Milliseconds()))
		b.ExpectedMS += expected
		for index < len(snapshot.Minutes) && snapshot.Minutes[index].Start < t.Unix() {
			index++
		}
		if index < len(snapshot.Minutes) && snapshot.Minutes[index].Start == t.Unix() {
			row := snapshot.Minutes[index]
			b.Count += uint64(row.Count)
			out.Total += uint64(row.Count)
			b.RunningMS += min(expected, row.RunningMS)
			b.HealthyMS += min(expected, row.HealthyMS)
			b.Issues |= row.Issues
		}
	}
	for i := range out.Buckets {
		b := &out.Buckets[i]
		switch {
		case b.ExpectedMS == 0:
			b.Status = "future"
		case b.RunningMS == 0:
			b.Status = "no saved coverage"
		case b.RunningMS < b.ExpectedMS:
			b.Status = "partial coverage"
		case b.HealthyMS < b.ExpectedMS || b.Issues != 0:
			b.Status = "impaired coverage"
		default:
			b.Status = "covered"
		}
	}
	return out
}
