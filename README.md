# keycountr

A small Go application for Ubuntu that counts keyboard presses without recording typed text.

Status: a stage-1 foreground counter is runnable on Linux amd64. The user confirmed physical counting and shutdown with the default command. Device lifecycle and recovery have synthetic test coverage; physical edge cases and duplicate events across distinct interfaces remain under validation; no database or server has been implemented.

## Scope

- Target: Ubuntu desktop running Wayland; the evdev backend currently supports Linux amd64.
- Count from accessible keyboard interfaces whenever the process runs. Lock screens, text consoles, and other users' sessions are included when device access permits. There is no session monitoring or pause/resume logic.
- Stage 1: show a live, in-memory count in a terminal.
- Stage 2: save aggregate counts locally in SQLite.
- Stage 3: serve a local dashboard with hourly, daily, and weekly totals.

Count each physical key-down once, including modifiers, navigation, and function keys. Ignore releases, held-key repeats, mouse buttons, and other non-keyboard events. Two simultaneous presses count as two. Count across attached keyboard-class interfaces, subject to the code-range and duplicate-device gaps described in [STAGE1.md](STAGE1.md).

Privacy rule: never translate events into characters, reconstruct text, or persist/log key codes, individual event timestamps, device identifiers, window titles, or application names. Raw input stays at the input boundary and is discarded after filtering. Only totals leave that boundary. Password-entry presses contribute to the total, including on the lock screen, but their contents are never reconstructed. Stage 1 saves no counts to disk.

## Setup

Go 1.26.5 on linux/amd64 was available during setup. The module declares Go 1.26.0 and has no third-party dependencies. Its local module path is `keycountr`; change it to the repository URL if publishing it later.

Build and run as your ordinary user, without sudo:

```sh
go build -buildvcs=false -o bin/keycountr ./cmd/keycountr
./bin/keycountr
```

The counter prints a running total once per second. Wait for `state=counting`, then press keys, hold a key to test repeats, and click the mouse. Ctrl+C prints the final total and stops. Totals reset on restart. The previous `--prototype` flag is still accepted for compatibility; no flags are required to count.

`udevadm` supplies device classification. Neither D-Bus access nor a session ID is required. A private runtime directory (`$XDG_RUNTIME_DIR`, or `/run/user/UID`) holds the per-user instance lock. `-buildvcs=false` works around this workspace's nonfunctional `.git` metadata.

An optional read-only device-access diagnostic does not open input streams:

```sh
bash tools/stage1-probe.sh
```

See [STAGE1.md](STAGE1.md) for observations, manual checks, automated tests, and known counting gaps. Input permissions and services have not been changed.

See [PLAN.md](PLAN.md) for design decisions and [TODO.md](TODO.md) for the implementation checklist.
