package web

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
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
