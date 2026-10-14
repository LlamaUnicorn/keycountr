//go:build linux && amd64

package main

import (
	"context"
	"crypto/rand"
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

	"github.com/mattn/go-isatty"
	"golang.org/x/sys/unix"

	"keycountr/internal/counter"
	"keycountr/internal/input"
	"keycountr/internal/storage"
	"keycountr/internal/web"
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
	quiet := flag.Bool("quiet", false, "disable live collector status updates (startup, warnings, and final total remain)")
	dataDir := flag.String("data-dir", "", "private storage directory (default: XDG_DATA_HOME/keycountr)")
	dashboard := flag.Bool("dashboard", false, "serve saved counts on loopback without collecting input")
	port := flag.Int("port", 8080, "dashboard loopback port (0 chooses an available port)")
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
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if *duration > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, *duration)
		defer cancel()
	}
	var err error
	dir := *dataDir
	if dir == "" {
		dir, err = storage.DataDir()
		if err != nil {
			return err
		}
	}
	if *dashboard {
		if len(excluded) != 0 {
			return errors.New("--exclude-device applies only to collection")
		}
		return web.Run(ctx, dir, *port, os.Stdout)
	}
	if _, err := exec.LookPath("udevadm"); err != nil {
		return errors.New("required device tool unavailable: udevadm")
	}
	unlock, err := instanceLock()
	if err != nil {
		return err
	}
	defer unlock()
	store, err := storage.Open(ctx, dir)
	if err != nil {
		return err
	}
	defer store.Close()
	total, err := store.Total(ctx)
	if err != nil {
		return err
	}
	sessionID := make([]byte, 16)
	if _, err := rand.Read(sessionID); err != nil {
		return errors.New("cannot create flush identifier")
	}
	buffer := counter.New(time.Now())
	var batch []counter.Minute
	var batchID string
	var sequence uint64
	storageOK := true
	flush := func() error {
		if len(batch) == 0 {
			batch = buffer.Rows()
			if len(batch) == 0 {
				return nil
			}
			sequence++
			batchID = fmt.Sprintf("%x-%d", sessionID, sequence)
		}
		// Independent of the canceled collector context: shutdown still writes.
		writeCtx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := store.Apply(writeCtx, batchID, batch); err != nil {
			return err
		}
		buffer.Acknowledge(batch)
		batch = nil
		return nil
	}
	fmt.Println("Counting while running, including lock screens and other sessions when device access permits.")
	fmt.Println("Physical key-downs including modifiers; repeats and mouse buttons excluded. Ctrl+C flushes and stops. Totals persist in local SQLite.")
	fmt.Println("If no input opens or unavailable is nonzero, run bash tools/stage1-probe.sh and see docs/INPUT.md for access guidance.")
	devices := input.NewDevices(excluded)
	defer devices.Close()
	state := "starting"
	var nextScan time.Time
	readTick := time.NewTicker(10 * time.Millisecond)
	status := statusOutput{out: os.Stdout, live: !*quiet && isatty.IsTerminal(os.Stdout.Fd()) && os.Getenv("TERM") != "dumb"}
	status.columns = func() int {
		if size, err := unix.IoctlGetWinsize(int(os.Stdout.Fd()), unix.TIOCGWINSZ); err == nil {
			return int(size.Col)
		}
		return 80
	}
	var displayTick <-chan time.Time
	if status.live {
		display := time.NewTicker(time.Second)
		defer display.Stop()
		displayTick = display.C
	}
	flushTick := time.NewTicker(5 * time.Second)
	defer flushTick.Stop()
	defer readTick.Stop()
	show := func(final bool) {
		c := devices.Coverage()
		storageState := "ok"
		if !storageOK {
			storageState = "retrying"
		}
		status.show(fmt.Sprintf("total=%d state=%s keyboards=%d/%d unavailable=%d dropped=%d read_errors=%d pending=%d storage=%s",
			total, state, c.Open, c.Interfaces, c.Unavailable, c.Dropped, c.ReadErrors, buffer.Pending(), storageState), final)
	}
	finish := func(cause error) error {
		devices.Close()
		if err := buffer.Observe(time.Now(), 0, false, 0); err != nil {
			cause = errors.Join(cause, err)
		}
		// Three bounded attempts, draining both a retried batch and newer deltas.
		for attempt := 0; attempt < 3; attempt++ {
			err := flush()
			storageOK = err == nil
			if err == nil && len(buffer.Rows()) == 0 {
				break
			}
		}
		state = "stopped"
		show(true)
		if len(buffer.Rows()) != 0 {
			return errors.Join(cause, errors.New("final flush failed; pending aggregates will be lost on exit"))
		}
		return cause
	}
	var previous input.Coverage
	for {
		select {
		case <-ctx.Done():
			return finish(nil)
		case <-flushTick.C:
			err := flush()
			if err != nil && storageOK {
				status.clear()
				fmt.Fprintln(os.Stderr, "Storage flush failed; retaining pending aggregates and retrying every five seconds.")
			}
			if err == nil && !storageOK {
				status.clear()
				fmt.Fprintln(os.Stderr, "Storage writes recovered.")
			}
			storageOK = err == nil
		case <-displayTick:
			show(false)
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
			presses := devices.Read()
			total += presses
			c := devices.Coverage()
			var issues int64
			if c.Open == 0 {
				issues |= counter.NoInput
			}
			if c.Unavailable > 0 {
				issues |= counter.Degraded
			}
			if c.Dropped > previous.Dropped || c.ReadErrors > previous.ReadErrors {
				issues |= counter.InputLoss
			}
			if err := buffer.Observe(time.Now(), presses, issues == 0, issues); err != nil {
				return finish(err)
			}
			previous = c
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
