package storage

import (
	"context"
	"testing"
	"time"

	"keycountr/internal/counter"
)

func TestActivityLastSevenRecordedDays(t *testing.T) {
	for _, zone := range []string{"UTC", "Asia/Riyadh", "America/New_York"} {
		t.Run(zone, func(t *testing.T) {
			loc, err := time.LoadLocation(zone)
			if err != nil {
				t.Fatal(err)
			}
			today := time.Date(2026, 11, 2, 0, 0, 0, 0, loc)
			store := openTest(t, t.TempDir())
			ctx := context.Background()
			var rows []counter.Minute
			for i := 1; i <= 9; i++ {
				day := today.AddDate(0, 0, -2*i)
				rows = append(rows, counter.Minute{Start: day.Unix(), Count: int64(10*i - 1)}, counter.Minute{Start: day.Add(time.Minute).Unix(), Count: 1})
			}
			rows = append(rows,
				counter.Minute{Start: today.AddDate(0, 0, -1).Unix(), RunningMS: 60000},
				counter.Minute{Start: today.Unix(), Count: 99},
				counter.Minute{Start: today.AddDate(0, 0, 1).Unix(), Count: 1000})
			if err := store.Apply(ctx, "history", rows); err != nil {
				t.Fatal(err)
			}
			stats, err := store.Activity(ctx, today, nil, nil, "")
			if err != nil || stats.Today != 99 || stats.Previous == nil || stats.Previous.Count != 10 || stats.Previous.Date != today.AddDate(0, 0, -2).Format("2006-01-02") || stats.Days != 7 || stats.Average == nil || *stats.Average != 40 {
				t.Fatalf("stats: %+v, %v", stats, err)
			}
			pending := []counter.Minute{{Start: today.AddDate(0, 0, -1).Unix(), Count: 6}, {Start: today.Unix(), Count: 1}}
			check := func() {
				t.Helper()
				stats, err := store.Activity(ctx, today, pending, pending, "retry")
				if err != nil || stats.Today != 100 || stats.Previous == nil || stats.Previous.Count != 6 || stats.Days != 7 || stats.Average == nil || *stats.Average != float64(216)/7 {
					t.Fatalf("pending stats: %+v, %v", stats, err)
				}
			}
			check()
			if err := store.Apply(ctx, "retry", pending); err != nil {
				t.Fatal(err)
			}
			check()
		})
	}
}
