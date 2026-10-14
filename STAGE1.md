# Stage 1 implementation record

The foreground counter runs whenever the process is running. On 2026-10-14 the user explicitly accepted counting across lock screens, text consoles, and other users' sessions when input access permits. Session/lock monitoring and its acceptance checks have been removed.

## Observed on 2026-10-14

- Ubuntu 26.04.1 LTS; Go 1.26.5 linux/amd64.
- GNOME Shell 50.1; Wayland on seat0, VT2. These describe the host, not runtime requirements for session gating.
- Two udev keyboard-class interfaces were accessible to the current user. A three-second smoke run opened both streams, queried held-key state, attempted nonblocking reads, and stopped with no errors.
- The user subsequently confirmed physical counting with both the original prototype command and the default command. The default run opened both keyboard-class interfaces, reported no unavailable devices, drops, or read errors, and printed its final total on Ctrl+C. Exact modifier/repeat/mouse deltas remain unverified. Only aggregate findings are recorded here; pasted typed input is not saved.
- The sandbox hides `/dev/input`, so actual keyboard checks run on the desktop host.

Device inventory included a keyboard interface and a composite mouse interface with keyboard capabilities. `kbd` handlers alone are insufficient for selection. udev classification also does not prove unique physical keyboards; duplicate-interface behavior still needs testing. Device identifiers and raw inventories are not saved in the project.

## Workflow

```sh
go build -buildvcs=false -o bin/keycountr ./cmd/keycountr
./bin/keycountr
# Optional fixed-duration smoke check:
./bin/keycountr --duration 3s
# Optional device-access diagnostic (no input events read):
bash tools/stage1-probe.sh
```

Run as the ordinary user without sudo. `--prototype` remains accepted for compatibility. There is no `--session` option or session discovery; neither `loginctl` nor `gdbus` is used. Device discovery requires `udevadm`.

Wait for `state=counting`; note the baseline total, then press one ordinary key three times, hold it once, press/release Shift once, and click/move the mouse. The expected delta is five. Ctrl+C itself can add Ctrl and C presses before stopping. Counting is global across accessible selected interfaces and independent of terminal focus. No events are translated into characters.

Output contains totals, state, interface counts, and coarse error/drop counts. `no-input` means no keyboard stream opened. `unavailable` indicates device discovery/open failures. The optional probe prints only keyboard-interface/readability counts and metadata success; exit 1 means discovery/access was insufficient, and exit 2 means invalid arguments. Readability does not by itself prove a stream can open.

On this host existing user permissions sufficed. If access fails elsewhere, investigate the current per-device ACLs before selecting a permissions method; no broad input-group change is prescribed. The app does not modify permissions. It continues counting through locking and switching sessions if its streams remain usable, but device removal or operating-system access changes can still create gaps. Logout can terminate a foreground terminal process normally; this is not an application pause mechanism.

## Counting and device handling

The counter selects udev keyboard-class interfaces, counts down events for classic keyboard codes 1..255, tracks held state independently per interface, and ignores releases, autorepeat, and button codes. Ordinary modifiers, navigation, and function keys are included. Extended key codes above 255 are excluded, including some media/vendor keys. Composite interfaces can still duplicate physical presses; deduplication remains unresolved.

Streams are nonblocking and never exclusively grabbed. Discovery repeats approximately every two seconds. Each metadata scan has a shared one-second budget; unchanged open nodes bypass metadata queries. Failed/incomplete discovery preserves working streams. Node identity includes filesystem device, inode, and kernel device number, and is checked again after open to reject replacement races. Aliases with the same kernel device number open only one reader; separate keyboard interfaces remain independent. Startup/reopen seeds currently held keys via `EVIOCGKEY`, avoiding invented presses. `SYN_DROPPED` closes the stream and causes a later reopen/resynchronization, deliberately undercounting lost events; an aggregate drop counter records this. Actual hotplug and overrun behavior still need hardware validation. Discovery queries can still delay event processing by up to the scan budget, so very busy queues can overflow.

A private flock in the user's runtime directory prevents a second instance. The lock file contains no input or totals. Shutdown closes descriptors and discards pending input. No database or service exists; totals reset on restart.

## Automated checks

```sh
go test -race ./...
go vet ./...
bash -n tools/stage1-probe.sh
```

Go tests cover fragmented records, simultaneous keys, repeats, duplicate down events, button exclusion, stream overrun, held-at-startup state, discarded pending state, separate keyboards, and instance protection. A CLI regression test runs with an empty session ID, a nonexistent session bus, and failing session tools; counting starts and stops without invoking those tools. It checks both default startup and the legacy `--prototype` command without reading real keyboards.

## Lifecycle validation on 2026-10-14

Deterministic backend fixtures validate removal/reconnection, independent simultaneous keyboards, a replaced node with a reused inode, open-time replacement races, kernel-device aliases and alias takeover, access/state-query failures with retries, interrupted reads, read failures, and shutdown cleanup. Dropped-stream recovery is exercised through `Devices.Read` and `Refresh`: trailing records are discarded, the new stream seeds held keys, and counting resumes after release without reconstructing missed presses. Incomplete or cancelled scans retain known working devices.

The updated build passed `go test -race ./...`, `go vet ./...`, and the probe shell syntax check. A host read-only probe reported two readable keyboard interfaces. A three-second host run of the rebuilt binary opened both, reported no unavailable devices, read errors, or drops, and shut down cleanly. Output contained only aggregate totals and coarse status. This smoke run did not validate physical press deltas, physical hotplug, or actual kernel queue overruns.

## Remaining stage-1 checks

- Confirm exact modifier/repeat/mouse deltas with the manual sequence above.
- Validate unplug/replug, multiple physical keyboards, and duplicate interfaces.
- Validate dropped-stream recovery with a controlled fixture or hardware test.
- Audit aggregate output and finalize portable permission guidance.

Lock/unlock, user-switch, console-switch, and session-monitor failure checks are no longer required by the agreed scope.

## API reference

[Linux input event codes](https://docs.kernel.org/input/event-codes.html) documents press/release/repeat values and dropped-event resynchronization.
