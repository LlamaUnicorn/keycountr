package storage

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"keycountr/internal/counter"
)

func TestReaderWithActiveCollector(t *testing.T) {
	ctx := context.Background()
	s := openTest(t, t.TempDir())
	dir := filepath.Dir(s.lock.Name())
	if err := s.Apply(ctx, "first", []counter.Minute{{Start: 1800000000, Count: 7, RunningMS: 5000, HealthyMS: 4000, Issues: 2}, {Start: 1800000060, RunningMS: 60000, HealthyMS: 60000}, {Start: 1800000120, Count: 9}}); err != nil {
		t.Fatal(err)
	}
	reader, err := OpenReader(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	snapshot, err := reader.Snapshot(ctx, 1800000000, 1800000120, time.Unix(1800000120, 0))
	if err != nil || snapshot.Total != 16 || len(snapshot.Minutes) != 2 || snapshot.Minutes[0].Count != 7 || snapshot.Minutes[1].Count != 0 || snapshot.LatestMinute == nil || *snapshot.LatestMinute != 1800000120 {
		t.Fatal(snapshot, err)
	}
	if _, err := reader.db.Exec("DELETE FROM minute_counts"); err == nil {
		t.Fatal("reader wrote database")
	}
	if err := s.Apply(ctx, "next", []counter.Minute{{Start: 1800000000, Count: 3}}); err != nil {
		t.Fatal(err)
	}
	snapshot, err = reader.Snapshot(ctx, 1800000000, 1800000120, time.Unix(1800000120, 0))
	if err != nil || snapshot.Total != 19 || snapshot.Minutes[0].Count != 10 {
		t.Fatal(snapshot, err)
	}
}

func TestReaderNeverCreatesAndRejectsUnsafeFiles(t *testing.T) {
	ctx := context.Background()
	dir := filepath.Join(t.TempDir(), "absent")
	if r, err := OpenReader(ctx, dir); err == nil {
		r.Close()
		t.Fatal("missing directory accepted")
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatal("created directory")
	}
	dir = t.TempDir()
	os.Chmod(dir, 0700)
	if r, err := OpenReader(ctx, dir); err == nil {
		r.Close()
		t.Fatal("missing database accepted")
	}
	if _, err := os.Stat(filepath.Join(dir, "counts.sqlite3")); !os.IsNotExist(err) {
		t.Fatal("created database")
	}
	s := openTest(t, dir)
	s.Close()
	for _, name := range []string{"counts.sqlite3-journal", "counts.sqlite3-wal", "counts.sqlite3-shm"} {
		os.Symlink("missing", filepath.Join(dir, name))
		if r, err := OpenReader(ctx, dir); err == nil {
			r.Close()
			t.Fatal("unsafe sidecar accepted")
		}
		os.Remove(filepath.Join(dir, name))
	}
	os.Chmod(filepath.Join(dir, "counts.sqlite3"), 0644)
	if r, err := OpenReader(ctx, dir); err == nil {
		r.Close()
		t.Fatal("public database accepted")
	}
}
