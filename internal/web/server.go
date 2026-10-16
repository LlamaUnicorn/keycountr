package web

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"keycountr/internal/storage"
)

//go:embed dashboard.html
var page string
var dashboard = template.Must(template.New("dashboard").Parse(page))

type Source interface {
	Snapshot(context.Context, int64, int64) (storage.Snapshot, error)
}

func Handler(source Source, now func() time.Time) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" && r.URL.Path != "/api/report" {
			http.NotFound(w, r)
			return
		}
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", "GET")
			http.Error(w, "GET required", http.StatusMethodNotAllowed)
			return
		}
		current := now()
		params := r.URL.Query()
		q, err := ParseQuery(params.Get("start"), params.Get("end"), params.Get("timezone"), params.Get("granularity"), current)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
		defer cancel()
		snapshot, err := source.Snapshot(ctx, q.Start.Unix(), q.End.Unix())
		if err != nil {
			http.Error(w, "Saved data unavailable; retry or check collector storage.", http.StatusServiceUnavailable)
			return
		}
		report := Aggregate(q, snapshot, current)
		if r.URL.Path == "/api/report" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(report)
			return
		}
		var peak uint64
		for _, b := range report.Buckets {
			peak = max(peak, b.Count)
		}
		widths := make([]int, len(report.Buckets))
		if peak != 0 {
			for i, b := range report.Buckets {
				widths[i] = int(float64(b.Count) / float64(peak) * 100)
			}
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		dashboard.Execute(w, struct {
			Report
			From, To string
			Widths   []int
		}{report, params.Get("start"), params.Get("end"), widths})
	})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Reject hostile Host/Origin values, including DNS rebinding to loopback.
		host, _, err := net.SplitHostPort(r.Host)
		if err != nil || host != "127.0.0.1" {
			http.Error(w, "loopback host required", http.StatusForbidden)
			return
		}
		if origin := r.Header.Get("Origin"); origin != "" {
			u, err := url.Parse(origin)
			if err != nil || u.Scheme != "http" || u.Host != r.Host {
				http.Error(w, "same origin required", http.StatusForbidden)
				return
			}
		}
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; form-action 'self'; frame-ancestors 'none'; base-uri 'none'")
		mux.ServeHTTP(w, r)
	})
}

func Listen(port int) (net.Listener, error) {
	if port < 0 || port > 65535 {
		return nil, errors.New("dashboard port must be 0–65535")
	}
	return net.Listen("tcp4", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
}

func Run(ctx context.Context, dir string, port int, output io.Writer) error {
	wait, err := Start(ctx, dir, port, output)
	if err != nil {
		return err
	}
	return wait()
}

// Start opens the read-only database and listener before returning. Cancel ctx
// to stop serving, then call the returned wait function exactly once to join
// shutdown and release the reader. Startup output is written synchronously.
func Start(ctx context.Context, dir string, port int, output io.Writer) (wait func() error, err error) {
	reader, err := storage.OpenReader(ctx, dir)
	if err != nil {
		return nil, err
	}
	listener, err := Listen(port)
	if err != nil {
		reader.Close()
		return nil, errors.New("cannot listen on dashboard loopback port")
	}
	server := &http.Server{Handler: Handler(reader, time.Now), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 16 << 10}
	fmt.Fprintf(output, "Dashboard: http://%s — committed aggregates only; Ctrl+C stops this process.\n", listener.Addr())
	finished := make(chan error, 1)
	go func() {
		err := serve(ctx, server, listener)
		listener.Close()
		reader.Close()
		finished <- err
	}()
	return func() error { return <-finished }, nil
}

func serve(ctx context.Context, server *http.Server, listener net.Listener) error {
	defer server.Close()
	done := make(chan error, 1)
	go func() { done <- server.Serve(listener) }()
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdown); err != nil {
			server.Close()
			return err
		}
		err := <-done
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}
