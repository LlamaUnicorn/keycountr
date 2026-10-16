package storage

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"keycountr/internal/counter"
)

type RecordedDay struct {
	Date  string `json:"date"`
	Count uint64 `json:"count"`
}

type ActivityStats struct {
	Today    uint64       `json:"today"`
	Previous *RecordedDay `json:"previous"`
	Average  *float64     `json:"average"`
	Days     int          `json:"days"`
}

func calendarDay(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
}

// Activity includes pending counts, removing an already committed retry batch.
func (s *Store) Activity(ctx context.Context, now time.Time, pending, batch []counter.Minute, batchID string) (ActivityStats, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return ActivityStats{}, errors.New("cannot read activity statistics")
	}
	defer tx.Rollback()
	var receipt string
	if err := tx.QueryRowContext(ctx, `SELECT coalesce((SELECT last_batch FROM collector_state WHERE singleton = 1), '')`).Scan(&receipt); err != nil {
		return ActivityStats{}, errors.New("cannot read activity receipt")
	}
	deltas := make(map[int64]int64)
	for _, row := range pending {
		deltas[row.Start] += row.Count
	}
	if batchID != "" && receipt == batchID {
		for _, row := range batch {
			deltas[row.Start] -= row.Count
		}
	}
	return readActivity(ctx, tx, now, deltas)
}

// readActivity shares the caller's transaction, keeping dashboard totals and
// comparisons consistent. Only seven completed days with presses are retained.
func readActivity(ctx context.Context, tx *sql.Tx, now time.Time, deltas map[int64]int64) (ActivityStats, error) {
	var out ActivityStats
	today := calendarDay(now)
	pendingDays := make(map[int64]uint64)
	for minute, count := range deltas {
		if count > 0 {
			day := calendarDay(time.Unix(minute, 0).In(now.Location()))
			pendingDays[day.Unix()] += uint64(count)
		}
	}
	countDay := func(day time.Time) (uint64, error) {
		var count uint64
		err := tx.QueryRowContext(ctx, `SELECT coalesce(sum(count), 0) FROM minute_counts WHERE bucket_start >= ? AND bucket_start < ?`, day.Unix(), day.AddDate(0, 0, 1).Unix()).Scan(&count)
		return count + pendingDays[day.Unix()], err
	}
	var err error
	if out.Today, err = countDay(today); err != nil {
		return out, errors.New("cannot read today's count")
	}
	cursor := today
	var total uint64
	for out.Days < 7 {
		var latest sql.NullInt64
		if err := tx.QueryRowContext(ctx, `SELECT max(bucket_start) FROM minute_counts WHERE bucket_start < ? AND count > 0`, cursor.Unix()).Scan(&latest); err != nil {
			return out, errors.New("cannot find recorded day")
		}
		var selected *time.Time
		if latest.Valid {
			day := calendarDay(time.Unix(latest.Int64, 0).In(now.Location()))
			selected = &day
		}
		for start := range pendingDays {
			day := time.Unix(start, 0).In(now.Location())
			if day.Before(cursor) && (selected == nil || day.After(*selected)) {
				selected = &day
			}
		}
		if selected == nil {
			break
		}
		count, err := countDay(*selected)
		if err != nil {
			return out, errors.New("cannot read recorded daily count")
		}
		if out.Previous == nil {
			out.Previous = &RecordedDay{Date: selected.Format("2006-01-02"), Count: count}
		}
		total += count
		out.Days++
		cursor = *selected
	}
	if out.Days > 0 {
		average := float64(total) / float64(out.Days)
		out.Average = &average
	}
	return out, nil
}
