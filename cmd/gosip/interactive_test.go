package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"
)

func newTestReader(input string) *lineReader {
	lr := newLineReader(strings.NewReader(input), nil, nil)
	lr.escWait = 20 * time.Millisecond
	return lr
}

func readTestIDNs(ctx context.Context, lr *lineReader, errOut io.Writer, fn func(idn uint32) error) lineStatus {
	return readLines(ctx, lr, "", errOut, func(line string) error {
		idn, err := parseIdn(line)
		if err != nil {
			return err
		}
		return fn(idn)
	})
}

func TestReadIDNsContinuesAfterErrors(t *testing.T) {
	var got []uint32
	var errOut bytes.Buffer
	st := readTestIDNs(context.Background(), newTestReader("S-0-0032.0.0\n\n  0x100A  \nnot-an-idn\n7\n"), &errOut, func(idn uint32) error {
		got = append(got, idn)
		if idn == 0x100A {
			return errors.New("boom")
		}
		return nil
	})
	if st != lineEOF {
		t.Fatalf("status = %v, want lineEOF", st)
	}
	want := []uint32{32, 0x100A, 7}
	if len(got) != len(want) {
		t.Fatalf("idns = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("idns = %v, want %v", got, want)
		}
	}
	if !strings.Contains(errOut.String(), "invalid IDN") || !strings.Contains(errOut.String(), "boom") {
		t.Fatalf("errOut = %q", errOut.String())
	}
}

func TestReadIDNsEscapeReturnsEscape(t *testing.T) {
	var got []uint32
	st := readTestIDNs(context.Background(), newTestReader("S-0-0032.0.0\n\x1b"), io.Discard, func(idn uint32) error {
		got = append(got, idn)
		return nil
	})
	if st != lineEscape {
		t.Fatalf("status = %v, want lineEscape", st)
	}
	if len(got) != 1 || got[0] != 32 {
		t.Fatalf("idns = %v, want [32]", got)
	}
}

func TestReadLineEditing(t *testing.T) {
	tests := []struct {
		input  string
		line   string
		status lineStatus
	}{
		{"ab\x7fc\r", "ac", lineOK},
		{"abc\x15xyz\r", "xyz", lineOK},
		{"ab\x1b[Acd\r", "abcd", lineOK},
		{"\x1b", "", lineEscape},
		{"\x03", "", lineEOF},
		{"\x04", "", lineEOF},
		{"S-0-0032.0.0", "S-0-0032.0.0", lineOK},
	}
	for _, tc := range tests {
		line, st := newTestReader(tc.input).ReadLine(context.Background(), "")
		if line != tc.line || st != tc.status {
			t.Errorf("input %q: got (%q, %v), want (%q, %v)", tc.input, line, st, tc.line, tc.status)
		}
	}
	lr := newTestReader("S-0-0032.0.0")
	lr.ReadLine(context.Background(), "")
	if _, st := lr.ReadLine(context.Background(), ""); st != lineEOF {
		t.Errorf("second ReadLine status = %v, want lineEOF", st)
	}
}

func TestReadIDNsStopsOnContextCancel(t *testing.T) {
	pr, pw := io.Pipe()
	defer pw.Close()
	lr := newLineReader(pr, nil, nil)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan lineStatus, 1)
	go func() {
		done <- readTestIDNs(ctx, lr, io.Discard, nil)
	}()
	time.Sleep(50 * time.Millisecond)
	cancel()
	select {
	case st := <-done:
		if st != lineEOF {
			t.Fatalf("status = %v, want lineEOF", st)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("readLines did not return after cancel")
	}
}

func TestParseReadLine(t *testing.T) {
	tests := []struct {
		line        string
		name        string
		idn         uint32
		errContains string
	}{
		{"S-0-0032.0.0", "readeverything", 32, ""},
		{"r 0x100A", "readeverything", 0x100A, ""},
		{"re 7", "readeverything", 7, ""},
		{"ro S-0-0032.0.0", "readonlydata", 32, ""},
		{"RD   7", "readdescription", 7, ""},
		{"rs 7", "readdatastate", 7, ""},
		{"readdatastate 7", "readdatastate", 7, ""},
		{"ro", "", 0, "missing IDN for readonlydata"},
		{"r", "", 0, "missing IDN for readeverything"},
		{"xx 7", "", 0, "unknown command"},
		{"ro 7 8", "", 0, "expected [command] IDN"},
		{"ro bogus", "", 0, "invalid IDN"},
		{"bogus", "", 0, "invalid IDN"},
	}
	for _, tc := range tests {
		t.Run(tc.line, func(t *testing.T) {
			rc, idn, err := parseReadLine(tc.line)
			if tc.errContains != "" {
				if err == nil || !strings.Contains(err.Error(), tc.errContains) {
					t.Fatalf("error = %v, want containing %q", err, tc.errContains)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseReadLine(%q): %v", tc.line, err)
			}
			if rc.name != tc.name || idn != tc.idn {
				t.Errorf("got (%q, %d), want (%q, %d)", rc.name, idn, tc.name, tc.idn)
			}
		})
	}
}

func TestReadLinesContinuesAfterErrors(t *testing.T) {
	var got []string
	var errOut bytes.Buffer
	st := readLines(context.Background(), newTestReader("ro 32\n\nxx 7\nrs 7\n"), "", &errOut, func(line string) error {
		rc, _, err := parseReadLine(line)
		if err != nil {
			return err
		}
		got = append(got, rc.name)
		return nil
	})
	if st != lineEOF {
		t.Fatalf("status = %v, want lineEOF", st)
	}
	if strings.Join(got, ",") != "readonlydata,readdatastate" {
		t.Errorf("commands = %v, want [readonlydata readdatastate]", got)
	}
	if !strings.Contains(errOut.String(), "unknown command") {
		t.Errorf("errOut = %q, want unknown command", errOut.String())
	}
}

func TestIsStatusLine(t *testing.T) {
	for _, line := range []string{"s", "S", "status", "Status"} {
		if !isStatusLine(line) {
			t.Errorf("isStatusLine(%q) = false, want true", line)
		}
	}
	for _, line := range []string{"ss", "re", "7", ""} {
		if isStatusLine(line) {
			t.Errorf("isStatusLine(%q) = true, want false", line)
		}
	}
}

func TestIsHelpLine(t *testing.T) {
	for _, line := range []string{"h", "H", "?", "help", "Help"} {
		if !isHelpLine(line) {
			t.Errorf("isHelpLine(%q) = false, want true", line)
		}
	}
	for _, line := range []string{"hh", "q", "", "status"} {
		if isHelpLine(line) {
			t.Errorf("isHelpLine(%q) = true, want false", line)
		}
	}
}

func TestIsQuitLine(t *testing.T) {
	for _, line := range []string{"q", "Q", "quit", "Quit"} {
		if !isQuitLine(line) {
			t.Errorf("isQuitLine(%q) = false, want true", line)
		}
	}
	for _, line := range []string{"qq", "h", "", "status"} {
		if isQuitLine(line) {
			t.Errorf("isQuitLine(%q) = true, want false", line)
		}
	}
}

func TestPrintPromptHelp(t *testing.T) {
	var out bytes.Buffer
	printPromptHelp(&out)
	for _, text := range []string{"[command] IDN", "re, r", "readonlydata", "readdescription", "readdatastate", "status", "h, ?", "q", "Esc, or Ctrl-D"} {
		if !strings.Contains(out.String(), text) {
			t.Errorf("help output missing %q: %s", text, out.String())
		}
	}
}

func TestReadLinesStopsOnQuit(t *testing.T) {
	var got []string
	var errOut bytes.Buffer
	status := readLines(context.Background(), newTestReader("q\n7\n"), "", &errOut, func(line string) error {
		if isQuitLine(line) {
			return errQuitInteractive
		}
		got = append(got, line)
		return nil
	})
	if status != lineEOF || len(got) != 0 || errOut.Len() != 0 {
		t.Fatalf("readLines = (%v, %v, %q), want (lineEOF, [], empty)", status, got, errOut.String())
	}
}

type fakeTimeouts struct {
	busy  time.Duration
	lease time.Duration
}

func (f *fakeTimeouts) BusyTimeout() time.Duration  { return f.busy }
func (f *fakeTimeouts) LeaseTimeout() time.Duration { return f.lease }

func TestTimeoutReporter(t *testing.T) {
	fake := &fakeTimeouts{}
	var out bytes.Buffer
	rep := newTimeoutReporter(fake, &out)
	rep.print()
	if out.String() != "not connected yet\n" {
		t.Fatalf("print before connect = %q", out.String())
	}
	out.Reset()
	fake.busy, fake.lease = 2*time.Second, 10*time.Second
	rep.print()
	want := "BusyTimeout: 2s\nLeaseTimeout: 10s\n"
	if out.String() != want {
		t.Fatalf("first print after connect = %q, want %q", out.String(), want)
	}
	rep.print()
	if out.String() != want+want {
		t.Errorf("second print after connect = %q, want %q", out.String(), want+want)
	}
}
