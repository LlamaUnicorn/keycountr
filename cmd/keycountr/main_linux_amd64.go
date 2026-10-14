//go:build linux && amd64

package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"keycountr/internal/input"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "keycountr:", err)
		os.Exit(1)
	}
}

func run() error {
	// Keep the original command working; counting no longer needs an opt-in.
	flag.Bool("prototype", false, "compatibility flag; counting is now the default")
	duration := flag.Duration("duration", 0, "stop after this duration (default: until Ctrl+C)")
	var excluded []string
	flag.Func("exclude-device", "exclude an input interface by absolute /dev/input path (repeatable; prefer by-id)", func(path string) error {
		path = filepath.Clean(path)
		if !strings.HasPrefix(path, "/dev/input/") {
			return errors.New("expected an absolute path under /dev/input")
		}
		excluded = append(excluded, path)
		return nil
	})
	flag.Parse()
	if flag.NArg() != 0 || *duration < 0 {
		return errors.New("unexpected arguments or negative duration")
	}
	if os.Getuid() == 0 {
		return errors.New("run as the desktop owner, without sudo")
	}
	for _, tool := range []string{"udevadm"} {
		if _, err := exec.LookPath(tool); err != nil {
			return fmt.Errorf("required device tool unavailable: %s", tool)
		}
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if *duration > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, *duration)
		defer cancel()
	}
	unlock, err := instanceLock()
	if err != nil {
		return err
	}
	defer unlock()
	fmt.Println("Counting while running, including lock screens and other sessions when device access permits.")
	fmt.Println("Physical key-downs including modifiers; repeats and mouse buttons excluded. Ctrl+C stops. Totals reset on restart.")
	fmt.Println("If no input opens or unavailable is nonzero, run bash tools/stage1-probe.sh and see docs/STAGE1.md for access guidance.")
	devices := input.NewDevices(excluded)
	defer devices.Close()
	var total uint64
	state := "starting"
	var nextScan time.Time
	readTick := time.NewTicker(10 * time.Millisecond)
	display := time.NewTicker(time.Second)
	defer readTick.Stop()
	defer display.Stop()
	show := func() {
		c := devices.Coverage()
		fmt.Printf("total=%d state=%s keyboards=%d/%d unavailable=%d dropped=%d read_errors=%d\n",
			total, state, c.Open, c.Interfaces, c.Unavailable, c.Dropped, c.ReadErrors)
	}
	for {
		select {
		case <-ctx.Done():
			devices.Close()
			state = "stopped"
			show()
			return nil
		case <-display.C:
			show()
		case <-readTick.C:
			if ctx.Err() != nil {
				continue
			}
			if time.Now().After(nextScan) {
				devices.Refresh(ctx)
				nextScan = time.Now().Add(2 * time.Second)
				if ctx.Err() != nil {
					continue
				}
			}
			state = "counting"
			if devices.Coverage().Open == 0 {
				state = "no-input"
			}
			total += devices.Read()
		}
	}
}

func instanceLock() (func(), error) {
	dir := os.Getenv("XDG_RUNTIME_DIR")
	if dir == "" {
		dir = filepath.Join("/run/user", strconv.Itoa(os.Getuid()))
	}
	dirFD, err := syscall.Open(dir, syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_CLOEXEC|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil, errors.New("private runtime directory unavailable; set XDG_RUNTIME_DIR to a directory you own with mode 0700")
	}
	defer syscall.Close(dirFD)
	var stat syscall.Stat_t
	if err := syscall.Fstat(dirFD, &stat); err != nil || stat.Uid != uint32(os.Getuid()) || stat.Mode&0777 != 0700 {
		return nil, errors.New("runtime directory must belong to the current user with mode 0700")
	}
	fd, err := syscall.Openat(dirFD, "keycountr.lock", syscall.O_RDWR|syscall.O_CREAT|syscall.O_CLOEXEC|syscall.O_NOFOLLOW, 0600)
	if err != nil {
		return nil, errors.New("cannot open the private instance lock")
	}
	if err := syscall.Fstat(fd, &stat); err != nil || stat.Uid != uint32(os.Getuid()) || stat.Mode&0777 != 0600 || stat.Mode&syscall.S_IFMT != syscall.S_IFREG {
		syscall.Close(fd)
		return nil, errors.New("instance lock must be a private regular file owned by the current user")
	}
	if err := syscall.Flock(fd, syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		syscall.Close(fd)
		return nil, errors.New("another counter is running or the instance lock is unavailable")
	}
	return func() { syscall.Close(fd) }, nil
}
