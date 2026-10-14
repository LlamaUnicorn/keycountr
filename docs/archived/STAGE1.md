# Stage 1 implementation record

The foreground counter runs whenever the process is running. On 2026-10-14 the user explicitly accepted counting across lock screens, text consoles, and other users' sessions when input access permits. Session/lock monitoring and its acceptance checks have been removed.

## Observed on 2026-10-14

- Ubuntu 26.04.1 LTS; Go 1.26.5 linux/amd64.
- GNOME Shell 50.1; Wayland on seat0, VT2. These describe the host, not runtime requirements for session gating.
- Two udev keyboard-class interfaces were accessible to the current user. A three-second smoke run opened both streams, queried held-key state, attempted nonblocking reads, and stopped with no errors.
- The user subsequently confirmed physical counting with both the original prototype command and the default command. The default run opened both keyboard-class interfaces, reported no unavailable devices, drops, or read errors, and printed its final total on Ctrl+C. At that point exact modifier/repeat/mouse deltas were unverified; subsequent checks are recorded below. Only aggregate findings are recorded here; pasted typed input is not saved.
- The sandbox hides `/dev/input`, so actual keyboard checks run on the desktop host.

Device inventory included a keyboard interface and a composite mouse interface with keyboard capabilities. `kbd` handlers alone are insufficient for selection. udev classification also does not prove unique physical keyboards; duplicate-interface behavior still needs testing. Device identifiers and raw inventories are not saved in the project.

## Workflow

Run these commands from the repository root.

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

The counter selects udev keyboard-class interfaces, counts down events for classic keyboard codes 1..255, tracks held state independently per interface, and ignores releases, autorepeat, and button codes. Ordinary modifiers, navigation, and function keys are included. Extended key codes above 255 are excluded, including some media/vendor keys. These are the Stage 1 defaults. Separate composite interfaces can duplicate physical presses; explicit exclusion is available as described below. Events from distinct keyboards are never merged by timing or key code.

Streams are nonblocking and never exclusively grabbed. Discovery repeats approximately every two seconds. Each metadata scan has a shared one-second budget; unchanged open nodes bypass metadata queries. Failed/incomplete discovery preserves working streams. Node identity includes filesystem device, inode, and kernel device number, and is checked again after open to reject replacement races. Aliases with the same kernel device number open only one reader; separate keyboard interfaces remain independent. Startup/reopen seeds currently held keys via `EVIOCGKEY`, avoiding invented presses. `SYN_DROPPED` closes the stream and causes a later reopen/resynchronization, deliberately undercounting lost events; an aggregate drop counter records this. Actual hotplug and overrun behavior still need hardware validation. Discovery queries can still delay event processing by up to the scan budget, so very busy queues can overflow.

## Composite interfaces and access setup

The minimal access method on this host is the existing ordinary-user device access: no permission changes or helper are needed. The rebuilt counter opened both interfaces on 2026-10-14. Elsewhere, if `state=no-input` or `unavailable` stays nonzero, run the probe as the same user in the desktop terminal. If it finds no interfaces, check device connection and `udevadm` availability. If interfaces exist but are unreadable, inspect their existing ACLs locally (for example with `getfacl` if installed). If they are readable but cannot open, the failure may instead be device removal or a held-state query failure. The counter retries every scan. A portable permission fallback is deferred until an actual access failure can be investigated; no input-group membership, broad chmod, root collector, or service is required by this setup.

When a physical press consistently adds two, test interfaces separately before choosing an exclusion. Inspect `/dev/input/by-id/` locally and compare runs excluding one candidate at a time. Do not copy device inventories into logs or project files. Use an actual existing stable interface symlink in place of the example:

```sh
./bin/keycountr --exclude-device /dev/input/by-id/EXTRA-INTERFACE-event-kbd
```

The option is repeatable. It excludes the selected kernel event device and all its aliases, without logging the path. Stable symlinks are resolved at each discovery scan, including after reconnecting. A disconnected/missing exclusion path has no effect; check spelling and confirm the reduced `keyboards` count before relying on it. Other resolution failures or a path that is not a character device stop collection until the exclusion is usable. Direct `/dev/input/eventN` paths also work but can change assignments after reconnection; prefer stable paths. Exclusion state is not saved, so supply the flag on every run. An exclusion can remove unique keys as well as mirrored ones: verify ordinary keys, modifiers, and any supported extra keys on all retained keyboards. Hardware evidence is still needed before selecting a host-specific exclusion.

A private flock in the user's runtime directory prevents a second instance. The lock file contains no input or totals. Shutdown closes descriptors and discards pending input. No database or service exists; totals reset on restart.

## Automated checks

```sh
go test -race ./...
go vet ./...
bash -n tools/stage1-probe.sh
```

Go tests also cover explicit exclusion of an interface and its aliases, stable-alias reconnection, disconnected exclusions, and rejection of unusable exclusions. The subprocess check verifies aggregate status fields and that an excluded device identifier is not printed.

Go tests cover fragmented records, simultaneous keys, repeats, duplicate down events, button exclusion, stream overrun, held-at-startup state, discarded pending state, separate keyboards, and instance protection. A CLI regression test runs with an empty session ID, a nonexistent session bus, and failing session tools; counting starts and stops without invoking those tools. It checks both default startup and the legacy `--prototype` command without reading real keyboards.

## Lifecycle validation on 2026-10-14

Deterministic backend fixtures validate removal/reconnection, independent simultaneous keyboards, a replaced node with a reused inode, open-time replacement races, kernel-device aliases and alias takeover, access/state-query failures with retries, interrupted reads, read failures, and shutdown cleanup. Dropped-stream recovery is exercised through `Devices.Read` and `Refresh`: trailing records are discarded, the new stream seeds held keys, and counting resumes after release without reconstructing missed presses. Incomplete or cancelled scans retain known working devices.

The updated build passed `go test -race ./...`, `go vet ./...`, and the probe shell syntax check. A host read-only probe reported two readable keyboard interfaces. A three-second host run of the rebuilt binary opened both, reported no unavailable devices, read errors, or drops, and shut down cleanly. Output contained only aggregate totals and coarse status. This smoke run did not validate physical press deltas, physical hotplug, or actual kernel queue overruns.

## Validation limits and hardware backlog

- Basic ordinary-key, Shift, held-key repeat, and mouse click/scroll checks are user-confirmed. An exact three-tap delta was not supplied; retain that limit on the evidence.
- Physical power-off/on recovery is confirmed. Split-keyboard wired/Bluetooth mode switching, multiple independent physical keyboards, and composite-interface duplication checks are backlogged at the user's request.
- Dropped-stream recovery passes controlled fixtures; a real kernel overrun is optional further validation.
- Permission setup and aggregate output audit are complete for this host; investigate a narrow fallback only if another installation needs it.

Lock/unlock, user-switch, console-switch, and session-monitor failure checks are no longer required by the agreed scope.

## API reference

[Linux input event codes](https://docs.kernel.org/input/event-codes.html) documents press/release/repeat values and dropped-event resynchronization.

## Continuation validation on 2026-10-14

Rebuilt `bin/keycountr` with the interface exclusion option. Race tests, vet, and shell syntax checks passed. The host probe reported `keyboard_interfaces=2 readable=2 metadata_ok=true`; a three-second run opened both interfaces with zero unavailable devices, drops, or read errors. No physical presses occurred during that run, so it does not establish exact deltas. Source and smoke-output audits found only fixed messages, aggregate status, and sanitized operational errors; input records and metadata are not printed or written. The runtime lock contains no input data. Exact deltas and physical hotplug/multiple-keyboard checks remain pending.

The user subsequently confirmed that physical Shift presses counted in a run of the rebuilt binary. Both interfaces stayed open, with no unavailable devices, drops, or read errors. Ctrl+C stopped cleanly; the final total increased by two, consistent with Ctrl and C down events. The exact physical sequence was not recorded, so this confirms basic modifier handling without establishing repeat/mouse deltas or absence of composite mirroring. Typed terminal echo from the supplied output is not saved here.

In a further user run, holding one letter for three seconds produced exactly +1, validating repeat exclusion on the physical keyboard. Powering the keyboard off caused open-interface coverage to fall from 2/2 to 1/2 and then 1/1, with one cumulative read error. Powering it on restored 2/2, and subsequent presses increased the total. No drops or unavailable devices were reported, and the read-error count remained at one after recovery. This validates physical stream removal and reopening; it does not establish behavior for a second physical keyboard. The interface count is not a count of physical keyboards.

The user confirmed that mouse clicks and scrolling added no counts, validating their exclusion on the target desktop. Multiple-keyboard behavior remains to be checked; the exact three-tap delta was not supplied.

The user further confirmed that multiple ordinary-key presses between display ticks were counted. Display updates do not reset or gate event accumulation. This completes the basic manual counting checks alongside Shift, the exact +1 held-key result, and mouse click/scroll exclusion. No numeric three-tap result was supplied, so none is inferred. Physical multiple-keyboard behavior remains unverified.

The user identified the keyboard as a split model supporting wired and Bluetooth modes and requested that further hardware validation be backlogged. Stage 1 is complete for the validated setup; wired/Bluetooth switching and independent multiple-keyboard checks remain documented follow-ups, not completed tests.
