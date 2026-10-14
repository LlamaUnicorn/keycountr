# Input counting and device access

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

The live terminal display reuses one line and clips long status to fit. `--quiet` and redirected output omit periodic updates; startup messages, warnings, and the complete final status remain. Output contains totals, state, interface counts, and coarse error/drop counts. `no-input` means no keyboard stream opened. `unavailable` indicates device discovery/open failures. The optional probe prints only keyboard-interface/readability counts and metadata success; exit 1 means discovery/access was insufficient, and exit 2 means invalid arguments. Readability does not by itself prove a stream can open.

On this host existing user permissions sufficed. If access fails elsewhere, investigate the current per-device ACLs before selecting a permissions method; no broad input-group change is prescribed. The app does not modify permissions. It continues counting through locking and switching sessions if its streams remain usable, but device removal or operating-system access changes can still create gaps. Logout can terminate a foreground terminal process normally; this is not an application pause mechanism.

## Counting and device handling

The counter selects udev keyboard-class interfaces, counts down events for classic keyboard codes 1..255, tracks held state independently per interface, and ignores releases, autorepeat, and button codes. Ordinary modifiers, navigation, and function keys are included. Extended key codes above 255 are excluded, including some media/vendor keys. These are the Stage 1 defaults. Separate composite interfaces can duplicate physical presses; explicit exclusion is available as described below. Events from distinct keyboards are never merged by timing or key code.

Streams are nonblocking and never exclusively grabbed. Discovery repeats approximately every two seconds. Each metadata scan has a shared one-second budget; unchanged open nodes bypass metadata queries. Failed/incomplete discovery preserves working streams. Node identity includes filesystem device, inode, and kernel device number, and is checked again after open to reject replacement races. Aliases with the same kernel device number open only one reader; separate keyboard interfaces remain independent. Startup/reopen seeds currently held keys via `EVIOCGKEY`, avoiding invented presses. `SYN_DROPPED` closes the stream and causes a later reopen/resynchronization, deliberately undercounting lost events; an aggregate drop counter records this. Physical power-off/on recovery is user-confirmed; real kernel-overrun behavior remains unverified. Discovery queries can still delay event processing by up to the scan budget, so very busy queues can overflow.

## Composite interfaces and access setup

The minimal access method on this host is the existing ordinary-user device access: no permission changes or helper are needed. The rebuilt counter opened both interfaces on 2026-10-14. Elsewhere, if `state=no-input` or `unavailable` stays nonzero, run the probe as the same user in the desktop terminal. If it finds no interfaces, check device connection and `udevadm` availability. If interfaces exist but are unreadable, inspect their existing ACLs locally (for example with `getfacl` if installed). If they are readable but cannot open, the failure may instead be device removal or a held-state query failure. The counter retries every scan. A portable permission fallback is deferred until an actual access failure can be investigated; no input-group membership, broad chmod, root collector, or service is required by this setup.

When a physical press consistently adds two, test interfaces separately before choosing an exclusion. Inspect `/dev/input/by-id/` locally and compare runs excluding one candidate at a time. Do not copy device inventories into logs or project files. Use an actual existing stable interface symlink in place of the example:

```sh
./bin/keycountr --exclude-device /dev/input/by-id/EXTRA-INTERFACE-event-kbd
```

The option is repeatable. It excludes the selected kernel event device and all its aliases, without logging the path. Stable symlinks are resolved at each discovery scan, including after reconnecting. A disconnected/missing exclusion path has no effect; check spelling and confirm the reduced `keyboards` count before relying on it. Other resolution failures or a path that is not a character device stop collection until the exclusion is usable. Direct `/dev/input/eventN` paths also work but can change assignments after reconnection; prefer stable paths. Exclusion state is not saved, so supply the flag on every run. An exclusion can remove unique keys as well as mirrored ones: verify ordinary keys, modifiers, and any supported extra keys on all retained keyboards. Hardware evidence is still needed before selecting a host-specific exclusion.

A private flock in the user's runtime directory prevents a second instance. The lock file contains no input or totals. Shutdown closes descriptors and discards pending input. Pending aggregates are flushed to SQLite on orderly shutdown; see [storage](STORAGE.md).

## Validation limits

- Basic ordinary-key, Shift, held-key repeat, and mouse click/scroll checks are user-confirmed. An exact three-tap delta was not supplied; retain that limit on the evidence.
- Physical power-off/on recovery is confirmed. Split-keyboard wired/Bluetooth mode switching, multiple independent physical keyboards, and composite-interface duplication checks are backlogged at the user's request.
- Dropped-stream recovery passes controlled fixtures; a real kernel overrun is optional further validation.
- Permission setup and aggregate output audit are complete for this host; investigate a narrow fallback only if another installation needs it.

Lock/unlock, user-switch, console-switch, and session-monitor failure checks are no longer required by the agreed scope.

See [the Stage 1 archive](archived/STAGE1.md) for implementation history and validation evidence, and [TODO.md](TODO.md) for outstanding hardware checks.
