package storage

import (
	"context"
	"database/sql"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"keycountr/internal/counter"
)

func openTest(t *testing.T, dir string) *Store {
	t.Helper()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	s, err := Open(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestMigrationRetryRestartAndPrivacy(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	s := openTest(t, dir)
	rows := []counter.Minute{{Start: 1800000000, Count: 3, RunningMS: 100, HealthyMS: 90}, {Start: 1800000060, Count: 4, RunningMS: 100, Issues: counter.NoInput}}
	if err := s.Apply(ctx, "one", rows); err != nil {
		t.Fatal(err)
	}
	// Repeat after an apparently failed commit, including a reopened connection.
	s.db.Close()
	u := "file:" + filepath.Join(dir, "counts.sqlite3")
	s.db, _ = sql.Open("sqlite", u)
	if err := s.Apply(ctx, "one", rows); err != nil {
		t.Fatal(err)
	}
	total, err := s.Total(ctx)
	if err != nil || total != 7 {
		t.Fatalf("retry total=%d err=%v", total, err)
	}
	if err := s.Apply(ctx, "two", []counter.Minute{{Start: 1800000000, Count: 2}}); err != nil {
		t.Fatal(err)
	}
	if other, err := Open(ctx, dir); err == nil {
		other.Close()
		t.Fatal("second database collector accepted")
	}
	s.Close()
	s, err = Open(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	total, err = s.Total(ctx)
	if err != nil || total != 9 {
		t.Fatalf("restart total=%d err=%v", total, err)
	}
	var version int
	s.db.QueryRow("PRAGMA user_version").Scan(&version)
	if version != SchemaVersion {
		t.Fatal("schema version not persisted")
	}
	columns, err := s.db.Query(`SELECT name FROM pragma_table_info('minute_counts') UNION ALL SELECT name FROM pragma_table_info('minute_coverage') UNION ALL SELECT name FROM pragma_table_info('collector_state')`)
	if err != nil {
		t.Fatal(err)
	}
	defer columns.Close()
	allowed := map[string]bool{"bucket_start": true, "count": true, "running_ms": true, "healthy_ms": true, "issues": true, "singleton": true, "last_batch": true}
	for columns.Next() {
		var name string
		columns.Scan(&name)
		if !allowed[name] {
			t.Fatalf("unexpected stored field %s", name)
		}
	}
}

func TestFailedTransactionAndBusyRetry(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	s := openTest(t, dir)
	// Fail after the first row has been written; neither row nor receipt may remain.
	_, err := s.db.Exec(`CREATE TRIGGER storage_error BEFORE INSERT ON minute_counts WHEN NEW.bucket_start = 1800000060 BEGIN SELECT RAISE(ABORT, 'injected storage error'); END`)
	if err != nil {
		t.Fatal(err)
	}
	rows := []counter.Minute{{Start: 1800000000, Count: 2}, {Start: 1800000060, Count: 5}}
	if err := s.Apply(ctx, "retry", rows); err == nil {
		t.Fatal("storage error ignored")
	}
	total, _ := s.Total(ctx)
	if total != 0 {
		t.Fatal("partial transaction committed")
	}
	s.db.Exec("DROP TRIGGER storage_error")
	other, err := sql.Open("sqlite", "file:"+filepath.Join(dir, "counts.sqlite3"))
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	conn, err := other.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err = conn.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		t.Fatal(err)
	}
	if err = s.Apply(ctx, "retry", rows); err == nil {
		t.Fatal("busy write succeeded")
	}
	conn.ExecContext(ctx, "ROLLBACK")
	if err = s.Apply(ctx, "retry", rows); err != nil {
		t.Fatal(err)
	}
	if err = s.Apply(ctx, "retry", rows); err != nil {
		t.Fatal(err)
	}
	total, _ = s.Total(ctx)
	if total != 7 {
		t.Fatalf("retry total=%d", total)
	}
}

func TestPrivateStorageAndJournal(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	s := openTest(t, dir)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err = tx.Exec("INSERT INTO minute_counts VALUES (1800000000, 1)"); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"counts.sqlite3", "counts.sqlite3-journal", "collector.lock"} {
		info, err := os.Stat(filepath.Join(dir, name))
		if err != nil || info.Mode().Perm() != 0600 {
			t.Fatalf("unsafe mode %s: %v %v", name, info, err)
		}
	}
	for _, name := range []string{"counts.sqlite3", "counts.sqlite3-journal", "collector.lock"} {
		t.Run(name, func(t *testing.T) {
			bad := t.TempDir()
			os.Chmod(bad, 0700)
			target := filepath.Join(t.TempDir(), "untouched")
			os.Symlink(target, filepath.Join(bad, name))
			if store, err := Open(ctx, bad); err == nil {
				store.Close()
				t.Fatal("symlink accepted")
			}
			if _, err := os.Stat(target); !os.IsNotExist(err) {
				t.Fatal("symlink target modified")
			}
		})
	}
}

func TestFutureSchemaRejected(t *testing.T) {
	dir := t.TempDir()
	s := openTest(t, dir)
	s.db.Exec("PRAGMA user_version = 99")
	s.Close()
	if store, err := Open(context.Background(), dir); err == nil {
		store.Close()
		t.Fatal("future schema accepted")
	}
}

func TestAbruptExitProcess(t *testing.T) {
	dir := os.Getenv("KEYCOUNTR_CRASH_DIR")
	if dir == "" {
		return
	}
	s, err := Open(context.Background(), dir)
	if err != nil {
		os.Exit(2)
	}
	b := counter.New(time.Unix(1800000000, 0))
	b.Observe(time.Unix(1800000000, 0), 11, true, 0)
	if err = s.Apply(context.Background(), "before-crash", b.Rows()); err != nil {
		os.Exit(3)
	}
	b.Acknowledge(b.Rows())
	b.Observe(time.Unix(1800000001, 0), 7, true, 0)
	// No deferred final flush runs under SIGKILL.
	syscall.Kill(os.Getpid(), syscall.SIGKILL)
	os.Exit(4)
}

func TestAbruptExitPreservesOnlyCommittedCounts(t *testing.T) {
	dir := t.TempDir()
	os.Chmod(dir, 0700)
	cmd := exec.Command(os.Args[0], "-test.run=^TestAbruptExitProcess$")
	cmd.Env = append(os.Environ(), "KEYCOUNTR_CRASH_DIR="+dir)
	if err := cmd.Run(); err == nil {
		t.Fatal("child did not crash")
	}
	s := openTest(t, dir)
	total, err := s.Total(context.Background())
	if err != nil || total != 11 {
		t.Fatalf("crash total=%d err=%v", total, err)
	}
}

func TestDataDir(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	dir, err := DataDir()
	if err != nil || dir != filepath.Join(os.Getenv("XDG_DATA_HOME"), "keycountr") {
		t.Fatal(dir, err)
	}
	t.Setenv("XDG_DATA_HOME", "")
	t.Setenv("HOME", t.TempDir())
	dir, err = DataDir()
	if err != nil || dir != filepath.Join(os.Getenv("HOME"), ".local", "share", "keycountr") {
		t.Fatal(dir, err)
	}
	t.Setenv("XDG_DATA_HOME", "relative")
	if _, err = DataDir(); err == nil {
		t.Fatal("relative data home accepted")
	}
}

func TestFailedMigrationRollsBackAndReleasesLock(t *testing.T) {
	dir := t.TempDir()
	os.Chmod(dir, 0700)
	path := filepath.Join(dir, "counts.sqlite3")
	if err := os.WriteFile(path, nil, 0600); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	// A conflicting table fails the migration after minute_counts was created.
	if _, err := db.Exec("CREATE TABLE minute_coverage (fixture INTEGER)"); err != nil {
		t.Fatal(err)
	}
	if s, err := Open(context.Background(), dir); err == nil {
		s.Close()
		t.Fatal("conflicting schema accepted")
	}
	var tables, version int
	if err := db.QueryRow("SELECT count(*) FROM sqlite_master WHERE name = 'minute_counts'").Scan(&tables); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		t.Fatal(err)
	}
	if tables != 0 || version != 0 {
		t.Fatal("partial migration committed")
	}
	if _, err := db.Exec("DROP TABLE minute_coverage"); err != nil {
		t.Fatal(err)
	}
	s, err := Open(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	s.Close()
}
