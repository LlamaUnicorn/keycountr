package main

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"keycountr/internal/counter"
	"keycountr/internal/storage"
)

func TestDailyStatusAcrossMidnightAndRetries(t *testing.T) {
	for _, tc := range []struct {
		zone  string
		month time.Month
		day   int
	}{
		{"Asia/Riyadh", time.October, 16},
		{"America/New_York", time.March, 8},
		{"America/New_York", time.November, 1},
	} {
		t.Run(tc.zone+tc.month.String(), func(t *testing.T) {
			loc, err := time.LoadLocation(tc.zone)
			if err != nil {
				t.Fatal(err)
			}
			original := time.Local
			time.Local = loc
			t.Cleanup(func() { time.Local = original })
			today := time.Date(2026, tc.month, tc.day, 0, 0, 0, 0, loc)
			before := today.AddDate(0, 0, 1).Add(-100 * time.Millisecond)
			after := before.Add(200 * time.Millisecond)
			dir := t.TempDir()
			if err := os.Chmod(dir, 0700); err != nil {
				t.Fatal(err)
			}
			ctx := context.Background()
			store, err := storage.Open(ctx, dir)
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			if got := dailyStatus(store, before, nil, nil, "").Daily; got != "today=0 previous=none" {
				t.Fatalf("empty history: %s", got)
			}
			if err := store.Apply(ctx, "history", []counter.Minute{
				{Start: today.AddDate(0, 0, -2).Unix(), Count: 99},
				{Start: today.AddDate(0, 0, -1).Unix(), Count: 7},
				{Start: today.Unix(), Count: 4},
			}); err != nil {
				t.Fatal(err)
			}
			buffer := counter.New(before)
			if err := buffer.Observe(before, 2, true, 0); err != nil {
				t.Fatal(err)
			}
			var batch []counter.Minute
			check := func(now time.Time, want string) {
				t.Helper()
				display := dailyStatus(store, now, buffer.Rows(), batch, "retry")
				if got := display.Daily; got != want {
					t.Fatalf("at %s: %s, want %s", now, got, want)
				}
				wantAverage := "avg7=53.0(2d)"
				if now.After(before) {
					wantAverage = "avg7=37.3(3d)"
				}
				if now.After(after) {
					wantAverage = "avg7=28.8(4d)"
				}
				if want == "today=? previous=?" {
					wantAverage = "avg7=?"
				}
				if display.Average != wantAverage {
					t.Fatalf("average: %s, want %s", display.Average, wantAverage)
				}
			}
			wantBefore := fmt.Sprintf("today=6 previous=7(%s)", today.AddDate(0, 0, -1).Format("2006-01-02"))
			wantAfter := fmt.Sprintf("today=3 previous=6(%s)", today.Format("2006-01-02"))
			check(before, wantBefore)
			batch = buffer.Rows()
			check(before, wantBefore)
			// A successful commit whose acknowledgement was lost must not
			// duplicate the batch in live status while it awaits retry.
			if err := store.Apply(ctx, "retry", batch); err != nil {
				t.Fatal(err)
			}
			check(before, wantBefore)
			if err := buffer.Observe(after, 3, true, 0); err != nil {
				t.Fatal(err)
			}
			check(after, wantAfter)
			if err := store.Apply(ctx, "retry", batch); err != nil {
				t.Fatal(err)
			}
			buffer.Acknowledge(batch)
			batch = nil
			check(after, wantAfter)
			rows := buffer.Rows()
			if err := store.Apply(ctx, "next", rows); err != nil {
				t.Fatal(err)
			}
			buffer.Acknowledge(rows)
			check(after, wantAfter)
			check(before, wantBefore)
			check(after.AddDate(0, 0, 1), fmt.Sprintf("today=0 previous=3(%s)", after.Format("2006-01-02")))
			store.Close()
			check(after, "today=? previous=?")
		})
	}
}

func TestDailyStatusSkipsDaysWithoutPresses(t *testing.T) {
	loc, err := time.LoadLocation("Asia/Riyadh")
	if err != nil {
		t.Fatal(err)
	}
	original := time.Local
	time.Local = loc
	t.Cleanup(func() { time.Local = original })
	monday := time.Date(2026, 10, 19, 0, 0, 0, 0, loc)
	friday := monday.AddDate(0, 0, -3)
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	store, err := storage.Open(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.Apply(ctx, "history", []counter.Minute{
		{Start: friday.AddDate(0, 0, -1).Unix(), Count: 999},
		{Start: friday.Unix(), Count: 10},
		{Start: friday.Add(time.Hour).Unix(), Count: 20},
		{Start: monday.AddDate(0, 0, -1).Unix(), RunningMS: 60000, HealthyMS: 60000},
		{Start: monday.Unix(), Count: 5},
		{Start: monday.AddDate(0, 0, 1).Unix(), Count: 100},
	}); err != nil {
		t.Fatal(err)
	}
	check := func(pending, batch []counter.Minute, token, want string) {
		t.Helper()
		if got := dailyStatus(store, monday.Add(time.Hour), pending, batch, token).Daily; got != want {
			t.Fatalf("%s, want %s", got, want)
		}
	}
	check(nil, nil, "", "today=5 previous=30(2026-10-16)")
	// An earlier day still waiting to flush must supersede older saved history.
	batch := []counter.Minute{{Start: monday.AddDate(0, 0, -2).Unix(), Count: 8}}
	pending := append([]counter.Minute{{Start: monday.Unix(), Count: 2}}, batch...)
	check(pending, batch, "retry", "today=7 previous=8(2026-10-17)")
	if err := store.Apply(ctx, "retry", batch); err != nil {
		t.Fatal(err)
	}
	check(pending, batch, "retry", "today=7 previous=8(2026-10-17)")
	check(nil, nil, "", "today=5 previous=8(2026-10-17)")
}
