# Local dashboard

The dashboard uses Go `net/http`, embedded HTML/CSS,
and the existing SQLite schema. No frontend build or external assets are needed.

## Start and stop

From the repository root:

```sh
go build -buildvcs=false -o bin/keycountr ./cmd/keycountr
```

For a single process that collects input and serves the dashboard:

```sh
./bin/keycountr --with-dashboard
```

This initializes storage before starting the dashboard, then begins collection.
Ctrl+C, SIGTERM, or `--duration` stops both components and flushes pending
aggregates. The dashboard uses its own read-only database connection and shows
only committed data. If its reader or listener cannot start, collection does not
begin and the command exits with an error. A later HTTP server failure cancels
collection, performs the final flush, and exits with an error. A collector error
also stops the dashboard. The database and collector locks remain held until both
components stop. The browser does not open automatically.

Collector-only remains the default. For independent processes, run the collector
normally in one terminal (or use the previously configured optional user service).
In another terminal run:

```sh
./bin/keycountr --dashboard
```

With `--dashboard`, open <http://127.0.0.1:49371>. Ctrl+C or SIGTERM stops the dashboard independently
of the collector. With the collector stopped, the dashboard shows saved history.
It requires an existing private database initialized by the collector. It never
creates a directory, acquires the collector lock, migrates a schema, or writes
counts. It does not use input devices, `udevadm`, or the runtime lock directory.
Run as your ordinary user, without sudo.

`--dashboard` and `--with-dashboard` are mutually exclusive. Both accept `--port`
and `--data-dir`; `--quiet` suppresses live collector status in combined mode but
keeps the dashboard URL and final status visible.

Independent processes must select the same data directory. For a custom location:

```sh
./bin/keycountr --dashboard --data-dir=/absolute/private/directory --port=8081
```

`--port=0` chooses a free port and prints its URL. `--duration=30s` provides a
bounded run. The address is always IPv4 `127.0.0.1`; there is no LAN binding
option. Use the printed IP address: other HTTP Host values and different Origins
are rejected. Responses are uncached, with no third-party resources or CORS.

After an abrupt collector exit, SQLite may require hot-journal recovery. The
read-only dashboard cannot recover it; run the collector first. Failed reads
produce a visible error rather than replacing the report with zero.

## Reports

The initial view shows today and the preceding six days in the machine's local
timezone, grouped by day. The form offers start date, exclusive end date,
hour/day/week grouping, and a timezone such as `Asia/Riyadh` or `Europe/Berlin`.
`Local` selects the server machine's local timezone. Click **Apply / refresh**
to load a fresh consistent snapshot; the page does not auto-refresh. Cards and
table rows use a compact layout. Date range help, explanations of the totals, and
saved-data/coverage notes are collapsed by default; click their headings or use
Enter/Space while focused to expand them.

Date fields are blank by default: From defaults to six calendar days before
today and Until to tomorrow in the reporting timezone. Each page reload or
**Apply / refresh** recalculates blank dates, keeping the default view on the
latest seven days, including today. Explicit dates remain fixed across refreshes;
either boundary can be set independently. Clear a date to restore its moving
default. The effective range is shown below the form. Range limits still apply.

Summary cards show today's saved presses, the previous recorded day with its
date, and the average of the last seven recorded days before today. They use
the selected reporting timezone and all saved history independently of the date
filters. Days without presses (including coverage-only days) are skipped. Today
is excluded from the average; with fewer than seven earlier recorded days, the
average uses the available days and displays their number. With no earlier days,
the comparison and average show “No earlier recorded days.” These counts may
come from days with incomplete coverage. The terminal uses the same definitions,
in the machine's local timezone, and also includes pending presses.

- Only committed data is shown, including the saved part of the current minute.
  The normal save delay is five seconds; retries can extend it. All-time totals
  cover the entire database independently of the selected range.
- Start is included and end excluded. For October 14 only, choose October 14
  through October 15. Limits are 31 calendar days for hours and 366 for days or
  weeks. Supported dates are 1970–2099; exclusive January 1, 2100 is allowed.
  Nonexistent dates and historical boundaries with second-level timezone offsets
  are rejected because storage is minute-based.
- UTC minutes are grouped by the selected local calendar. Weeks start Monday;
  first/last weeks may contain only the selected portion. Changing timezone
  changes historical grouping without changing stored data.
- DST days may have 23 or 25 hours; repeated hours have distinct UTC-offset
  labels. Half-hour DST transitions and skipped dates follow actual elapsed time.
- The report read timestamp shows when the query ran. **Latest saved minute**
  is the latest bucket across counts and coverage, even outside the range. It is
  not an exact write timestamp or proof the collector is running. An empty
  database displays “No saved data yet.”

## Coverage

| Status | Meaning |
| --- | --- |
| covered | Saved running/healthy durations cover the elapsed period, without issue flags. Zero means observed zero activity. |
| impaired coverage | Running coverage is complete, but healthy duration is incomplete or issue flags are present. |
| partial coverage | Some elapsed time lacks saved running coverage, including startup, shutdown, gaps or pending writes. |
| no saved coverage | No saved running duration; zero cannot be interpreted as observed inactivity. |
| future | No elapsed duration to assess yet. |

Expected duration ends at report time for current periods and excludes future
minutes. Missing data may mean the collector was stopped, a crash lost pending
data, or a flush is pending. The schema cannot distinguish those causes or
reconstruct exact stopped intervals. Partial and impaired periods can undercount.
Even “covered” is a coarse estimate, not proof every physical press arrived.
Issue flags are explained on the page and in [STORAGE.md](STORAGE.md). Counts with
no coverage remain visible.

## Aggregate API

The same parameters work on `GET /api/report`, for example:

```text
http://127.0.0.1:49371/api/report?start=2026-10-01&end=2026-10-15&granularity=day&timezone=Asia%2FRiyadh
```

JSON contains range boundaries, timezone, grouping, query timestamp, latest saved
minute, range/all-time totals, and bucket boundaries, labels, counts, durations,
issue flags and status. Counts, coverage and totals use one SQLite read transaction.
The `activity` object contains `today`, `previous` (a `date` and `count`, or null),
`average` (or null), and `days` (the number of earlier days averaged, from 0 to 7).
These summary values share the same read transaction as the report.
No raw events, key codes, device or batch identifiers are exposed. Invalid queries
return 400; failed storage reads return 503.

See [the Stage 3 archive](archived/STAGE3.md) for implementation and validation history.
