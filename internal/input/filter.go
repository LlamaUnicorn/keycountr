package input

import "encoding/binary"

// Filter is private to one device stream. No event data crosses this boundary.
// This prototype supports the classic keyboard code range, 1..255.
type Filter struct {
	down    [256]bool
	pending []byte
}

// Feed consumes Linux amd64 input_event records, including fragmented reads.
// An overrun invalidates this stream: the caller must close and reopen it.
// Timestamps are never decoded. Only aggregate counts leave the filter.
func (f *Filter) Feed(data []byte) (presses uint64, dropped bool) {
	f.pending = append(f.pending, data...)
	for len(f.pending) >= 24 {
		e := f.pending[:24]
		typ := binary.NativeEndian.Uint16(e[16:18])
		code := binary.NativeEndian.Uint16(e[18:20])
		value := int32(binary.NativeEndian.Uint32(e[20:24]))
		clear(e)
		f.pending = f.pending[24:]
		if typ == 0 && code == 3 { // SYN_DROPPED: discard, then reopen.
			f.Reset()
			return presses, true
		}
		if typ != 1 || code == 0 || code >= 256 {
			continue
		}
		switch value {
		case 0:
			f.down[code] = false
		case 1:
			if !f.down[code] {
				presses++
			}
			f.down[code] = true
		}
	}
	return presses, false
}

func (f *Filter) Reset() {
	clear(f.pending)
	f.pending = nil
	clear(f.down[:])
}

func (f *Filter) seed(bits []byte) {
	for key := range f.down {
		f.down[key] = bits[key/8]&(1<<uint(key%8)) != 0
	}
}
