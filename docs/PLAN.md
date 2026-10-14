# Implementation plan

## Confirmed requirements

- Go, Ubuntu, Wayland.
- Counts only; never record actual characters.
- Count while the process runs, including lock screens, text consoles, and other users' sessions when input access permits. No session-based pause/resume.
- Deliver a working counter first, persistence second, dashboard third.
- Keep setup simple and data local.

## Stage 1 — Live counter

Build a foreground CLI that prints an aggregate total at a modest refresh rate and exits cleanly on Ctrl+C. Its count resets on restart until stage 2.

Scope revised at the user's request: first deliver physical counting, then finish device handling. The user explicitly accepted counting through locking and session switches while the process runs; remove session monitoring and all pause/resume requirements.

Start with a feasibility spike on the target Ubuntu installation. Record Ubuntu version, desktop/compositor, session identity, seat, and available keyboard devices. Evaluate Linux evdev as the input source: it operates below the display server and is a candidate for Wayland, but device permissions must be solved explicitly. Do not assume that an ordinary Wayland client can observe global keypresses.

For evdev, count keyboard `EV_KEY` events with value `1`; ignore value `0` (release) and `2` (repeat). Filter by both keyboard device capabilities and keyboard event codes: `EV_KEY` also represents buttons. Do not grab devices exclusively or interfere with normal typing. Handle unplug/replug, multiple devices, partial reads, and `SYN_DROPPED`; after a dropped stream, resynchronize without inventing missed counts and report possible undercounting using aggregate diagnostics.

Count from accessible keyboard interfaces for the process lifetime. Locking, console switching, and user switching do not trigger application pauses or descriptor closure. Operating-system permissions, device removal, and normal process termination can still interrupt collection; document those gaps. Do not require a session ID or D-Bus session monitor.

Choose the narrowest workable device-access method after the spike. Avoid making the user a permanent member of a broad input-access group. Keep the counter core unprivileged; if a privileged input helper is necessary, make its interface aggregate-only and document its small scope before implementing it. Never run the future dashboard as root. Do not assume logind's session-controller device APIs are available to a second client alongside the compositor.

Acceptance: a scripted/manual sequence produces the expected total; held keys do not inflate it; mouse activity is excluded; counting is independent of session/lock state; keyboard reconnection recovers; permission failures are actionable; shutdown closes resources. No raw input appears in output or logs.

## Stage 2 — Local storage

Use one SQLite database, with a Go driver chosen at this stage after checking Go/Linux compatibility and build requirements. Prefer a pure-Go driver if its maintenance and compatibility are suitable. No separate database service.

- Store UTC minute buckets with integer counts, e.g. `minute_counts(bucket_start INTEGER PRIMARY KEY, count INTEGER NOT NULL CHECK(count >= 0))`. Bucket start is Unix seconds rounded down to a minute, not a per-event timestamp.
- Aggregate in memory, then transactionally add pending deltas every five seconds and on orderly shutdown. Retain each delta until commit succeeds; serialize writes and prevent duplicate collector instances. Use explicit retry behavior that cannot double-apply committed deltas.
- A crash may lose uncommitted counts; the nominal window is five seconds while writes succeed, longer during storage failures. Surface failure and pending-count status; define bounded buffering before implementation.
- Use the OS user data directory (typically `$XDG_DATA_HOME/keycountr`, falling back to `~/.local/share/keycountr`) with private directory/file permissions, including SQLite sidecar files.
- Start with indefinite local retention; add deletion instructions. No raw-event table, per-key metrics, or cloud sync.
- Store schema version and apply migrations transactionally. A separate collection-coverage table may record coarse running/stopped intervals and device-access gaps if needed to distinguish zero activity from missing collection; it must contain no input details.

Acceptance: restart preserves committed counts; minute rollover is correct; transaction/retry behavior does not double count; storage failure is visible; no sensitive event fields exist in schema or logs. Check clean shutdown and an abrupt process exit against the documented loss window.

## Stage 3 — Local dashboard

Use Go `net/http`, server-rendered HTML, and a small amount of JavaScript only where useful. A separate frontend framework is unnecessary. Implementation of a website belongs to this later stage.

- Bind explicitly to `127.0.0.1` by default. No LAN exposure or public hosting in the initial scope.
- Read the same local database with unprivileged access. The server should not need input-device access.
- Show totals and hourly/daily/weekly charts with a date range and visible reporting timezone.
- Persist UTC; group by calendar boundaries in the reporting timezone. Default to the machine's local timezone, Monday-start weeks, and inclusive-start/exclusive-end ranges. Handle DST and label repeated hours distinctly. Document that changing reporting timezone changes historical grouping.
- Define whether the current incomplete minute is included. Initially show committed data and its refresh timestamp so the five-second storage delay is visible.
- Distinguish zero presses from periods the collector was stopped or lacked device access, using coverage information if needed.

Acceptance: fixture counts produce correct totals across midnight, week/year boundaries, timezone changes, and DST; the server listens only on loopback; no event-level data is exposed.

## Planned code shape

Create packages when the relevant stage starts; avoid empty scaffolding now.

```text
cmd/keycountr/          CLI entry point
internal/input/        Linux input and device lifecycle; discard raw records here
internal/counter/      Aggregate counting and minute buckets
internal/storage/      SQLite, migrations, queries (stage 2)
internal/web/          HTTP handlers, templates, assets (stage 3)
```

Use a small aggregate-only boundary between input and the counter. Test filtering/device lifecycle with synthetic input; test time bucketing with an injected clock. Keep manual hardware checks separate from automated tests so normal tests require no elevated privileges or keyboard access. Add systemd startup only after foreground operation is reliable, using a user service where the chosen access model permits.

## References

- [Linux input event codes](https://docs.kernel.org/input/event-codes.html): press/release/repeat semantics and synchronization behavior.
- [SQLite overview](https://www.sqlite.org/different.html): embedded storage without a database server.

## Deferred choices

Resolve during stage 1: device selection/deduplication, extended-key coverage, backend reliability, and minimal permissions. Resolve during stage 2: SQLite driver and buffering bounds. Defaults pending implementation review: include modifier keys, exclude repeats, minute granularity, local timezone, and Monday-start weeks.

Out of initial scope: per-key statistics, app/window tracking, mouse counters, typed-text capture, remote dashboards, multi-user aggregation, and cross-platform support.
