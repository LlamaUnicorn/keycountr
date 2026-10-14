//go:build linux && amd64

package input

import (
	"context"
	"errors"
	"syscall"
	"testing"
)

type fakeNode struct {
	stat                      syscall.Stat_t
	held                      [32]byte
	data                      []byte
	readErr, openErr, heldErr error
}
type fakeIO struct {
	nodes                       map[string]*fakeNode
	order                       []string
	fds                         map[int]*fakeNode
	next, closed, metadataCalls int
	inventoryErr, metadataErr   error
	openedStat                  *syscall.Stat_t
}

func fixture() (*Devices, *fakeIO) {
	io := &fakeIO{nodes: map[string]*fakeNode{}, fds: map[int]*fakeNode{}}
	return &Devices{io: io}, io
}
func (f *fakeIO) add(path string, id uint64) *fakeNode {
	n := &fakeNode{stat: syscall.Stat_t{Dev: 1, Ino: id, Rdev: id, Mode: syscall.S_IFCHR}}
	f.nodes[path] = n
	f.order = append(f.order, path)
	return n
}
func (f *fakeIO) paths() ([]string, error) {
	var paths []string
	for _, p := range f.order {
		if f.nodes[p] != nil {
			paths = append(paths, p)
		}
	}
	return paths, f.inventoryErr
}
func (f *fakeIO) metadata(context.Context, string) (string, error) {
	f.metadataCalls++
	return "ID_INPUT_KEYBOARD=1", f.metadataErr
}
func (f *fakeIO) stat(p string) (syscall.Stat_t, error) {
	if n := f.nodes[p]; n != nil {
		return n.stat, nil
	}
	return syscall.Stat_t{}, syscall.ENOENT
}
func (f *fakeIO) open(p string) (int, error) {
	n := f.nodes[p]
	if n.openErr != nil {
		return 0, n.openErr
	}
	f.next++
	f.fds[f.next] = n
	return f.next, nil
}
func (f *fakeIO) fstat(fd int) (syscall.Stat_t, error) {
	if f.openedStat != nil {
		return *f.openedStat, nil
	}
	return f.fds[fd].stat, nil
}
func (f *fakeIO) held(fd int) ([32]byte, error) { n := f.fds[fd]; return n.held, n.heldErr }
func (f *fakeIO) read(fd int, b []byte) (int, error) {
	n := f.fds[fd]
	if n.readErr != nil {
		err := n.readErr
		n.readErr = nil
		return 0, err
	}
	if len(n.data) == 0 {
		return 0, syscall.EAGAIN
	}
	size := copy(b, n.data)
	n.data = n.data[size:]
	return size, nil
}
func (f *fakeIO) close(fd int) { delete(f.fds, fd); f.closed++ }

func TestDeviceHotplugAndReplacement(t *testing.T) {
	d, io := fixture()
	a := io.add("a", 1)
	b := io.add("b", 2)
	d.Refresh(context.Background())
	a.data, b.data = event(1, 30, 1), event(1, 30, 1)
	if n := d.Read(); n != 2 {
		t.Fatalf("independent devices: %d", n)
	}
	d.Refresh(context.Background())
	if io.metadataCalls != 2 || io.next != 2 {
		t.Fatal("unchanged devices reopened or reclassified")
	}
	old := d.open["a"]
	delete(io.nodes, "a")
	d.Refresh(context.Background())
	if d.Coverage().Open != 1 || io.closed != 1 || old.filter.down[30] {
		t.Fatal("unplug retained stream state")
	}
	replacement := io.add("a", 3)
	replacement.held[30/8] = 1 << (30 % 8)
	d.Refresh(context.Background())
	replacement.data = append(event(1, 30, 1), event(1, 30, 0)...)
	replacement.data = append(replacement.data, event(1, 30, 1)...)
	if n := d.Read(); n != 1 {
		t.Fatalf("held key at replug: %d", n)
	}
	// A reused inode alone is not sufficient to establish stream identity.
	replacement.stat.Rdev = 4
	d.Refresh(context.Background())
	if io.closed != 2 || io.next != 4 {
		t.Fatal("node replacement missed")
	}
	d.Close()
	d.Close()
	if len(io.fds) != 0 || io.closed != 4 {
		t.Fatal("shutdown leaked or closed descriptors twice")
	}
}

func TestDeviceDropRecovery(t *testing.T) {
	d, io := fixture()
	n := io.add("a", 1)
	d.Refresh(context.Background())
	old := d.open["a"]
	n.data = append(event(1, 30, 1), event(0, 3, 0)...)
	n.data = append(n.data, event(1, 31, 1)...)
	if count := d.Read(); count != 1 {
		t.Fatalf("count before drop: %d", count)
	}
	if d.Coverage().Dropped != 1 || d.Coverage().Open != 0 || len(old.filter.pending) != 0 {
		t.Fatal("drop did not discard stream")
	}
	n.held[31/8] = 1 << (31 % 8)
	d.Refresh(context.Background())
	n.data = append(event(1, 31, 1), event(1, 31, 0)...)
	n.data = append(n.data, event(1, 31, 1)...)
	if count := d.Read(); count != 1 {
		t.Fatalf("recovery invented or missed press: %d", count)
	}
	if d.Coverage().ReadErrors != 0 {
		t.Fatal("drop treated as read error")
	}
	d.Close()
}

func TestDeviceAliases(t *testing.T) {
	d, io := fixture()
	a := io.add("a", 1)
	alias := io.add("alias", 2)
	alias.stat.Rdev = a.stat.Rdev
	d.Refresh(context.Background())
	if d.Coverage().Open != 1 || d.Coverage().Interfaces != 2 {
		t.Fatal("same kernel device opened twice")
	}
	delete(io.nodes, "a")
	alias.held[30/8] = 1 << (30 % 8)
	d.Refresh(context.Background())
	alias.data = event(1, 30, 1)
	if d.Coverage().Open != 1 || d.Read() != 0 {
		t.Fatal("alias takeover failed or invented held press")
	}
	d.Close()
}

func TestExcludedCompositeInterface(t *testing.T) {
	d, io := fixture()
	a := io.add("keyboard", 1)
	b := io.add("composite", 2)
	alias := io.add("stable-alias", 3)
	alias.stat.Rdev = b.stat.Rdev
	d.excluded = []string{"stable-alias"}
	d.Refresh(context.Background())
	a.data, b.data = event(1, 30, 1), event(1, 30, 1)
	if d.Coverage().Open != 1 || d.Coverage().Interfaces != 1 || d.Read() != 1 {
		t.Fatal("excluded interface or its alias was counted")
	}
	// A stable alias follows the new kernel device number after reconnection.
	b.stat.Rdev, alias.stat.Rdev = 4, 4
	d.Refresh(context.Background())
	if d.Coverage().Open != 1 || io.next != 1 {
		t.Fatal("excluded reconnected interface opened")
	}
	delete(io.nodes, "composite")
	delete(io.nodes, "stable-alias")
	d.Refresh(context.Background())
	if d.Coverage().Open != 1 || d.Coverage().Unavailable != 0 {
		t.Fatal("disconnected exclusion interrupted keyboard")
	}
	d.Close()
}

func TestUnusableExclusionStopsCollection(t *testing.T) {
	d, io := fixture()
	io.add("keyboard", 1)
	d.Refresh(context.Background())
	io.add("invalid-exclusion", 2).stat.Mode = syscall.S_IFREG
	d.excluded = []string{"invalid-exclusion"}
	d.Refresh(context.Background())
	if d.Coverage().Open != 0 || d.Coverage().Unavailable != 1 || len(io.fds) != 0 {
		t.Fatal("unusable exclusion left input streams open")
	}
	d.Close()
}

func TestDeviceFailuresAndRetry(t *testing.T) {
	for _, kind := range []string{"open", "held", "replaced", "not-character"} {
		t.Run(kind, func(t *testing.T) {
			d, io := fixture()
			n := io.add("a", 1)
			switch kind {
			case "open":
				n.openErr = syscall.EACCES
			case "held":
				n.heldErr = syscall.ENODEV
			case "replaced":
				s := n.stat
				s.Ino++
				io.openedStat = &s
			case "not-character":
				s := n.stat
				s.Mode = syscall.S_IFREG
				io.openedStat = &s
			}
			d.Refresh(context.Background())
			if d.Coverage().Open != 0 || d.Coverage().Unavailable != 1 || len(io.fds) != 0 {
				t.Fatal("failed open leaked stream or lacked diagnostic")
			}
			n.openErr, n.heldErr, io.openedStat = nil, nil, nil
			d.Refresh(context.Background())
			if d.Coverage().Open != 1 || d.Coverage().Unavailable != 0 {
				t.Fatal("retry failed")
			}
			d.Close()
		})
	}
}

func TestDiscoveryFailurePreservesStreams(t *testing.T) {
	d, io := fixture()
	n := io.add("a", 1)
	d.Refresh(context.Background())
	io.inventoryErr = errors.New("inventory unavailable")
	d.Refresh(context.Background())
	if d.Coverage().Open != 1 || d.Coverage().Unavailable != 1 {
		t.Fatal("failed inventory closed working stream")
	}
	io.inventoryErr = nil
	io.metadataErr = errors.New("metadata unavailable")
	io.add("b", 2)
	d.Refresh(context.Background())
	n.data = event(1, 30, 1)
	if d.Read() != 1 || d.Coverage().Unavailable != 1 {
		t.Fatal("metadata failure interrupted working stream")
	}
	d.Close()
}

func TestDeviceReadErrors(t *testing.T) {
	for _, err := range []error{syscall.EINTR, syscall.ENODEV} {
		d, io := fixture()
		n := io.add("a", 1)
		d.Refresh(context.Background())
		n.readErr, n.data = err, event(1, 30, 1)
		count := d.Read()
		if err == syscall.EINTR {
			if count != 1 || d.Coverage().ReadErrors != 0 {
				t.Fatal("interrupted read lost progress")
			}
		} else {
			if count != 0 || d.Coverage().ReadErrors != 1 || d.Coverage().Open != 0 {
				t.Fatal("failed stream retained")
			}
			n.data = nil // a reopened evdev stream has no pre-open history
			d.Refresh(context.Background())
			n.data = event(1, 31, 1)
			if d.Read() != 1 {
				t.Fatal("read error retry failed")
			}
		}
		d.Close()
	}
}

func TestCancelledScanPreservesKnownDevices(t *testing.T) {
	d, io := fixture()
	io.add("a", 1)
	d.Refresh(context.Background())
	io.add("b", 2)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	d.Refresh(ctx)
	if d.Coverage().Open != 1 || d.Coverage().Interfaces != 1 || d.Coverage().Unavailable != 1 {
		t.Fatalf("cancelled scan coverage: %+v", d.Coverage())
	}
	if io.metadataCalls != 1 {
		t.Fatal("cancelled scan invoked metadata tool")
	}
	d.Close()
}
