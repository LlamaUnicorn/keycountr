# keycountr

A small Go application for Ubuntu that counts keyboard presses without recording typed text.

Status: Stage 1 is complete for the validated Linux amd64 setup. Physical key/modifier counting, repeat and mouse exclusion, keyboard power-off/on recovery, and shutdown are user-confirmed. Further split-keyboard wired/Bluetooth and multiple-keyboard checks are backlogged. Stage 2 now saves minute aggregates and coarse coverage in a private local SQLite database. Explicit interface exclusion is available for mirrored composite events. Stage 3 provides a separate local dashboard with saved totals, charts, date ranges, and coverage labels.

## Scope

- Target: Ubuntu desktop running Wayland; the evdev backend currently supports Linux amd64.
- Count from accessible keyboard interfaces whenever the process runs. Lock screens, text consoles, and other users' sessions are included when device access permits. There is no session monitoring or pause/resume logic.
- Stage 1: show a live, in-memory count in a terminal.
- Stage 2: save aggregate counts locally in SQLite.
- Stage 3: serve a local dashboard with hourly, daily, and weekly totals.

Count each physical key-down once, including modifiers, navigation, and function keys. Ignore releases, held-key repeats, mouse buttons, and other non-keyboard events. Two simultaneous presses count as two. Count across attached keyboard-class interfaces, subject to the code-range and duplicate-device gaps described in [INPUT.md](INPUT.md).

Privacy rule: never translate events into characters, reconstruct text, or persist/log key codes, individual event timestamps, device identifiers, window titles, or application names. Raw input stays at the input boundary and is discarded after filtering. Only totals leave that boundary. Password-entry presses contribute to the total, including on the lock screen, but their contents are never reconstructed. Only minute aggregates and coarse collection coverage are saved to disk; see [STORAGE.md](STORAGE.md).

## Setup

Go 1.26.5 on linux/amd64 was available during setup. The module declares Go 1.26.0 and uses the pinned pure-Go `modernc.org/sqlite` driver for local storage; CGO is not required. Its local module path is `keycountr`; change it to the repository URL if publishing it later.

Run commands from the repository root. Build and run as your ordinary user, without sudo:

```sh
go build -buildvcs=false -o bin/keycountr ./cmd/keycountr
./bin/keycountr
```

In an interactive terminal, the counter updates one status line once per second. Long live status lines are clipped to fit the terminal width; the final status is complete. Use `./bin/keycountr --quiet` to disable live updates. Redirected output, user-service logs, and `TERM=dumb` automatically omit live updates. Startup messages, storage warnings, and the final total remain visible in all modes. Quiet mode does not change collection or saving.

Wait for `state=counting` in the live display, then press keys, hold a key to test repeats, and click the mouse. Ctrl+C flushes pending aggregates, prints the final total, and stops. Totals include previously committed counts and persist across restarts. `pending` reports uncommitted presses; `storage=retrying` signals a failed write. Writes occur every five seconds. The previous `--prototype` flag is still accepted for compatibility; no flags are required to count.

`udevadm` supplies device classification. Neither D-Bus access nor a session ID is required. A private runtime directory (`$XDG_RUNTIME_DIR`, or `/run/user/UID`) holds the per-user instance lock. `-buildvcs=false` omits VCS metadata from the build; Git operations now work in this workspace.

An optional read-only device-access diagnostic does not open input streams:

```sh
bash tools/stage1-probe.sh
```

See [INPUT.md](INPUT.md) for device access, manual checks, and known counting gaps. Existing ordinary-user device access is the chosen setup on this host; input permissions and services have not been changed. For mirrored composite events, `--exclude-device` accepts a repeatable absolute input-interface path; prefer a stable `/dev/input/by-id/` symlink. See the interface isolation workflow in [INPUT.md](INPUT.md) before choosing one.

See [STORAGE.md](STORAGE.md) for the data location, retry/loss behavior, coverage schema, backup/deletion, and optional user-service setup. No service has been registered.

To collect and serve the dashboard in one process:

```sh
./bin/keycountr --with-dashboard
```

Open <http://127.0.0.1:49371>. Ctrl+C stops both components and flushes pending
aggregates. Use `--port=0` to choose a free port. If the dashboard cannot start,
collection does not begin; a later server failure stops collection with a final
flush and an error. Collector-only remains the default. No browser opens automatically.

Alternatively, after the collector has initialized the database, start the dashboard in another terminal:

```sh
./bin/keycountr --dashboard
```

Open <http://127.0.0.1:49371>. Choose dates, hour/day/week grouping, and reporting timezone; click **Apply / refresh** for fresh committed data. Leave dates blank for the latest seven days, recalculated on each refresh; explicitly entered dates stay fixed. Ctrl+C stops the dashboard independently of collection. Use the same `--data-dir` for both processes when overriding storage. See [DASHBOARD.md](DASHBOARD.md) for coverage, range semantics, the aggregate API, and coverage limits.

See [TODO.md](TODO.md) for open work and the [archive](archived/README.md) for completed plans and validation records.
