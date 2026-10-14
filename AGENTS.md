# Project instructions

## Purpose and boundaries

keycountr is a local Go application for Ubuntu/Wayland, currently Linux amd64.
It counts physical keyboard down events from accessible evdev interfaces for
the process lifetime, including lock screens and other sessions when access
permits. Do not add session monitoring or pause/resume logic without a scope
change. Count modifiers; exclude releases, held-key repeats, and mouse buttons.

Privacy is a core requirement: never reconstruct typed text or persist/log key
codes, raw events, individual event timestamps, device identifiers, window
titles, or application names. Raw events stay inside `internal/input`; only
aggregate counts and coarse coverage cross that boundary. Do not copy local
device inventories or typed terminal input into project files.

Run as the ordinary desktop user, without sudo. Preserve the current narrow
device-access model; do not grant broad input permissions, exclusively grab
devices, or install/enable services as part of routine development.

## Architecture

- `cmd/keycountr`: CLI flags, collection lifecycle, single-instance lock, terminal status.
- `internal/input`: evdev filtering, discovery, exclusions, reconnects, and dropped-event recovery.
- `internal/counter`: aggregate minute buckets, coverage, bounded pending buffer.
- `internal/storage`: private SQLite storage, migrations, transactional retries, and report reads.
- `internal/web`: loopback HTTP dashboard, report aggregation, embedded HTML/CSS/JS.
- `tools`: read-only device diagnostic and optional user-service template.

The same binary currently runs either the collector or `--dashboard`, as separate
processes. Combined mode is backlogged. The dashboard reads committed aggregates
without input access or collector locks. Keep it bound to `127.0.0.1` and preserve
Host/Origin checks. No frontend framework or external assets are required.

Preserve private storage permissions, single-writer protection, immutable retry
batches, bounded buffering, and final flush on orderly shutdown. Store UTC minute
aggregates; reports use timezone-aware calendar grouping, Monday weeks, and
inclusive-start/exclusive-end ranges. Missing coverage must not imply observed
zero activity or a known reason for collection gaps.

## Development and validation

Run commands from the repository root. The module requires Go 1.26.0 or later;
SQLite uses a pinned pure-Go driver and does not require CGO.

```sh
go test ./...
go vet ./...
go build -buildvcs=false -o bin/keycountr ./cmd/keycountr
```

Use `go test -race ./...` for changes involving concurrency, lifecycle, storage,
or HTTP serving. Use `bash -n tools/stage1-probe.sh` when editing the probe.
Format changed Go files with `gofmt`. Run `git diff --check` before committing.

Automated tests must use synthetic input and temporary storage, require no
elevated permissions, and never read real keyboards. Keep physical device checks
separate and user-directed; the hardware backlog is intentionally deferred.
Add focused regression coverage when behavior warrants it; do not add tests
that only mirror trivial implementation details.

Interactive status reuses one line and is clipped to terminal width. `--quiet`,
redirected output, and `TERM=dumb` omit periodic updates. Keep startup messages,
storage warnings, and complete final status visible. Quiet output must not alter
collection or persistence.

## Documentation and scope

Read `docs/README.md` for usage and `docs/TODO.md` for outstanding work and the
next-session handoff. Operational details live in `docs/INPUT.md`,
`docs/STORAGE.md`, and `docs/DASHBOARD.md`. Keep these aligned with behavior.

Keep the active backlog focused on unfinished work. Move completed plans,
checklists, and stage validation records into `docs/archived`; update links when
moving documents. Archived records describe historical behavior, not current
instructions. Preserve evidence limits: do not report hardware or browser checks
as passed unless they were actually performed.

Do not implement backlogged features merely because they are listed. Preserve
environment-managed metadata and leave ignored `.codex/` content untouched.
Do not commit generated binaries, runtime databases, logs, or local inventories.
