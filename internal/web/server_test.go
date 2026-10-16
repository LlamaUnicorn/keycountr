package web

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"keycountr/internal/counter"
	"keycountr/internal/storage"
)

type fixture struct{ fail bool }

func (f fixture) Snapshot(context.Context, int64, int64) (storage.Snapshot, error) {
	if f.fail {
		return storage.Snapshot{}, errors.New("sensitive fixture path")
	}
	return storage.Snapshot{Total: 42}, nil
}

type readyWriter struct{ ready chan string }

func (w readyWriter) Write(p []byte) (int, error) { w.ready <- string(p); return len(p), nil }

func TestContinuousCollectionAcrossMidnight(t *testing.T) {
	for _, zone := range []string{"UTC", "Asia/Riyadh", "America/New_York"} {
		t.Run(zone, func(t *testing.T) {
			loc, err := time.LoadLocation(zone)
			if err != nil {
				t.Fatal(err)
			}
			before := time.Date(2026, 10, 15, 23, 59, 59, 900000000, loc)
			after := before.Add(200 * time.Millisecond)
			buffer := counter.New(before)
			dir := t.TempDir()
			if err := os.Chmod(dir, 0700); err != nil {
				t.Fatal(err)
			}
			ctx := context.Background()
			store, err := storage.Open(ctx, dir)
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			reader, err := storage.OpenReader(ctx, dir)
			if err != nil {
				t.Fatal(err)
			}
			defer reader.Close()
			handler := Handler(reader, func() time.Time { return after })
			read := func() Report {
				t.Helper()
				response := httptest.NewRecorder()
				handler.ServeHTTP(response, httptest.NewRequest("GET", "http://127.0.0.1:8080/api/report?start=2026-10-15&end=2026-10-17&timezone="+zone, nil))
				var report Report
				if response.Code != 200 {
					t.Fatal(response.Code, response.Body.String())
				}
				if err := json.Unmarshal(response.Body.Bytes(), &report); err != nil {
					t.Fatal(err)
				}
				return report
			}
			for i, observation := range []struct {
				at    time.Time
				count uint64
			}{{before, 2}, {after, 3}} {
				if err := buffer.Observe(observation.at, observation.count, true, 0); err != nil {
					t.Fatal(err)
				}
				rows := buffer.Rows()
				if err := store.Apply(ctx, observation.at.String(), rows); err != nil {
					t.Fatal(err)
				}
				buffer.Acknowledge(rows)
				report := read()
				wantToday := uint64(i * 3)
				if len(report.Buckets) != 2 || report.Buckets[0].Label != "2026-10-15" || report.Buckets[1].Label != "2026-10-16" || report.Buckets[0].Count != 2 || report.Buckets[1].Count != wantToday || report.Total != 2+wantToday || report.LifetimeTotal != 2+wantToday {
					t.Fatalf("midnight report: %+v", report)
				}
			}
		})
	}
}

func TestRunServesSavedDataAndShutsDown(t *testing.T) {
	dir := t.TempDir()
	os.Chmod(dir, 0700)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s, err := storage.Open(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	minute := time.Now().UTC().Truncate(time.Minute).Unix()
	if err := s.Apply(ctx, "http-fixture", []counter.Minute{{Start: minute, Count: 12, RunningMS: 1000, HealthyMS: 1000}}); err != nil {
		t.Fatal(err)
	}
	ready := make(chan string, 1)
	done := make(chan error, 1)
	go func() { done <- Run(ctx, dir, 0, readyWriter{ready}) }()
	var address string
	select {
	case message := <-ready:
		address = strings.Fields(message)[1]
	case err := <-done:
		t.Fatal(err)
	case <-time.After(3 * time.Second):
		t.Fatal("server did not start")
	}
	client := http.Client{Timeout: 2 * time.Second}
	for _, path := range []string{"/?timezone=UTC", "/api/report?timezone=UTC"} {
		response, err := client.Get(address + path)
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(response.Body)
		response.Body.Close()
		if err != nil || response.StatusCode != 200 {
			t.Fatalf("HTTP: %v %d %s", err, response.StatusCode, body)
		}
		if strings.HasPrefix(path, "/api") {
			var r Report
			if err := json.Unmarshal(body, &r); err != nil || r.Total != 12 || r.LifetimeTotal != 12 || r.LatestMinute == nil {
				t.Fatal(r, err)
			}
		} else if !strings.Contains(string(body), "width:100%") || strings.Contains(string(body), "ZgotmplZ") {
			t.Fatal(string(body))
		}
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("server did not stop")
	}
}

func TestHTTPReportsAndPrivacy(t *testing.T) {
	handler := Handler(fixture{}, func() time.Time { return time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC) })
	for _, path := range []string{"/", "/api/report"} {
		req := httptest.NewRequest("GET", "http://127.0.0.1:8080"+path+"?timezone=UTC", nil)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, req)
		if response.Code != 200 {
			t.Fatal(response.Code, response.Body.String())
		}
		if response.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("cached private report")
		}
		if path == "/" {
			if !strings.Contains(response.Body.String(), "No saved data yet") || !strings.Contains(response.Body.String(), "width:0%") || strings.Contains(response.Body.String(), "ZgotmplZ") {
				t.Fatal(response.Body.String())
			}
		} else {
			var report Report
			if err := json.Unmarshal(response.Body.Bytes(), &report); err != nil || report.LifetimeTotal != 42 || len(report.Buckets) != 7 {
				t.Fatal(report, err)
			}
		}
	}
	for _, tc := range []struct {
		method, target, origin string
		code                   int
	}{
		{"GET", "http://evil.test:8080/", "", 403}, {"GET", "http://127.0.0.1:8080/", "https://evil.test", 403},
		{"POST", "http://127.0.0.1:8080/", "", 405}, {"GET", "http://127.0.0.1:8080/?granularity=raw", "", 400},
		{"GET", "http://127.0.0.1:8080/unknown", "", 404},
	} {
		req := httptest.NewRequest(tc.method, tc.target, nil)
		req.Header.Set("Origin", tc.origin)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, req)
		if response.Code != tc.code {
			t.Fatal(tc, response.Code)
		}
	}
	response := httptest.NewRecorder()
	Handler(fixture{true}, time.Now).ServeHTTP(response, httptest.NewRequest("GET", "http://127.0.0.1:8080/", nil))
	if response.Code != 503 || strings.Contains(response.Body.String(), "sensitive") {
		t.Fatal(response.Body.String())
	}
}

func TestDateDefaultsStayRelativeAcrossRefreshes(t *testing.T) {
	loc, err := time.LoadLocation("Asia/Riyadh")
	if err != nil {
		t.Fatal(err)
	}
	before := time.Date(2026, 10, 15, 23, 59, 59, 0, loc)
	for _, tc := range []struct {
		name, from, to string
		starts, ends   [2]string
	}{
		{"defaults", "", "", [2]string{"2026-10-09", "2026-10-10"}, [2]string{"2026-10-16", "2026-10-17"}},
		{"fixed start", "2026-10-14", "", [2]string{"2026-10-14", "2026-10-14"}, [2]string{"2026-10-16", "2026-10-17"}},
		{"fixed end", "", "2026-10-20", [2]string{"2026-10-09", "2026-10-10"}, [2]string{"2026-10-20", "2026-10-20"}},
		{"fixed range", "2026-10-14", "2026-10-20", [2]string{"2026-10-14", "2026-10-14"}, [2]string{"2026-10-20", "2026-10-20"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			current := before
			handler := Handler(fixture{}, func() time.Time { return current })
			params := url.Values{"start": {tc.from}, "end": {tc.to}, "timezone": {loc.String()}, "granularity": {"day"}}
			for i, instant := range []time.Time{before, before.Add(2 * time.Second)} {
				current = instant
				for _, path := range []string{"/", "/api/report"} {
					response := httptest.NewRecorder()
					handler.ServeHTTP(response, httptest.NewRequest("GET", "http://127.0.0.1:8080"+path+"?"+params.Encode(), nil))
					if response.Code != 200 {
						t.Fatal(response.Code, response.Body.String())
					}
					if path == "/" {
						body := response.Body.String()
						// Submitting the rendered form must preserve blank defaults,
						// rather than freezing the dates calculated for this request.
						for name, value := range map[string]string{"start": tc.from, "end": tc.to} {
							if !strings.Contains(body, `name="`+name+`" value="`+value+`">`) {
								t.Fatalf("date field %s did not preserve %q", name, value)
							}
						}
						if !strings.Contains(body, "Showing "+tc.starts[i]+" (included) through "+tc.ends[i]+" (excluded)") {
							t.Fatal("effective range missing or stale")
						}
						continue
					}
					var report Report
					if err := json.Unmarshal(response.Body.Bytes(), &report); err != nil {
						t.Fatal(err)
					}
					if report.Start.In(loc).Format("2006-01-02") != tc.starts[i] || report.End.In(loc).Format("2006-01-02") != tc.ends[i] {
						t.Fatalf("unexpected range: %s through %s", report.Start, report.End)
					}
				}
			}
		})
	}
}

func TestLoopbackListener(t *testing.T) {
	listener, err := Listen(0)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	addr := listener.Addr().(*net.TCPAddr)
	if !addr.IP.Equal(net.ParseIP("127.0.0.1")) || addr.Port == 0 {
		t.Fatal(addr)
	}
	if listener, err := Listen(-1); err == nil {
		listener.Close()
		t.Fatal("invalid port accepted")
	}
}
