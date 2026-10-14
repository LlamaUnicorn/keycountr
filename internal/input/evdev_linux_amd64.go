//go:build linux && amd64

package input

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

type device struct {
	fd       int
	identity deviceIdentity
	filter   Filter
}

// Devices holds raw input only within the input package. Its public output is
// aggregate counts and coarse coverage diagnostics.
type Devices struct {
	io          deviceIO
	open        map[string]*device
	interfaces  int
	unavailable int
	readErrors  uint64
	dropped     uint64
}

type Coverage struct {
	Open, Interfaces, Unavailable int
	ReadErrors, Dropped           uint64
}

func (d *Devices) Coverage() Coverage {
	return Coverage{len(d.open), d.interfaces, d.unavailable, d.readErrors, d.dropped}
}

func (d *Devices) Close() {
	for path, dev := range d.open {
		d.remove(path, dev)
	}
	d.interfaces, d.unavailable = 0, 0
}

func (d *Devices) remove(path string, dev *device) {
	d.backend().close(dev.fd)
	dev.filter.Reset()
	delete(d.open, path)
}

// Refresh discovers udev keyboard-class interfaces without logging metadata.
// It never grabs devices or broadens access permissions. A scan has one shared
// time budget so a failing metadata tool cannot stall collection per device.
func (d *Devices) Refresh(ctx context.Context) {
	if d.open == nil {
		d.open = make(map[string]*device)
	}
	io := d.backend()
	ctx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	paths, err := io.paths()
	if err != nil {
		d.unavailable = 1
		return
	}
	d.interfaces, d.unavailable = 0, 0
	seen := make(map[string]bool)
	for _, path := range paths {
		// Keep existing streams on incomplete discovery; only remove nodes known
		// to have disappeared from a successful inventory.
		seen[path] = true
	}
	for path, dev := range d.open {
		if !seen[path] {
			d.remove(path, dev)
		}
	}
	for _, path := range paths {
		stat, err := io.stat(path)
		if err != nil {
			d.unavailable++
			if old := d.open[path]; old != nil {
				d.remove(path, old)
			}
			continue
		}
		identity := identityOf(stat)
		if old := d.open[path]; old != nil {
			if old.identity == identity {
				d.interfaces++
				continue
			}
			d.remove(path, old)
		}
		if ctx.Err() != nil {
			d.unavailable++
			continue
		}
		metadata, err := io.metadata(ctx, path)
		if err != nil {
			d.unavailable++
			continue
		}
		if !strings.Contains("\n"+metadata+"\n", "\nID_INPUT_KEYBOARD=1\n") {
			continue
		}
		d.interfaces++
		// Two names for the same kernel event device must not create two readers.
		// Distinct interfaces of a composite physical device remain independent.
		duplicate := false
		for _, dev := range d.open {
			if dev.identity.rdev == identity.rdev {
				duplicate = true
				break
			}
		}
		if duplicate {
			continue
		}
		fd, err := io.open(path)
		if err != nil {
			d.unavailable++
			continue
		}
		opened, err := io.fstat(fd)
		if err != nil || opened.Mode&syscall.S_IFMT != syscall.S_IFCHR || identityOf(opened) != identity {
			io.close(fd)
			d.unavailable++
			continue
		}
		dev := &device{fd: fd, identity: identity}
		bits, err := io.held(fd)
		if err != nil {
			clear(bits[:])
			io.close(fd)
			d.unavailable++
			continue
		}
		dev.filter.seed(bits[:])
		clear(bits[:])
		d.open[path] = dev
	}
}

// Kernel node identity catches replacement even when an inode is reused.
type deviceIdentity struct{ dev, inode, rdev uint64 }

func identityOf(s syscall.Stat_t) deviceIdentity {
	return deviceIdentity{s.Dev, s.Ino, s.Rdev}
}

// deviceIO keeps deterministic lifecycle fixtures independent of real input.
type deviceIO interface {
	paths() ([]string, error)
	metadata(context.Context, string) (string, error)
	stat(string) (syscall.Stat_t, error)
	open(string) (int, error)
	fstat(int) (syscall.Stat_t, error)
	held(int) ([32]byte, error)
	read(int, []byte) (int, error)
	close(int)
}

func (d *Devices) backend() deviceIO {
	if d.io == nil {
		d.io = systemIO{}
	}
	return d.io
}

type systemIO struct{}

func (systemIO) paths() ([]string, error) { return filepath.Glob("/dev/input/event*") }
func (systemIO) metadata(ctx context.Context, path string) (string, error) {
	return command(ctx, "udevadm", "info", "--query=property", "--name="+path)
}
func (systemIO) stat(path string) (s syscall.Stat_t, err error) { err = syscall.Stat(path, &s); return }
func (systemIO) open(path string) (int, error) {
	return syscall.Open(path, syscall.O_RDONLY|syscall.O_NONBLOCK|syscall.O_CLOEXEC|syscall.O_NOFOLLOW, 0)
}
func (systemIO) fstat(fd int) (s syscall.Stat_t, err error) { err = syscall.Fstat(fd, &s); return }
func (systemIO) held(fd int) (bits [32]byte, err error) {
	// EVIOCGKEY(32). Raw state stays in this package; timestamps are not decoded.
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), uintptr(0x80204518), uintptr(unsafe.Pointer(&bits[0])))
	if errno != 0 {
		err = errno
	}
	return
}
func (systemIO) read(fd int, b []byte) (int, error) { return syscall.Read(fd, b) }
func (systemIO) close(fd int)                       { syscall.Close(fd) }

// Read performs bounded, nonblocking reads. No key codes or event timestamps
// leave this method. Devices are reopened after stream loss; lost presses are
// reported as a gap rather than reconstructed.
func (d *Devices) Read() uint64 {
	var total uint64
	var buffer [24 * 64]byte
	for path, dev := range d.open {
		for batch := 0; batch < 4; batch++ {
			n, err := d.backend().read(dev.fd, buffer[:])
			if errors.Is(err, syscall.EINTR) {
				continue
			}
			if errors.Is(err, syscall.EAGAIN) {
				break
			}
			if err != nil || n == 0 {
				d.readErrors++
				d.remove(path, dev)
				break
			}
			count, dropped := dev.filter.Feed(buffer[:n])
			clear(buffer[:n])
			total += count
			if dropped {
				d.dropped++
				d.remove(path, dev)
				break
			}
		}
	}
	return total
}
