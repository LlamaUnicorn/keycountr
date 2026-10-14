// Package counter retains only minute aggregates, never individual input events.
package counter

import (
	"errors"
	"math"
	"time"
)

const MaxMinutes = 1440

// Coverage issues are coarse flags, without device identifiers.
const (
	NoInput int64 = 1 << iota
	Degraded
	InputLoss
	ClockChange
	PollGap
)

type Minute struct {
	Start     int64
	Count     int64
	RunningMS int64
	HealthyMS int64
	Issues    int64
}

// Buffer is used by the collector goroutine only. Capacity exhaustion stops the
// collector rather than silently evicting old, uncommitted counts.
type Buffer struct {
	rows    map[int64]Minute
	last    time.Time
	healthy bool
}

func New(now time.Time) *Buffer {
	return &Buffer{rows: make(map[int64]Minute), last: now}
}

func minute(t time.Time) int64 { return t.UTC().Truncate(time.Minute).Unix() }

func (b *Buffer) Observe(now time.Time, count uint64, healthy bool, issues int64) error {
	if count > math.MaxInt64 {
		return errors.New("count exceeds storage integer limit")
	}
	start := minute(now)
	from := b.last.UnixMilli()
	to := now.UnixMilli()
	if to < from {
		issues |= ClockChange
		from = to
	}
	// A long polling pause may conceal an input-access gap. Never claim healthy
	// coverage for it, even if interfaces are open at both endpoints.
	intervalHealthy := b.healthy && healthy && issues == 0 && to-from <= 250
	if to-from > 250 {
		issues |= PollGap
	}
	first := minute(time.UnixMilli(from))
	if (start-first)/60 >= MaxMinutes {
		return errors.New("pending minute buffer full; collection stopped")
	}
	needed := 0
	for m := first; m <= start; m += 60 {
		if _, ok := b.rows[m]; !ok {
			needed++
		}
	}
	if len(b.rows)+needed > MaxMinutes {
		return errors.New("pending minute buffer full; collection stopped")
	}
	if b.rows[start].Count > math.MaxInt64-int64(count) {
		return errors.New("minute count exceeds storage integer limit")
	}
	for from < to {
		m := minute(time.UnixMilli(from))
		end := min(to, (m+60)*1000)
		row := b.rows[m]
		row.Start = m
		row.RunningMS = row.RunningMS + end - from
		if intervalHealthy {
			row.HealthyMS = row.HealthyMS + end - from
		}
		row.Issues |= issues
		b.rows[m] = row
		from = end
	}
	row := b.rows[start]
	row.Start = start
	row.Count += int64(count)
	row.Issues |= issues
	b.rows[start] = row
	b.last, b.healthy = now, healthy
	return nil
}

func (b *Buffer) Rows() []Minute {
	rows := make([]Minute, 0, len(b.rows))
	for _, row := range b.rows {
		rows = append(rows, row)
	}
	return rows
}

func (b *Buffer) Pending() uint64 {
	var total uint64
	for _, row := range b.rows {
		total += uint64(row.Count)
	}
	return total
}

// Acknowledge only after the entire batch has committed successfully.
func (b *Buffer) Acknowledge(rows []Minute) {
	for _, flushed := range rows {
		row := b.rows[flushed.Start]
		if row == flushed {
			delete(b.rows, flushed.Start)
			continue
		}
		row.Count -= flushed.Count
		row.RunningMS -= flushed.RunningMS
		row.HealthyMS -= flushed.HealthyMS
		b.rows[flushed.Start] = row
	}
}
