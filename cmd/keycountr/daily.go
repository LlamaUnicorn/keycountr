package main

import (
	"context"
	"fmt"
	"time"

	"keycountr/internal/counter"
	"keycountr/internal/storage"
)

func dailyStatus(store *storage.Store, now time.Time, pending, batch []counter.Minute, batchID string) string {
	local := now.In(time.Local)
	today := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, local.Location())
	tomorrow := today.AddDate(0, 0, 1).Unix()
	var pendingDay *time.Time
	for _, row := range pending {
		if row.Count <= 0 || row.Start >= today.Unix() {
			continue
		}
		local := time.Unix(row.Start, 0).In(today.Location())
		day := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, today.Location())
		if pendingDay == nil || day.After(*pendingDay) {
			pendingDay = &day
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	summary, err := store.DailyTotals(ctx, today, pendingDay)
	if err != nil {
		return "today=? previous=?"
	}
	add := func(rows []counter.Minute, subtract bool) {
		for _, row := range rows {
			var target *uint64
			switch {
			case row.Start >= today.Unix() && row.Start < tomorrow:
				target = &summary.Current
			case summary.PreviousDay != nil && row.Start >= summary.PreviousDay.Unix() && row.Start < summary.PreviousDay.AddDate(0, 0, 1).Unix():
				target = &summary.Previous
			default:
				continue
			}
			if subtract {
				*target -= uint64(row.Count)
			} else {
				*target += uint64(row.Count)
			}
		}
	}
	add(pending, false)
	if batchID != "" && summary.Receipt == batchID {
		add(batch, true)
	}
	if summary.PreviousDay == nil {
		return fmt.Sprintf("today=%d previous=none", summary.Current)
	}
	return fmt.Sprintf("today=%d previous=%d(%s)", summary.Current, summary.Previous, summary.PreviousDay.Format("2006-01-02"))
}
