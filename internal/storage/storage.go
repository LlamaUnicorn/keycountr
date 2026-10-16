// Package storage persists aggregate counts and coarse coverage in SQLite.
package storage

import (
	"context"
	"database/sql"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"syscall"

	"keycountr/internal/counter"
	_ "modernc.org/sqlite"
)

const SchemaVersion = 1

type Store struct {
	db   *sql.DB
	lock *os.File
}

func DataDir() (string, error) {
	base := os.Getenv("XDG_DATA_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		base = filepath.Join(home, ".local", "share")
	}
	if !filepath.IsAbs(base) {
		return "", errors.New("user data directory must be absolute")
	}
	return filepath.Join(base, "keycountr"), nil
}

// PrivateDir never changes permissions on an existing directory.
func PrivateDir(dir string) error {
	if !filepath.IsAbs(dir) {
		return errors.New("data directory must be absolute")
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return errors.New("cannot create private data directory")
	}
	info, err := os.Lstat(dir)
	if err != nil || !info.IsDir() || info.Mode().Perm() != 0700 {
		return errors.New("data directory must be a private directory with mode 0700")
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != uint32(os.Getuid()) {
		return errors.New("data directory must belong to the current user")
	}
	return nil
}

func privateFile(path string) (*os.File, error) {
	fd, err := syscall.Open(path, syscall.O_RDWR|syscall.O_CREAT|syscall.O_CLOEXEC|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0600)
	if err != nil {
		return nil, errors.New("cannot open private storage file")
	}
	f := os.NewFile(uintptr(fd), path)
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 {
		f.Close()
		return nil, errors.New("storage files must be regular files with mode 0600")
	}
	stat := info.Sys().(*syscall.Stat_t)
	if stat.Uid != uint32(os.Getuid()) || stat.Nlink != 1 {
		f.Close()
		return nil, errors.New("storage files must be owned by the current user without hard links")
	}
	return f, nil
}

func Open(ctx context.Context, dir string) (*Store, error) {
	if err := PrivateDir(dir); err != nil {
		return nil, err
	}
	lock, err := privateFile(filepath.Join(dir, "collector.lock"))
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		lock.Close()
		return nil, errors.New("another collector owns this database")
	}
	s := &Store{lock: lock}
	success := false
	defer func() {
		if !success {
			s.Close()
		}
	}()
	path := filepath.Join(dir, "counts.sqlite3")
	file, err := privateFile(path)
	if err != nil {
		return nil, err
	}
	file.Close()
	// SQLite inherits database mode for its journal. Reject unsafe pre-existing
	// sidecars before SQLite can open them (including dangling symlinks).
	for _, suffix := range []string{"-journal", "-wal", "-shm"} {
		side := path + suffix
		if _, err := os.Lstat(side); !os.IsNotExist(err) {
			f, err := privateFile(side)
			if err != nil {
				return nil, err
			}
			f.Close()
		}
	}
	u := url.URL{Scheme: "file", Path: path}
	q := u.Query()
	q.Set("_pragma", "busy_timeout(100)")
	q.Add("_pragma", "journal_mode(DELETE)")
	q.Add("_pragma", "synchronous(FULL)")
	u.RawQuery = q.Encode()
	s.db, err = sql.Open("sqlite", u.String())
	if err != nil {
		return nil, errors.New("cannot open database")
	}
	s.db.SetMaxOpenConns(1)
	if err := s.migrate(ctx); err != nil {
		return nil, err
	}
	success = true
	return s, nil
}

func (s *Store) migrate(ctx context.Context) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return errors.New("cannot begin schema migration")
	}
	defer tx.Rollback()
	var version int
	if err := tx.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		return errors.New("cannot read schema version")
	}
	if version > SchemaVersion {
		return errors.New("database schema is newer than this collector")
	}
	if version == 0 {
		for _, stmt := range []string{
			`CREATE TABLE minute_counts (bucket_start INTEGER PRIMARY KEY CHECK(bucket_start % 60 = 0), count INTEGER NOT NULL CHECK(typeof(count) = 'integer' AND count >= 0))`,
			`CREATE TABLE minute_coverage (bucket_start INTEGER PRIMARY KEY CHECK(bucket_start % 60 = 0), running_ms INTEGER NOT NULL CHECK(running_ms BETWEEN 0 AND 60000), healthy_ms INTEGER NOT NULL CHECK(healthy_ms BETWEEN 0 AND running_ms), issues INTEGER NOT NULL CHECK(issues >= 0))`,
			`CREATE TABLE collector_state (singleton INTEGER PRIMARY KEY CHECK(singleton = 1), last_batch TEXT NOT NULL)`,
			`PRAGMA user_version = 1`,
		} {
			if _, err := tx.ExecContext(ctx, stmt); err != nil {
				return errors.New("cannot migrate aggregate schema")
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return errors.New("cannot commit schema migration")
	}
	return nil
}

// Apply is idempotent for the current batch. The collector must retry the exact
// same token and rows until success, before issuing a new batch. The single-row
// receipt and counts commit together; ambiguous commit results cannot duplicate
// a batch. A per-database lock enforces one collector, even with different
// XDG_RUNTIME_DIR values. Only the latest receipt is needed, with bounded space.
func (s *Store) Apply(ctx context.Context, token string, rows []counter.Minute) error {
	if token == "" {
		return errors.New("empty batch token")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return errors.New("cannot begin aggregate flush")
	}
	defer tx.Rollback()
	var previous string
	err = tx.QueryRowContext(ctx, "SELECT last_batch FROM collector_state WHERE singleton = 1").Scan(&previous)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return errors.New("cannot read flush receipt")
	}
	if previous == token {
		return nil
	}
	for _, row := range rows {
		if row.Start%60 != 0 || row.Count < 0 || row.RunningMS < 0 || row.HealthyMS < 0 || row.HealthyMS > row.RunningMS || row.Issues < 0 {
			return errors.New("invalid minute aggregate")
		}
		if row.Count != 0 {
			_, err = tx.ExecContext(ctx, `INSERT INTO minute_counts VALUES (?, ?) ON CONFLICT(bucket_start) DO UPDATE SET count = count + excluded.count`, row.Start, row.Count)
			if err != nil {
				return errors.New("cannot write minute counts")
			}
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO minute_coverage VALUES (?, ?, ?, ?) ON CONFLICT(bucket_start) DO UPDATE SET running_ms = min(60000, running_ms + excluded.running_ms), healthy_ms = min(60000, healthy_ms + excluded.healthy_ms), issues = issues | excluded.issues`, row.Start, min(int64(60000), row.RunningMS), min(int64(60000), row.HealthyMS), row.Issues)
		if err != nil {
			return errors.New("cannot write minute coverage")
		}
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO collector_state VALUES (1, ?) ON CONFLICT(singleton) DO UPDATE SET last_batch = excluded.last_batch`, token); err != nil {
		return errors.New("cannot write flush receipt")
	}
	if err := tx.Commit(); err != nil {
		return errors.New("cannot confirm aggregate commit; retrying same batch")
	}
	return nil
}

func (s *Store) Total(ctx context.Context) (uint64, error) {
	var total uint64
	if err := s.db.QueryRowContext(ctx, "SELECT coalesce(sum(count), 0) FROM minute_counts").Scan(&total); err != nil {
		return 0, errors.New("cannot read persisted total")
	}
	return total, nil
}

// DailyTotals reads both calendar ranges and the retry receipt in one snapshot.
// The receipt lets live status avoid counting a committed retry batch twice.
func (s *Store) DailyTotals(ctx context.Context, yesterday, today, tomorrow int64) (previous, current uint64, receipt string, err error) {
	err = s.db.QueryRowContext(ctx, `SELECT
 coalesce(sum(CASE WHEN bucket_start < ? THEN count ELSE 0 END), 0),
 coalesce(sum(CASE WHEN bucket_start >= ? THEN count ELSE 0 END), 0),
 coalesce((SELECT last_batch FROM collector_state WHERE singleton = 1), '')
 FROM minute_counts WHERE bucket_start >= ? AND bucket_start < ?`, today, today, yesterday, tomorrow).Scan(&previous, &current, &receipt)
	if err != nil {
		err = errors.New("cannot read daily totals")
	}
	return
}

func (s *Store) Close() error {
	var err error
	if s.db != nil {
		err = s.db.Close()
	}
	if s.lock != nil {
		err = errors.Join(err, s.lock.Close())
	}
	return err
}
