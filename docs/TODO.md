# Backlog

Stages 1–3 and the terminal-output cleanup are complete. See the [archive](archived/README.md) for completed plans, checklists, and validation records. Current usage is in [README.md](README.md).

## Application lifecycle backlog — separate sessions

The binary supports collector-only, dashboard-only, and explicit combined (`--with-dashboard`) modes. See the [combined-mode record](archived/COMBINED_MODE.md). The following options are deferred for discussion and implementation in future sessions (2026-10-14).

- [ ] Decide whether combined mode should eventually become the default; collector-only is still the default.
- [ ] Discuss optional browser opening for combined mode, with an opt-out for headless/service use; opening a page must not be required for collection.
- [ ] Discuss a single optional user service running combined mode, so no terminal needs to remain open. Reuse the existing service documentation; installation/enabling remains an explicit user action.
- [ ] Discuss optional dashboard auto-refresh with a modest interval, preserving selected filters and distinguishing saved-data freshness from collector liveness.
- [ ] Discuss running the app in the tray bar with a popup tooltip showing current count. Click will open up a menu with 'dashboard' and 'quit'.
- [ ] 

## Hardware validation backlog

- [ ] Validate the split keyboard in both wired and Bluetooth modes, including switching modes and reconnecting; check for duplicate events across distinct composite interfaces.
- [ ] Validate two independent physical keyboards, including simultaneous presses of the same key, when hardware is available.

These checks were deferred at the user's request on 2026-10-14. They do not block the validated Stage 1 setup.

## Other follow-ups

- [ ] Manually verify dashboard layout and interaction in a browser.
- [ ] Extend keyboard codes beyond 255 if needed for media/vendor keys.
- [ ] Investigate a narrow portable device-permission fallback if another installation requires it.

## Next-session handoff

The collector and read-only dashboard can run separately or together with `--with-dashboard`. Combined mode initializes storage first, cancels both components when either stops, and flushes on shutdown. Dashboard startup failure prevents collection; later server failure stops collection and returns an error after the final flush. Reports still use saved data and manual refresh. Schema version 1 remains unchanged. The collector reuses one live terminal line; `--quiet` and redirected output omit periodic status. No service has been installed or enabled, and input permissions have not been changed. Keep hardware validation deferred unless requested. Follow [AGENTS.md](../AGENTS.md) for project constraints and checks.
