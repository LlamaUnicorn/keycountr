package input

import (
	"encoding/binary"
	"testing"
)

func event(typ, code uint16, value int32) []byte {
	e := make([]byte, 24)
	// Deliberately nonzero timestamp bytes: decoding must ignore these.
	for i := 0; i < 16; i++ {
		e[i] = 0xff
	}
	binary.NativeEndian.PutUint16(e[16:18], typ)
	binary.NativeEndian.PutUint16(e[18:20], code)
	binary.NativeEndian.PutUint32(e[20:24], uint32(value))
	return e
}

func TestPhysicalPresses(t *testing.T) {
	var f Filter
	var events []byte
	for _, e := range [][]byte{
		event(1, 42, 1),                  // shift
		event(1, 30, 1),                  // another key, simultaneous with shift
		event(1, 30, 2), event(1, 30, 2), // autorepeat
		event(1, 30, 1),                  // duplicate down, already held
		event(1, 30, 0), event(1, 30, 1), // new physical press
		event(1, 42, 0), event(1, 30, 0),
		event(1, 272, 1), event(1, 256, 1), // mouse/button namespace
		event(2, 0, 1), event(1, 0, 1), event(1, 30, -1),
	} {
		events = append(events, e...)
	}
	count, dropped := f.Feed(events)
	if count != 3 || dropped {
		t.Fatalf("count=%d dropped=%t", count, dropped)
	}
}

func TestFragmentedRecords(t *testing.T) {
	var f Filter
	data := append(event(1, 30, 1), event(1, 31, 1)...)
	var total uint64
	for _, b := range data {
		n, dropped := f.Feed([]byte{b})
		if dropped {
			t.Fatal("unexpected drop")
		}
		total += n
	}
	if total != 2 || len(f.pending) != 0 {
		t.Fatalf("count=%d remaining=%d", total, len(f.pending))
	}
}

func TestDroppedStreamDoesNotReplay(t *testing.T) {
	var f Filter
	data := append(event(0, 3, 0), event(1, 30, 1)...)
	n, dropped := f.Feed(data)
	if n != 0 || !dropped || len(f.pending) != 0 {
		t.Fatal("overrun must discard pending records")
	}
	// A reopened stream has no old held-key state or pending raw bytes.
	n, dropped = f.Feed(event(1, 30, 1))
	if n != 1 || dropped {
		t.Fatal("fresh stream did not resume")
	}
}

func TestHeldAtStartupAndReset(t *testing.T) {
	var f Filter
	bits := make([]byte, 32)
	bits[30/8] = 1 << (30 % 8)
	f.seed(bits)
	n, _ := f.Feed(event(1, 30, 1))
	if n != 0 {
		t.Fatal("invented a startup press for a held key")
	}
	f.Feed(event(1, 30, 0))
	n, _ = f.Feed(event(1, 30, 1))
	if n != 1 {
		t.Fatal("new press missing after release")
	}
	f.Feed(event(1, 31, 1)[:12])
	f.Reset()
	if len(f.pending) != 0 || f.down[30] {
		t.Fatal("closed stream retained input state")
	}
}

func TestSeparateKeyboards(t *testing.T) {
	var a, b Filter
	one, _ := a.Feed(event(1, 30, 1))
	two, _ := b.Feed(event(1, 30, 1))
	if one+two != 2 {
		t.Fatal("independent keyboards collapsed")
	}
}
