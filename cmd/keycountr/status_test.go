package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestLiveStatusReplacesLineAndPreservesWarnings(t *testing.T) {
	var out bytes.Buffer
	s := statusOutput{out: &out, live: true}
	s.show("total=123", false)
	s.show("total=4", false)
	s.clear()
	out.WriteString("warning\n")
	s.show("total=5", false)
	s.show("total=5 state=stopped", true)
	want := "total=123\r\x1b[2Ktotal=4\r\x1b[2Kwarning\ntotal=5\r\x1b[2Ktotal=5 state=stopped\n"
	if out.String() != want {
		t.Fatalf("output = %q, want %q", out.String(), want)
	}
}

func TestQuietStatusOnlyPrintsFinalSnapshot(t *testing.T) {
	var out bytes.Buffer
	s := statusOutput{out: &out}
	s.show("total=1", false)
	s.show("total=2", false)
	s.clear()
	s.show("total=2 state=stopped", true)
	if out.String() != "total=2 state=stopped\n" {
		t.Fatalf("unexpected quiet output: %q", out.String())
	}
}

func TestLiveStatusFitsTerminalAndFinalSnapshotIsComplete(t *testing.T) {
	for _, columns := range []int{0, 1, 3, 12, 80} {
		var out bytes.Buffer
		s := statusOutput{out: &out, live: true, columns: func() int { return columns }}
		line := "total=123 state=counting"
		s.show(line, false)
		if out.Len() >= columns && columns > 1 {
			t.Fatalf("status wraps at width %d: %q", columns, out.String())
		}
		s.show(line, true)
		if !strings.HasSuffix(out.String(), line+"\n") {
			t.Fatalf("final snapshot truncated: %q", out.String())
		}
	}
}
