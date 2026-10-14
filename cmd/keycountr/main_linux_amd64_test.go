//go:build linux && amd64

package main

import (
	"bytes"
	"context"
	"database/sql"
	"flag"
	"keycountr/internal/counter"
	"keycountr/internal/storage"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Run the real CLI in a child process so flag parsing and shutdown are tested
// without changing the test runner or accessing actual keyboard streams.
func TestCounterProcess(t *testing.T) {
	if os.Getenv("KEYCOUNTR_TEST_PROCESS") != "yes" {
		return
	}
	os.Args = []string{"keycountr", "--duration=50ms"}
	if os.Getenv("KEYCOUNTR_TEST_LEGACY") == "yes" {
		os.Args = append(os.Args, "--prototype")
	}
	if os.Getenv("KEYCOUNTR_TEST_LEGACY") == "excluded" {
		os.Args = append(os.Args, "--exclude-device=/dev/input/by-id/keycountr-test-excluded")
	}
	if os.Getenv("KEYCOUNTR_TEST_LEGACY") == "dashboard" {
		os.Args = append(os.Args, "--dashboard", "--port=0")
	}
	flag.CommandLine = flag.NewFlagSet("keycountr", flag.ExitOnError)
	main()
	os.Exit(0)
}

func TestDashboardWithoutInputToolsOrRuntimeLock(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("the CLI runs as an ordinary user")
	}
	home := t.TempDir()
	store, err := storage.Open(context.Background(), filepath.Join(home, "keycountr"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close() // The dashboard must coexist with this collector lock.
	t.Setenv("XDG_DATA_HOME", home)
	t.Setenv("XDG_RUNTIME_DIR", "/nonexistent")
	t.Setenv("PATH", "")
	t.Setenv("KEYCOUNTR_TEST_PROCESS", "yes")
	t.Setenv("KEYCOUNTR_TEST_LEGACY", "dashboard")
	cmd := exec.Command(os.Args[0], "-test.run=^TestCounterProcess$")
	output, err := cmd.CombinedOutput()
	if err != nil || !bytes.Contains(output, []byte("Dashboard: http://127.0.0.1:")) || bytes.Contains(output, []byte("state=counting")) {
		t.Fatalf("dashboard failed: %v %s", err, output)
	}
}

func TestCountingWithoutSessionServices(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("the CLI runs as an ordinary user")
	}
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	toolsDir := t.TempDir()
	// Exclude all real devices even if tests run on the host. Session tools
	// deliberately fail and leave evidence if the CLI tries to invoke them.
	if err := os.WriteFile(filepath.Join(toolsDir, "udevadm"), []byte("#!/bin/sh\necho ID_INPUT_KEYBOARD=0\n"), 0700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"loginctl", "gdbus"} {
		if err := os.WriteFile(filepath.Join(toolsDir, name), []byte("#!/bin/sh\necho called > \"$XDG_RUNTIME_DIR/session-tool-called\"\nexit 1\n"), 0700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", toolsDir)
	t.Setenv("XDG_RUNTIME_DIR", dir)
	dataHome := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dataHome)
	store, err := storage.Open(context.Background(), filepath.Join(dataHome, "keycountr"))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Apply(context.Background(), "fixture", []counter.Minute{{Start: 1800000000, Count: 23}}); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_SESSION_ID", "")
	t.Setenv("DBUS_SESSION_BUS_ADDRESS", "unix:path=/nonexistent")
	t.Setenv("KEYCOUNTR_TEST_PROCESS", "yes")
	for _, legacy := range []string{"no", "yes", "excluded"} {
		t.Run("legacy="+legacy, func(t *testing.T) {
			t.Setenv("KEYCOUNTR_TEST_LEGACY", legacy)
			cmd := exec.Command(os.Args[0], "-test.run=^TestCounterProcess$")
			output, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("CLI failed: %v\n%s", err, output)
			}
			if !bytes.Contains(output, []byte("total=23 state=stopped")) || !bytes.Contains(output, []byte("pending=0 storage=ok")) || strings.Contains(string(output), "paused") {
				t.Fatalf("unexpected lifecycle output: %s", output)
			}
			status := regexp.MustCompile(`^total=[0-9]+ state=(starting|counting|no-input|stopped) keyboards=[0-9]+/[0-9]+ unavailable=[0-9]+ dropped=[0-9]+ read_errors=[0-9]+ pending=[0-9]+ storage=(ok|retrying)$`)
			for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
				if strings.HasPrefix(line, "total=") && !status.MatchString(line) {
					t.Fatalf("status contains non-aggregate fields: %s", line)
				}
			}
			db, err := sql.Open("sqlite", "file:"+filepath.Join(dataHome, "keycountr", "counts.sqlite3"))
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			var coverage int
			if err := db.QueryRow("SELECT count(*) FROM minute_coverage WHERE running_ms > 0 AND (issues & 1) != 0").Scan(&coverage); err != nil || coverage == 0 {
				t.Fatalf("final coverage not persisted: %d %v", coverage, err)
			}
			if bytes.Contains(output, []byte("keycountr-test-excluded")) {
				t.Fatal("CLI disclosed an excluded device identifier")
			}
			if _, err := os.Stat(filepath.Join(dir, "session-tool-called")); !os.IsNotExist(err) {
				t.Fatal("CLI invoked a session service")
			}
		})
	}
}

func TestInstanceLock(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_RUNTIME_DIR", dir)
	unlock, err := instanceLock()
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	if another, err := instanceLock(); err == nil {
		another()
		t.Fatal("second instance acquired lock")
	}
}

func TestInstanceLockRejectsSymlink(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_RUNTIME_DIR", dir)
	if err := os.Symlink(filepath.Join(dir, "target"), filepath.Join(dir, "keycountr.lock")); err != nil {
		t.Fatal(err)
	}
	if unlock, err := instanceLock(); err == nil {
		unlock()
		t.Fatal("symlink lock accepted")
	}
	if _, err := os.Stat(filepath.Join(dir, "target")); !os.IsNotExist(err) {
		t.Fatal("symlink target was created")
	}
}

func TestFinalFlushFailureIsVisible(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("the CLI runs as an ordinary user")
	}
	runtimeDir := t.TempDir()
	os.Chmod(runtimeDir, 0700)
	dataHome := t.TempDir()
	toolsDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(toolsDir, "udevadm"), []byte("#!/bin/sh\necho ID_INPUT_KEYBOARD=0\n"), 0700); err != nil {
		t.Fatal(err)
	}
	store, err := storage.Open(context.Background(), filepath.Join(dataHome, "keycountr"))
	if err != nil {
		t.Fatal(err)
	}
	store.Close()
	db, err := sql.Open("sqlite", "file:"+filepath.Join(dataHome, "keycountr", "counts.sqlite3"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TRIGGER fail_coverage BEFORE INSERT ON minute_coverage BEGIN SELECT RAISE(ABORT, 'fixture'); END`); err != nil {
		t.Fatal(err)
	}
	db.Close()
	t.Setenv("PATH", toolsDir)
	t.Setenv("XDG_RUNTIME_DIR", runtimeDir)
	t.Setenv("XDG_DATA_HOME", dataHome)
	t.Setenv("KEYCOUNTR_TEST_PROCESS", "yes")
	cmd := exec.Command(os.Args[0], "-test.run=^TestCounterProcess$")
	output, err := cmd.CombinedOutput()
	if err == nil || !bytes.Contains(output, []byte("final flush failed")) || !bytes.Contains(output, []byte("storage=retrying")) {
		t.Fatalf("final write failure hidden: %v %s", err, output)
	}
}
