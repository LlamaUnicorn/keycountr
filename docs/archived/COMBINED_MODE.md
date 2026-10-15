# Combined collector and dashboard mode

Completed 2026-10-15. Current usage is in [README.md](../README.md) and
[DASHBOARD.md](../DASHBOARD.md).

- Added explicit `--with-dashboard`; collector-only remains the default and
  `--dashboard` remains independent and read-only. The two flags are mutually
  exclusive.
- Combined mode initializes storage and obtains collector locks before opening
  the dashboard reader and loopback listener. Input collection starts only after
  dashboard startup succeeds.
- Either component stopping cancels the other. Shutdown retains the bounded
  final flush and waits for dashboard cleanup before closing writer storage and
  releasing collector locks. Startup and runtime errors propagate to the CLI.
- Preserved loopback binding, Host/Origin checks, committed-only reports,
  single-line terminal status, quiet behavior, and private storage.

Validation: `go test ./...`, `go test -race ./...`, `go vet ./...`, and
`go build -buildvcs=false -o bin/keycountr ./cmd/keycountr` passed. Automated
coverage includes combined CLI duration/quiet shutdown with input classification
stubbed to exclude real devices, HTTP reads while synthetic collection runs,
final writes after cancellation, collector-error propagation, listener cleanup,
and occupied-port rejection before collection starts.

No physical keyboard or manual browser validation was performed. Browser
opening, service installation, auto-refresh, and changing the default mode
remain outside this change.
