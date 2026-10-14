# TODO

## Setup — complete

- [x] Confirm empty project and available Go toolchain (Go 1.26.5).
- [x] Confirm Wayland target; scope later revised to count for the process lifetime.
- [x] Add Go module without dependencies, ignore rules, README, and staged plan.
- [x] Define privacy boundaries, proposed count semantics, and stage acceptance criteria.

## Stage 1 — count keypresses (complete for the validated setup)

- [x] Check actual Ubuntu version, desktop/compositor, seat, and input devices. See [STAGE1.md](STAGE1.md) (2026-10-14).
- [x] Build a runnable evdev counting prototype; verify stream opening on the target desktop without permission changes.
- [x] Build event filtering and an aggregate-only counter with synthetic-event tests.
- [x] Add foreground CLI, periodic total display, graceful shutdown, and single-instance protection.
- [x] Validate basic physical counting and Ctrl+C shutdown with user runs of both prototype and default commands.
- [x] Confirm physical Shift presses count on the target desktop (user report).
- [x] Validate held-key repeat exclusion: holding one letter for three seconds produces exactly +1 (user report).
- [x] Validate mouse click and scroll exclusion on the target desktop (user report).
- [x] Confirm multiple ordinary-key presses accumulate between display ticks (user report; exact three-tap delta not supplied).
- [x] Remove session/lock monitoring and pause/resume logic under the revised user-approved scope.
- [x] Finalize counting defaults and document permissions/setup (existing ordinary-user access on this host; portable fallback deferred until needed).
- [x] Harden stream identity and lifecycle; validate hotplug, multiple keyboards, and dropped-event recovery with deterministic fixtures.
- [x] Avoid duplicate readers for aliases of the same kernel event device; preserve working streams on incomplete discovery and bound metadata scans.
- [x] Validate physical keyboard power-off/on recovery: interface disappears, reopens, and counting resumes (user run).
- [x] Complete basic manual key/modifier/repeat/mouse checks: multiple presses between ticks, Shift counting, held-key +1, and mouse click/scroll exclusion.
- [x] Audit source and runtime output for raw data; add aggregate-status and excluded-identifier output regression checks.
- [x] Add explicit interface exclusion for mirrored composite events, including aliases and reconnecting stable paths; physical selection still needs validation.
- [x] Document the runnable stage-1 prototype workflow and known counting gaps.

## Stage 2 — persist counts (complete)

- [x] Select SQLite driver; add schema versioning and minute-bucket migration.
- [x] Create private per-user data directory and database.
- [x] Add transactional delta flush, final flush, retry semantics, and bounded failure buffering.
- [x] Persist coarse minute coverage and input/polling/clock issue flags; document coverage limits.
- [x] Test restart, minute rollover, abrupt exit, retries, and storage errors.
- [x] Document data location, crash-loss behavior, retention, backup, and deletion.
- [x] Provide an optional user startup service and setup instructions; do not register it automatically.

## Stage 3 — show counts (complete)

- [x] Add loopback-only HTTP server and range/granularity queries.
- [x] Implement timezone-aware hour/day/week aggregation and Monday week starts.
- [x] Add totals, charts, date-range controls, timezone label, and data freshness.
- [x] Label missing collection and stopped periods accurately, without guessing why coverage is absent.
- [x] Test range boundaries, DST, empty data, totals, and loopback binding.
- [x] Document dashboard startup and stop commands.

## Hardware validation backlog

- [ ] Validate the split keyboard in both wired and Bluetooth modes, including switching modes and reconnecting; check for duplicate events across distinct composite interfaces.
- [ ] Validate two independent physical keyboards, including simultaneous presses of the same key, when hardware is available.

These checks were deferred at the user's request on 2026-10-14. They do not block the validated Stage 1 setup.

## Next-session handoff

Stages 1–3 are implemented. Read [STAGE3.md](STAGE3.md), [STAGE2.md](STAGE2.md), and [README.md](README.md). Run the collector normally and start `./bin/keycountr --dashboard` separately for a read-only dashboard at `http://127.0.0.1:8080`. Reports support date ranges, timezone-aware hour/day/Monday-week grouping, committed totals, and coverage labels. Refresh is manual; latest saved minute is not an exact write timestamp or collector-liveness signal. Schema version 1 is unchanged. Race tests, vet, and a CGO-disabled build passed, including real loopback HTTP and concurrent reader/collector tests. Browser layout has not been manually verified. No service was installed/enabled and input permissions were not changed. Keep hardware checks backlogged unless requested. Leave ignored `.codex/` and environment-managed metadata untouched.

The provided `.git` directory did not function during initial setup. It now supports `git status` and diffs; no repository repair is needed for this continuation. Preserve environment-managed metadata and leave the local ignored `.codex/` directory untouched.
