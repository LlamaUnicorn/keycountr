package main

import (
	"context"
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
			if got := dailyStatus(store, before, nil, nil, ""); got != "today=0 yesterday=0" {
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
				if got := dailyStatus(store, now, buffer.Rows(), batch, "retry"); got != want {
					t.Fatalf("at %s: %s, want %s", now, got, want)
				}
			}
			check(before, "today=6 yesterday=7")
			batch = buffer.Rows()
			check(before, "today=6 yesterday=7")
			// A successful commit whose acknowledgement was lost must not
			// duplicate the batch in live status while it awaits retry.
			if err := store.Apply(ctx, "retry", batch); err != nil {
				t.Fatal(err)
			}
			check(before, "today=6 yesterday=7")
			if err := buffer.Observe(after, 3, true, 0); err != nil {
				t.Fatal(err)
			}
			check(after, "today=3 yesterday=6")
			if err := store.Apply(ctx, "retry", batch); err != nil {
				t.Fatal(err)
			}
			buffer.Acknowledge(batch)
			batch = nil
			check(after, "today=3 yesterday=6")
			rows := buffer.Rows()
			if err := store.Apply(ctx, "next", rows); err != nil {
				t.Fatal(err)
			}
			buffer.Acknowledge(rows)
			check(after, "today=3 yesterday=6")
			check(before, "today=6 yesterday=7")
			check(after.AddDate(0, 0, 1), "today=0 yesterday=3")
			store.Close()
			check(after, "today=? yesterday=?")
		})
	}
}
