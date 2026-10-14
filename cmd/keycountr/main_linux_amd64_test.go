//go:build linux && amd64

package main

import (
	"bytes"
	"flag"
	"os"
	"os/exec"
	"path/filepath"
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
	flag.CommandLine = flag.NewFlagSet("keycountr", flag.ExitOnError)
	main()
	os.Exit(0)
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
	t.Setenv("XDG_SESSION_ID", "")
	t.Setenv("DBUS_SESSION_BUS_ADDRESS", "unix:path=/nonexistent")
	t.Setenv("KEYCOUNTR_TEST_PROCESS", "yes")
	for _, legacy := range []string{"no", "yes"} {
		t.Run("legacy="+legacy, func(t *testing.T) {
			t.Setenv("KEYCOUNTR_TEST_LEGACY", legacy)
			cmd := exec.Command(os.Args[0], "-test.run=^TestCounterProcess$")
			output, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("CLI failed: %v\n%s", err, output)
			}
			if !bytes.Contains(output, []byte("state=stopped")) || strings.Contains(string(output), "paused") {
				t.Fatalf("unexpected lifecycle output: %s", output)
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
