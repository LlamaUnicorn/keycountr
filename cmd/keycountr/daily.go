package main

import (
	"context"
	"fmt"
	"time"

	"keycountr/internal/counter"
	"keycountr/internal/storage"
)

type dailyDisplay struct{ Daily, Average string }

func dailyStatus(store *storage.Store, now time.Time, pending, batch []counter.Minute, batchID string) dailyDisplay {
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	stats, err := store.Activity(ctx, now.In(time.Local), pending, batch, batchID)
	if err != nil {
		return dailyDisplay{"today=? previous=?", "avg7=?"}
	}
	daily := fmt.Sprintf("today=%d previous=none", stats.Today)
	if stats.Previous != nil {
		daily = fmt.Sprintf("today=%d previous=%d(%s)", stats.Today, stats.Previous.Count, stats.Previous.Date)
	}
	average := "avg7=none"
	if stats.Average != nil {
		average = fmt.Sprintf("avg7=%.1f(%dd)", *stats.Average, stats.Days)
	}
	return dailyDisplay{daily, average}
}
