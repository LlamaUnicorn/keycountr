package counter

import (
	"testing"
	"time"
)

func TestMinuteRolloverAndAcknowledgement(t *testing.T) {
	start := time.Date(2026, 10, 14, 23, 59, 59, 900000000, time.FixedZone("local", 3*3600))
	b := New(start)
	if err := b.Observe(start, 2, true, 0); err != nil {
		t.Fatal(err)
	}
	if err := b.Observe(start.Add(200*time.Millisecond), 3, true, 0); err != nil {
		t.Fatal(err)
	}
	rows := b.Rows()
	if len(rows) != 2 {
		t.Fatalf("rows=%v", rows)
	}
	for _, row := range rows {
		if row.Start%60 != 0 || row.RunningMS != 100 || row.HealthyMS != 100 {
			t.Fatalf("wrong UTC minute/coverage: %+v", row)
		}
	}
	if err := b.Observe(start.Add(210*time.Millisecond), 7, true, 0); err != nil {
		t.Fatal(err)
	}
	b.Acknowledge(rows)
	if b.Pending() != 7 {
		t.Fatalf("newer counts lost: %d", b.Pending())
	}
	rows = b.Rows()
	if len(rows) != 1 || rows[0].RunningMS != 10 {
		t.Fatalf("newer coverage lost: %v", rows)
	}
	b.Acknowledge(rows)
	if len(b.Rows()) != 0 {
		t.Fatal("committed rows remain")
	}
}

func TestCoverageGapsAndClockChange(t *testing.T) {
	start := time.Unix(1800000000, 0)
	b := New(start)
	b.Observe(start, 0, true, 0)
	b.Observe(start.Add(time.Second), 0, true, 0)
	row := b.Rows()[0]
	if row.RunningMS != 1000 || row.HealthyMS != 0 || row.Issues&PollGap == 0 {
		t.Fatalf("poll gap hidden: %+v", row)
	}
	b.Observe(start, 1, false, NoInput)
	row = b.Rows()[0]
	if row.Count != 1 || row.Issues&(ClockChange|NoInput) != ClockChange|NoInput {
		t.Fatalf("clock change hidden: %+v", row)
	}
}

func TestBufferLimitRetainsPending(t *testing.T) {
	start := time.Unix(1800000000, 0)
	b := New(start)
	for i := 0; i < MaxMinutes; i++ {
		if err := b.Observe(start.Add(time.Duration(i)*time.Minute), 1, false, NoInput); err != nil {
			t.Fatal(err)
		}
	}
	if err := b.Observe(start.Add(MaxMinutes*time.Minute), 2, true, 0); err == nil {
		t.Fatal("unbounded buffer")
	}
	if b.Pending() != MaxMinutes || len(b.Rows()) != MaxMinutes {
		t.Fatal("pending counts evicted")
	}
}
