# TODO

## Setup — complete

- [x] Confirm empty project and available Go toolchain (Go 1.26.5).
- [x] Confirm Wayland target; scope later revised to count for the process lifetime.
- [x] Add Go module without dependencies, ignore rules, README, and staged plan.
- [x] Define privacy boundaries, proposed count semantics, and stage acceptance criteria.

## Stage 1 — count keypresses

- [x] Check actual Ubuntu version, desktop/compositor, seat, and input devices. See [STAGE1.md](STAGE1.md) (2026-10-14).
- [x] Build a runnable evdev counting prototype; verify stream opening on the target desktop without permission changes.
- [x] Build event filtering and an aggregate-only counter with synthetic-event tests.
- [x] Add foreground CLI, periodic total display, graceful shutdown, and single-instance protection.
- [x] Validate basic physical counting and Ctrl+C shutdown with user runs of both prototype and default commands.
- [ ] Validate exact modifier/repeat/mouse deltas on the target desktop; finalize the minimal access method.
- [x] Remove session/lock monitoring and pause/resume logic under the revised user-approved scope.
- [ ] Finalize counting defaults and document permissions/setup.
- [x] Harden stream identity and lifecycle; validate hotplug, multiple keyboards, and dropped-event recovery with deterministic fixtures.
- [x] Avoid duplicate readers for aliases of the same kernel event device; preserve working streams on incomplete discovery and bound metadata scans.
- [ ] Validate physical hotplug and multiple keyboards on the target desktop; resolve duplicate events across distinct composite interfaces.
- [ ] Run manual key/modifier/repeat/mouse checks; audit logs for raw data.
- [x] Document the runnable stage-1 prototype workflow and known counting gaps.

## Stage 2 — persist counts

- [ ] Select SQLite driver; add schema versioning and minute-bucket migration.
- [ ] Create private per-user data directory and database.
- [ ] Add transactional delta flush, final flush, retry semantics, and bounded failure buffering.
- [ ] Persist coarse collector coverage if required to label gaps accurately.
- [ ] Test restart, minute rollover, abrupt exit, retries, and storage errors.
- [ ] Document data location, crash-loss behavior, retention, backup, and deletion.
- [ ] Add optional startup service after the permissions model is validated.

## Stage 3 — show counts

- [ ] Add loopback-only HTTP server and range/granularity queries.
- [ ] Implement timezone-aware hour/day/week aggregation and Monday week starts.
- [ ] Add totals, charts, date-range controls, timezone label, and data freshness.
- [ ] Label missing collection and stopped periods accurately.
- [ ] Test range boundaries, DST, empty data, totals, and loopback binding.
- [ ] Document dashboard startup and stop commands.

## Next-session handoff

Read [docs/STAGE1.md](STAGE1.md), [docs/README.md](README.md), and [docs/PLAN.md](PLAN.md). Stage 1 is in progress. The user confirmed the prototype appears to count correctly and explicitly accepted counting while running across lock screens and other sessions. Session checks and their tests were removed; the CLI starts without flags, and --prototype remains compatible. Lifecycle fixtures now pass for hotplug, multiple keyboards, drops, replacement races, access failures, and kernel-device aliases. A rebuilt three-second host run opened both interfaces with no errors. Continue with exact repeat/modifier/mouse validation, physical hotplug/multiple-keyboard checks, and deduplication across distinct composite interfaces. Alias deduplication does not solve mirrored events from distinct kernel devices. Hardware checks require host device access outside the sandbox. No dependencies were installed, input permissions changed, or services registered.

The provided `.git` directory did not function as a Git repository during setup (`git status` reported no repository). Files are prepared in the workspace; initialize or repair version control separately when appropriate, preserving environment-managed metadata.
