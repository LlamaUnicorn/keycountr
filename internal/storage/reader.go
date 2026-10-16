package storage

import (
	"context"
	"database/sql"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"keycountr/internal/counter"
)

// Reader holds no collector lock and never creates or migrates a database.
type Reader struct{ db *sql.DB }

func OpenReader(ctx context.Context, dir string) (*Reader, error) {
	if !filepath.IsAbs(dir) {
		return nil, errors.New("data directory must be absolute")
	}
	info, err := os.Lstat(dir)
	if err != nil || !info.IsDir() || info.Mode().Perm() != 0700 {
		return nil, errors.New("existing private data directory required; run the collector first")
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != uint32(os.Getuid()) {
		return nil, errors.New("data directory must belong to the current user")
	}
	path := filepath.Join(dir, "counts.sqlite3")
	for _, suffix := range []string{"", "-journal", "-wal", "-shm"} {
		info, err := os.Lstat(path + suffix)
		if suffix != "" && os.IsNotExist(err) {
			continue
		}
		if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 {
			return nil, errors.New("existing private database required; run the collector first")
		}
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok || stat.Uid != uint32(os.Getuid()) || stat.Nlink != 1 {
			return nil, errors.New("unsafe database file")
		}
	}
	u := url.URL{Scheme: "file", Path: path}
	q := u.Query()
	q.Set("mode", "ro")
	q.Add("_pragma", "busy_timeout(100)")
	q.Add("_pragma", "query_only(ON)")
	u.RawQuery = q.Encode()
	db, err := sql.Open("sqlite", u.String())
	if err != nil {
		return nil, errors.New("cannot open dashboard database")
	}
	db.SetMaxOpenConns(1)
	var version int
	if err := db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil || version != SchemaVersion {
		db.Close()
		return nil, errors.New("dashboard requires the current database schema; recover with the collector after an abrupt exit")
	}
	return &Reader{db: db}, nil
}

type Snapshot struct {
	Minutes      []counter.Minute
	Total        uint64
	LatestMinute *int64
	Activity     ActivityStats
}

// Snapshot reads counts, coverage and lifetime total from one consistent transaction.
func (r *Reader) Snapshot(ctx context.Context, start, end int64, now time.Time) (Snapshot, error) {
	var out Snapshot
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return out, errors.New("cannot read aggregate snapshot")
	}
	defer tx.Rollback()
	if err := tx.QueryRowContext(ctx, "SELECT coalesce(sum(count), 0) FROM minute_counts").Scan(&out.Total); err != nil {
		return out, errors.New("cannot read aggregate total")
	}
	var latest sql.NullInt64
	if err := tx.QueryRowContext(ctx, `SELECT max(bucket_start) FROM (SELECT bucket_start FROM minute_counts UNION ALL SELECT bucket_start FROM minute_coverage)`).Scan(&latest); err != nil {
		return out, errors.New("cannot read latest saved minute")
	}
	if latest.Valid {
		out.LatestMinute = &latest.Int64
	}
	rows, err := tx.QueryContext(ctx, `SELECT m.bucket_start, coalesce(c.count,0), coalesce(v.running_ms,0), coalesce(v.healthy_ms,0), coalesce(v.issues,0)
 FROM (SELECT bucket_start FROM minute_counts WHERE bucket_start >= ? AND bucket_start < ? UNION SELECT bucket_start FROM minute_coverage WHERE bucket_start >= ? AND bucket_start < ?) m
 LEFT JOIN minute_counts c ON c.bucket_start=m.bucket_start LEFT JOIN minute_coverage v ON v.bucket_start=m.bucket_start ORDER BY m.bucket_start`, start, end, start, end)
	if err != nil {
		return out, errors.New("cannot read aggregate minutes")
	}
	for rows.Next() {
		var m counter.Minute
		if err := rows.Scan(&m.Start, &m.Count, &m.RunningMS, &m.HealthyMS, &m.Issues); err != nil {
			rows.Close()
			return out, errors.New("cannot decode aggregate minutes")
		}
		out.Minutes = append(out.Minutes, m)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, errors.New("cannot finish aggregate query")
	}
	out.Activity, err = readActivity(ctx, tx, now, nil)
	if err != nil {
		return out, err
	}
	if err := tx.Commit(); err != nil {
		return out, errors.New("cannot finish aggregate snapshot")
	}
	return out, nil
}

func (r *Reader) Close() error { return r.db.Close() }
