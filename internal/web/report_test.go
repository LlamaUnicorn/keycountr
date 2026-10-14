package web

import (
	"testing"
	"time"

	"keycountr/internal/counter"
	"keycountr/internal/storage"
)

func query(t *testing.T, from, to, zone, unit string) Query {
	t.Helper()
	q, err := ParseQuery(from, to, zone, unit, time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	return q
}

func TestDSTHours(t *testing.T) {
	for _, tc := range []struct {
		date, end string
		hours     int
	}{{"2026-03-08", "2026-03-09", 23}, {"2026-11-01", "2026-11-02", 25}} {
		q := query(t, tc.date, tc.end, "America/New_York", "hour")
		var s storage.Snapshot
		for m := q.Start.Unix(); m < q.End.Unix(); m += 60 {
			s.Minutes = append(s.Minutes, counter.Minute{Start: m, Count: 1, RunningMS: 60000, HealthyMS: 60000})
		}
		report := Aggregate(q, s, q.End)
		if len(report.Buckets) != tc.hours || report.Total != uint64(tc.hours*60) {
			t.Fatalf("DST report: %+v", report)
		}
		for _, b := range report.Buckets {
			if b.Status != "covered" || b.Count != 60 {
				t.Fatalf("bad hour: %+v", b)
			}
		}
		if tc.hours == 25 && (report.Buckets[1].Label == report.Buckets[2].Label || report.Buckets[1].Start.Equal(report.Buckets[2].Start)) {
			t.Fatal("repeated hours ambiguous")
		}
	}
}

func TestHalfHourDSTAndSkippedDate(t *testing.T) {
	q := query(t, "2026-10-04", "2026-10-05", "Australia/Lord_Howe", "hour")
	r := Aggregate(q, storage.Snapshot{}, q.End)
	var elapsed int64
	for _, b := range r.Buckets {
		elapsed += b.ExpectedMS
	}
	if elapsed != int64((23*time.Hour+30*time.Minute)/time.Millisecond) {
		t.Fatal(elapsed)
	}
	// This crosses a skipped calendar day and must finish without AddDate looping.
	q = query(t, "2011-12-29", "2011-12-31", "Pacific/Apia", "day")
	r = Aggregate(q, storage.Snapshot{}, q.End)
	if len(r.Buckets) != 1 {
		t.Fatal(r.Buckets)
	}
	if _, err := ParseQuery("2011-12-30", "2011-12-31", "Pacific/Apia", "day", q.End); err == nil {
		t.Fatal("nonexistent date accepted")
	}
}

func TestRangeTimezoneWeekAndCoverage(t *testing.T) {
	q := query(t, "2025-12-28", "2026-01-06", "UTC", "week")
	start := q.Start.Unix()
	s := storage.Snapshot{Total: 99, Minutes: []counter.Minute{
		{Start: start - 60, Count: 30}, {Start: start, Count: 2, RunningMS: 60000, HealthyMS: 60000},
		{Start: start + 60, Count: 3, RunningMS: 10000, Issues: 1}, {Start: q.End.Unix() - 60, Count: 5}, {Start: q.End.Unix(), Count: 40},
	}}
	r := Aggregate(q, s, q.End)
	if len(r.Buckets) != 3 || r.Total != 10 || r.LifetimeTotal != 99 || r.Buckets[0].Label != "Week of 2025-12-22" || r.Buckets[1].Label != "Week of 2025-12-29" || r.Buckets[2].Label != "Week of 2026-01-05" {
		t.Fatalf("wrong report: %+v", r)
	}
	if r.Buckets[0].Status != "partial coverage" || r.Buckets[0].Issues != 1 {
		t.Fatal(r.Buckets[0])
	}
	instant := time.Date(2026, 1, 1, 23, 0, 0, 0, time.UTC).Unix()
	for _, zone := range []string{"UTC", "Asia/Tokyo"} {
		q := query(t, "2026-01-01", "2026-01-03", zone, "day")
		r := Aggregate(q, storage.Snapshot{Minutes: []counter.Minute{{Start: instant, Count: 7}}}, q.End)
		idx := 0
		if zone == "Asia/Tokyo" {
			idx = 1
		}
		if r.Buckets[idx].Count != 7 || r.Total != 7 {
			t.Fatal(r)
		}
	}
}

func TestEmptyZeroImpairedAndCurrentMinute(t *testing.T) {
	q := query(t, "2026-10-14", "2026-10-16", "UTC", "day")
	var s storage.Snapshot
	for m := q.Start.Unix(); m < q.Start.AddDate(0, 0, 1).Unix(); m += 60 {
		s.Minutes = append(s.Minutes, counter.Minute{Start: m, RunningMS: 60000, HealthyMS: 60000})
	}
	r := Aggregate(q, s, q.End)
	if r.Total != 0 || r.Buckets[0].Status != "covered" || r.Buckets[1].Status != "no saved coverage" {
		t.Fatal(r)
	}
	s.Minutes[0].HealthyMS = 0
	s.Minutes[0].Issues = 2
	r = Aggregate(q, s, q.End)
	if r.Buckets[0].Status != "impaired coverage" {
		t.Fatal(r)
	}
	now := q.Start.Add(30 * time.Second)
	r = Aggregate(q, storage.Snapshot{Minutes: []counter.Minute{{Start: q.Start.Unix(), Count: 4, RunningMS: 25000, HealthyMS: 25000}}}, now)
	if r.Total != 4 || r.Buckets[0].ExpectedMS != 30000 || r.Buckets[0].Status != "partial coverage" || r.Buckets[1].Status != "future" {
		t.Fatal(r)
	}
}

func TestQueryValidation(t *testing.T) {
	for _, tc := range [][4]string{
		{"2026-01-01", "2026-01-01", "UTC", "day"}, {"bad", "2026-01-02", "UTC", "day"},
		{"2026-01-01", "2026-01-02", "no-such-zone", "day"}, {"2026-01-01", "2026-01-02", "UTC", "minute"},
		{"2026-01-01", "2026-02-02", "UTC", "hour"}, {"2025-01-01", "2026-01-03", "UTC", "day"},
	} {
		if _, err := ParseQuery(tc[0], tc[1], tc[2], tc[3], time.Now()); err == nil {
			t.Fatal(tc)
		}
	}
}
