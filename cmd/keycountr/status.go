package main

import (
	"fmt"
	"io"
)

// Live status occupies one terminal line. Redirected output receives only the
// final status, so service logs and pipes never accumulate periodic snapshots.
type statusOutput struct {
	out     io.Writer
	live    bool
	active  bool
	columns func() int
}

func (s *statusOutput) clear() {
	if s.active {
		fmt.Fprint(s.out, "\r\x1b[2K")
		s.active = false
	}
}

func (s *statusOutput) show(line string, final bool) {
	if !s.live && !final {
		return
	}
	s.clear()
	if final {
		fmt.Fprintln(s.out, line)
		return
	}
	// Status fields are ASCII. Leave the last column unused to avoid terminal
	// wrapping, including after a resize; the final snapshot stays complete.
	if s.columns != nil {
		width := s.columns() - 1
		if width < 1 {
			width = 1
		}
		if len(line) > width {
			if width > 3 {
				line = line[:width-3] + "..."
			} else {
				line = line[:width]
			}
		}
	}
	fmt.Fprint(s.out, line)
	s.active = true
}
