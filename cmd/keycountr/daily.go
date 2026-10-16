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
	yesterday, tomorrow := today.AddDate(0, 0, -1).Unix(), today.AddDate(0, 0, 1).Unix()
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	previous, current, receipt, err := store.DailyTotals(ctx, yesterday, today.Unix(), tomorrow)
	if err != nil {
		return "today=? yesterday=?"
	}
	add := func(rows []counter.Minute, subtract bool) {
		for _, row := range rows {
			var target *uint64
			switch {
			case row.Start >= today.Unix() && row.Start < tomorrow:
				target = &current
			case row.Start >= yesterday && row.Start < today.Unix():
				target = &previous
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
	if batchID != "" && receipt == batchID {
		add(batch, true)
	}
	return fmt.Sprintf("today=%d yesterday=%d", current, previous)
}
