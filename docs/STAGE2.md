# Stage 2 — aggregate persistence

Implemented on 2026-10-14 for Linux amd64. SQLite uses the pinned pure-Go
`modernc.org/sqlite v1.60.1` driver; no database service or CGO is required.
Driver reference: <https://pkg.go.dev/modernc.org/sqlite>.

## Run and storage location

```sh
go build -buildvcs=false -o bin/keycountr ./cmd/keycountr
./bin/keycountr
```

The printed `total` includes all previously committed counts plus this process's
new presses. `pending` is the number of presses awaiting a confirmed commit.
`storage=ok` means the latest write succeeded (or no write has been attempted);
`storage=retrying` means the latest write failed. The original input diagnostics
and `--prototype`, `--duration`, and repeatable `--exclude-device` flags remain.

The default database is `$XDG_DATA_HOME/keycountr/counts.sqlite3`, falling back to
`~/.local/share/keycountr/counts.sqlite3`. `XDG_DATA_HOME` must be absolute. Override
with `--data-dir=/absolute/private/directory`. The collector creates its directory
with mode 0700 and database/lock files with mode 0600. Existing locations with
unsafe modes, owners, symbolic links at the final directory/file, or hard-linked
storage files are rejected; their permissions are not silently changed.
SQLite's rollback journal inherits the database's private mode. A directory may
have user-managed symlinks in its parent path.

The runtime lock remains, and a second `collector.lock` in the data directory
prevents competing collectors even when their runtime directories differ.
Do not remove either lock while a collector is running. A stopped collector's
lock files are harmless and do not hold a lock.

## Schema and coverage

`PRAGMA user_version = 1` is migrated transactionally. A database with a newer
schema is rejected. The schema contains only:

- `minute_counts(bucket_start, count)`: UTC Unix seconds aligned to a minute and
  the aggregate count. Only nonzero count deltas create these rows.
- `minute_coverage(bucket_start, running_ms, healthy_ms, issues)`: coarse minute
  coverage, with durations capped at 60000 ms per minute. Coverage is saved even
  when no key was pressed.
- `collector_state(singleton, last_batch)`: a single random process/batch receipt
  for retry deduplication. It contains no input or device identifiers.

Counts are assigned to the minute when the collector reads the aggregate,
not to raw kernel event timestamps. Events near a boundary can therefore be
assigned to the next minute if polling was delayed. No per-key data, text,
individual event timestamps, device names, or application names are saved.

`running_ms` describes elapsed collector observation time; `healthy_ms` credits
intervals only when both endpoints have accessible input without reported
problems, and the interval is at most 250 ms. Longer polling/discovery/storage
pauses are conservatively excluded from healthy coverage. Coverage issue bits:
1 = no input; 2 = unavailable interfaces; 4 = dropped stream/read error;
8 = clock moved backward; 16 = polling gap longer than 250 ms.

These are estimates of coverage, not proof that every physical event arrived.
Partial startup/shutdown minutes have partial durations. Missing rows describe
unobserved periods, rather than zero activity. Wall-clock changes can overlap
coverage; durations are capped and affected minutes retain issue flags. The
future dashboard should mark incomplete or impaired coverage, including the
latest uncommitted interval. Coverage itself shares the crash-loss window.

## Flush, retries, and loss window

Every five seconds, one transaction adds a snapshot of pending minute deltas
and updates coverage and the batch receipt. The snapshot and token stay fixed
until confirmed success. Input collected during retries stays in the buffer;
acknowledging a snapshot removes only that snapshot's deltas. If a commit succeeds
but its result is ambiguous, retrying the same receipt avoids double counting.
Only the most recent receipt is retained, so receipt storage does not grow.
Writes are serialized. SQLite uses DELETE journaling, FULL synchronization, and
a 100 ms busy timeout; a flush has a one-second context deadline.

Ctrl+C, SIGTERM, and `--duration` close the input streams and make up to three
bounded flush attempts, also draining data collected after a failed snapshot.
A failed final flush is reported with a nonzero exit status. Pending data then
leaves memory and is lost. The terminal total can exceed the persisted total.

An abrupt kill or power loss may lose uncommitted data: nominally the latest
five seconds plus scheduling/write delays when writes work, longer while storage
is failing. There is no durable spill buffer. A maximum of 1440 distinct pending
minute buckets (including zero-count coverage) bounds memory. A frozen retry
snapshot uses at most another 1440 rows. At capacity exhaustion, collection stops
with an error and tries a final flush; accepted buffered deltas are retained until
then. Counts in the poll that encounters capacity/overflow can appear in the live
total without entering storage. The limit is roughly a day, not an exact 24-hour
wall-clock guarantee. There is no silent eviction of older pending minutes.

## Retention, backup, and deletion

Retention is indefinite, with no network synchronization. For a simple consistent
backup, stop the collector successfully, then copy its entire private data
directory to a private backup location. Do not copy only a live database file;
a hot journal may be required for recovery. On failed shutdown or abrupt exit,
reopen the database with the collector, then stop successfully before copying.
Restore the database into a private directory while the collector is stopped;
keep directory mode 0700 and files mode 0600.

To erase history, first stop the collector and any future dashboard. Delete
`counts.sqlite3` and any `counts.sqlite3-journal`, `counts.sqlite3-wal`, and
`counts.sqlite3-shm` files from the selected data directory. The next start
creates a fresh schema and starts at zero. Delete backups separately. Ordinary
file deletion does not guarantee physical erasure from disks, snapshots, or
backups.

## Optional startup

Foreground collection and existing input access are validated on this host.
An optional user-service template is provided at
[../tools/keycountr.service](../tools/keycountr.service). No service was installed
or enabled as part of implementation. It starts with the graphical session,
uses `~/.local/bin/keycountr`, sets a private umask, and stops with that session.
Copy any validated `--exclude-device` arguments into `ExecStart` before enabling
it. Do not run the foreground collector and service together.

If startup is wanted, stop the foreground counter, then run as the desktop user:

```sh
install -Dm755 bin/keycountr "$HOME/.local/bin/keycountr"
install -Dm644 tools/keycountr.service "$HOME/.config/systemd/user/keycountr.service"
systemctl --user daemon-reload
systemctl --user enable --now keycountr.service
systemctl --user status keycountr.service
journalctl --user -u keycountr.service -n 20
```

Check `keyboards`, `unavailable`, and `storage` in the journal: a service's device
access may differ from an interactive shell. This template does not grant input
permissions. Disable with `systemctl --user disable --now keycountr.service`.
After rebuilding, repeat binary installation while stopped, then start the
service again. The template does not auto-restart on storage failure, so a
buffer-capacity failure remains visible instead of repeatedly discarding data.

## Automated validation

`go test -race ./...` exercises UTC minute rollover, retry snapshot acknowledgement,
bounded buffering, clock/polling coverage gaps, transactional migration,
restart totals, deduplication across connections, write failure rollback,
SQLite writer contention and recovery, schema rejection, private database and
journal modes, symlink rejection, and a SIGKILL subprocess with committed and
uncommitted counts. CLI subprocess tests isolate storage and exclude real input;
they verify final persistence, restored totals, aggregate-only status, and visible
final-write failure. They do not repeat the backlogged hardware checks.
