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

func TestReadLineHistory(t *testing.T) {
	tests := []struct {
		name  string
		input string
		at    int
		want  string
	}{
		{"recall older entry", "ro 7\rw 1 2\r\x1b[A\x1b[A\r", 2, "ro 7"},
		{"down moves forward", "a\rb\r\x1b[A\x1b[A\x1b[B\r", 2, "b"},
		{"down restores draft", "z\rxy\x1b[A\x1b[B\r", 1, "xy"},
		{"duplicate suppression", "a\rb\rb\r\x1b[A\x1b[A\r", 3, "a"},
		{"blank lines are omitted", "a\r\r\x1b[A\r", 2, "a"},
		{"SS3 arrows", "a\r\x1bOA\r", 1, "a"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			lr := newTestReader(tc.input)
			for i := range tc.at + 1 {
				line, status := lr.ReadLine(context.Background(), "")
				if status != lineOK {
					t.Fatalf("ReadLine %d status = %v, want lineOK", i, status)
				}
				if i == tc.at && line != tc.want {
					t.Fatalf("ReadLine %d = %q, want %q", i, line, tc.want)
				}
			}
		})
	}
}

func TestReadLineCursorEditing(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"abc\x1b[D\x1b[Dx\r", "axbc"},
		{"abc\x1b[Hx\r", "xabc"},
		{"abc\x1b[H\x1b[3~\r", "bc"},
		{"abc\x1b[D\x7f\r", "ac"},
		{"abc\x01x\x05y\r", "xabcy"},
		{"abc\x1b[1;5Dx\r", "abxc"},
	}
	for _, tc := range tests {
		line, status := newTestReader(tc.input).ReadLine(context.Background(), "")
		if status != lineOK || line != tc.want {
			t.Errorf("input %q: got (%q, %v), want (%q, lineOK)", tc.input, line, status, tc.want)
		}
	}
}

func TestReadLinePaste(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"\x1b[200~ro\nS-0-0001\x1b[201~\r", "ro S-0-0001"},
		{"a\x1b[200~b\r\nc\td\x1b[201~e\r", "ab c de"},
		{"ac\x1b[D\x1b[200~b\x1b[201~\r", "abc"},
		{"\x1b[200~a\xc3\xa9b\x1b[201~\r", "ab"},
	}
	for _, tc := range tests {
		line, status := newTestReader(tc.input).ReadLine(context.Background(), "")
		if status != lineOK || line != tc.want {
			t.Errorf("input %q: got (%q, %v), want (%q, lineOK)", tc.input, line, status, tc.want)
		}
	}
}

func TestReadLineTerminalOutput(t *testing.T) {
	var out bytes.Buffer
	lr := newLineReader(strings.NewReader("ro 7\r\x1b[A\r"), &out, func() (func(), error) {
		return func() {}, nil
	})
	for i := range 2 {
		if _, status := lr.ReadLine(context.Background(), "gosip> "); status != lineOK {
			t.Fatalf("ReadLine %d status = %v, want lineOK", i, status)
		}
	}
	got := out.String()
	for _, want := range []string{"gosip> \x1b[?2004h", "\r\x1b[Kgosip> ro 7", "\x1b[?2004l"} {
		if !strings.Contains(got, want) {
			t.Errorf("terminal output %q does not contain %q", got, want)
		}
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

func TestParseWriteLine(t *testing.T) {
	tests := []struct {
		line      string
		wantWrite bool
		wantIDN   uint32
		wantRaw   bool
		wantInput string
		wantErr   string
	}{
		{line: "w S-0-0032 1.000", wantWrite: true, wantIDN: 32, wantInput: "1.000"},
		{line: "WR 7 e8 03", wantWrite: true, wantIDN: 7, wantRaw: true, wantInput: "e8 03"},
		{line: "w 7 hello  world", wantWrite: true, wantIDN: 7, wantInput: "hello  world"},
		{line: "w", wantWrite: true, wantErr: "missing IDN for w"},
		{line: "wr", wantWrite: true, wantErr: "missing IDN for wr"},
		{line: "w 7", wantWrite: true, wantErr: "missing data for w"},
		{line: "w bogus 1", wantWrite: true, wantErr: "invalid IDN"},
		{line: "ro 7"},
		{line: "S-0-0032"},
		{line: "write 7 1"},
	}
	for _, tt := range tests {
		t.Run(tt.line, func(t *testing.T) {
			got, isWrite, err := parseWriteLine(tt.line)
			if isWrite != tt.wantWrite {
				t.Fatalf("isWrite = %t, want %t", isWrite, tt.wantWrite)
			}
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("error = %v, want substring %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseWriteLine(%q): %v", tt.line, err)
			}
			if got.idn != tt.wantIDN || got.raw != tt.wantRaw || got.input != tt.wantInput {
				t.Errorf("got %+v, want idn=%d raw=%t input=%q", got, tt.wantIDN, tt.wantRaw, tt.wantInput)
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
	printPromptHelp(&out, true)
	for _, text := range []string{"[command] IDN", "re, r", "readonlydata", "readdescription", "readdatastate", "w IDN", "wr IDN", "a host[:port]", "b [interface]", "status", "h, ?", "q", "Esc, or Ctrl-D"} {
		if !strings.Contains(out.String(), text) {
			t.Errorf("help output missing %q: %s", text, out.String())
		}
	}
	var idnOut bytes.Buffer
	printPromptHelp(&idnOut, false)
	for _, command := range []string{"address", "browse"} {
		if strings.Contains(idnOut.String(), command) {
			t.Errorf("IDN help unexpectedly contains %q command: %s", command, idnOut.String())
		}
	}
}

func TestParseAddressLine(t *testing.T) {
	tests := []struct {
		line        string
		wantAddress string
		wantIsSet   bool
		wantErr     string
	}{
		{line: "a 192.0.2.1", wantAddress: "192.0.2.1", wantIsSet: true},
		{line: "A 192.0.2.1:1234", wantAddress: "192.0.2.1:1234", wantIsSet: true},
		{line: "address host", wantAddress: "host", wantIsSet: true},
		{line: "a", wantIsSet: true, wantErr: "expected a <address>"},
		{line: "a host extra", wantIsSet: true, wantErr: "expected a <address>"},
		{line: "ro 32"},
		{line: ""},
	}
	for _, tt := range tests {
		t.Run(tt.line, func(t *testing.T) {
			address, isAddress, err := parseAddressLine(tt.line)
			if address != tt.wantAddress || isAddress != tt.wantIsSet {
				t.Fatalf("parseAddressLine(%q) = (%q, %t), want (%q, %t)", tt.line, address, isAddress, tt.wantAddress, tt.wantIsSet)
			}
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("parseAddressLine(%q) error = %v", tt.line, err)
				}
			} else if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("parseAddressLine(%q) error = %v, want substring %q", tt.line, err, tt.wantErr)
			}
		})
	}
}

func TestParseBrowseLine(t *testing.T) {
	tests := []struct {
		line       string
		wantName   string
		wantBrowse bool
		wantErr    string
	}{
		{line: "b", wantBrowse: true},
		{line: "B eth0", wantName: "eth0", wantBrowse: true},
		{line: "browse eth0", wantName: "eth0", wantBrowse: true},
		{line: "b a c", wantBrowse: true, wantErr: "expected b [interface]"},
		{line: "ro 32"},
		{line: "bogus"},
		{line: "4106"},
	}
	for _, tt := range tests {
		t.Run(tt.line, func(t *testing.T) {
			name, isBrowse, err := parseBrowseLine(tt.line)
			if isBrowse != tt.wantBrowse || name != tt.wantName {
				t.Fatalf("parseBrowseLine(%q) = (%q, %t), want (%q, %t)", tt.line, name, isBrowse, tt.wantName, tt.wantBrowse)
			}
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("parseBrowseLine(%q) error = %v", tt.line, err)
				}
			} else if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("parseBrowseLine(%q) error = %v, want substring %q", tt.line, err, tt.wantErr)
			}
		})
	}
}

func TestChooseInterface(t *testing.T) {
	names := []string{"eth0", "lo"}
	tests := []struct {
		name    string
		input   string
		want    string
		wantOK  bool
		wantErr string
	}{
		{name: "numeric selection", input: "2\n", want: "lo", wantOK: true},
		{name: "name selection", input: "eth0\n", want: "eth0", wantOK: true},
		{name: "blank selection cancels", input: "\n"},
		{name: "out of range", input: "9\n", wantErr: "invalid interface selection"},
		{name: "unknown name", input: "wlan0\n", wantErr: "invalid interface selection"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out bytes.Buffer
			got, ok, err := chooseInterface(context.Background(), newTestReader(tt.input), &out, names)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("chooseInterface error = %v, want substring %q", err, tt.wantErr)
				}
				return
			}
			if err != nil || got != tt.want || ok != tt.wantOK {
				t.Fatalf("chooseInterface = (%q, %t, %v), want (%q, %t, nil)", got, ok, err, tt.want, tt.wantOK)
			}
			if !strings.Contains(out.String(), "1) eth0") || !strings.Contains(out.String(), "2) lo") {
				t.Errorf("options not printed: %q", out.String())
			}
		})
	}

	lr := newTestReader("1\n")
	lr.history = []string{"previous"}
	if _, ok, err := chooseInterface(context.Background(), lr, io.Discard, names); err != nil || !ok {
		t.Fatalf("history selection = (ok=%t, err=%v)", ok, err)
	}
	if len(lr.history) != 1 || lr.history[0] != "previous" {
		t.Errorf("history after selection = %v, want [previous]", lr.history)
	}
	if _, _, err := chooseInterface(context.Background(), newTestReader(""), io.Discard, nil); err == nil || !strings.Contains(err.Error(), "no network interface") {
		t.Errorf("empty interface list error = %v, want no-network-interface error", err)
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
