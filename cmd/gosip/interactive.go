package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"slices"
	"strings"
	"time"
	"unicode"

	"github.com/philippseith/gosip/sip"
	"golang.org/x/term"
)

const (
	idnPrompt     = "idn> "
	escapeTimeout = 50 * time.Millisecond // how long after ESC to wait for the rest of an escape sequence
)

var (
	errNoInput         = errors.New("no input")
	errQuitInteractive = errors.New("quit interactive input")
)

type lineStatus int

const (
	lineOK     lineStatus = iota // a line was entered (possibly empty)
	lineEscape                   // Escape pressed alone
	lineEOF                      // Ctrl-D on an empty line, Ctrl-C, end of input, read error or canceled context
)

type byteResult struct {
	b   byte
	err error
}

// byteReader reads one byte at a time. A Read is only outstanding while next waits for it, except
// after a timeout: that Read stays in pending and its byte is returned by the next call, so no input is lost.
type byteReader struct {
	in      io.Reader
	pending chan byteResult
}

func (r *byteReader) next(ctx context.Context, timeout time.Duration) (byte, error) {
	if r.pending == nil {
		ch := make(chan byteResult, 1)
		r.pending = ch
		go func(in io.Reader) {
			var buf [1]byte
			for {
				n, err := in.Read(buf[:])
				if n > 0 {
					ch <- byteResult{b: buf[0]}
					return
				}
				if err != nil {
					ch <- byteResult{err: err}
					return
				}
			}
		}(r.in)
	}
	var timer <-chan time.Time
	if timeout > 0 {
		t := time.NewTimer(timeout)
		defer t.Stop()
		timer = t.C
	}
	select {
	case res := <-r.pending:
		r.pending = nil
		return res.b, res.err
	case <-ctx.Done():
		return 0, ctx.Err()
	case <-timer:
		return 0, errNoInput
	}
}

// lineReader is a minimal line editor. out == nil means piped input: no prompt, no echo.
// makeRaw (terminal only) switches the terminal to raw mode and returns the restore func; it is
// called per ReadLine so that requests execute in cooked mode, where Ctrl-C raises SIGINT.
type lineReader struct {
	bytes   *byteReader
	out     io.Writer
	makeRaw func() (restore func(), err error)
	escWait time.Duration
}

func newLineReader(in io.Reader, out io.Writer, makeRaw func() (func(), error)) *lineReader {
	return &lineReader{bytes: &byteReader{in: in}, out: out, makeRaw: makeRaw, escWait: escapeTimeout}
}

func newStdinLineReader() *lineReader {
	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		return newLineReader(os.Stdin, nil, nil)
	}
	return newLineReader(os.Stdin, os.Stdout, func() (func(), error) {
		state, err := term.MakeRaw(fd)
		if err != nil {
			return nil, err
		}
		return func() { _ = term.Restore(fd, state) }, nil
	})
}

func (r *lineReader) echo(s string) {
	if r.out != nil {
		fmt.Fprint(r.out, s)
	}
}

// ReadLine prints prompt (terminal only) and returns the entered line.
// Keys: Enter ('\r' or '\n') accepts; Backspace (0x7f/0x08) deletes the last char; Ctrl-U clears the line;
// Ctrl-D on an empty line and Ctrl-C (raw mode delivers it as byte 0x03) return lineEOF;
// a lone Escape returns ("", lineEscape); escape sequences (arrow keys etc.) are ignored; only
// printable ASCII (0x20-0x7e) is stored. Output uses "\r\n" because raw mode disables newline translation.
// At end of input a non-empty partial line is returned as lineOK first; the next call returns lineEOF.
func (r *lineReader) ReadLine(ctx context.Context, prompt string) (string, lineStatus) { // nolint:cyclop
	if r.out != nil {
		fmt.Fprint(r.out, prompt)
		if r.makeRaw != nil {
			if restore, err := r.makeRaw(); err == nil { // on error stay in cooked mode: Enter still works, Esc needs Enter
				defer restore()
			}
		}
	}
	var line []byte
	for {
		b, err := r.bytes.next(ctx, 0)
		if err != nil {
			if len(line) > 0 && ctx.Err() == nil {
				return string(line), lineOK
			}
			r.echo("\r\n")
			return "", lineEOF
		}
		switch {
		case b == '\r' || b == '\n':
			r.echo("\r\n")
			return string(line), lineOK
		case b == 0x03:
			r.echo("\r\n")
			return "", lineEOF
		case b == 0x04:
			if len(line) == 0 {
				r.echo("\r\n")
				return "", lineEOF
			}
		case b == 0x7f || b == 0x08:
			if len(line) > 0 {
				line = line[:len(line)-1]
				r.echo("\b \b")
			}
		case b == 0x15:
			r.echo(strings.Repeat("\b \b", len(line)))
			line = line[:0]
		case b == 0x1b:
			if r.consumeEscape(ctx) {
				r.echo("\r\n")
				return "", lineEscape
			}
		case b >= 0x20 && b < 0x7f:
			line = append(line, b)
			r.echo(string(b))
		}
	}
}

// consumeEscape runs after an ESC byte. It returns true if ESC stood alone (nothing followed within
// escWait, or input ended). Otherwise it swallows the escape sequence and returns false.
func (r *lineReader) consumeEscape(ctx context.Context) bool {
	b, err := r.bytes.next(ctx, r.escWait)
	if err != nil {
		return ctx.Err() == nil
	}
	switch b {
	case '[': // CSI: parameter bytes, then one final byte 0x40-0x7e
		for {
			c, err := r.bytes.next(ctx, r.escWait)
			if err != nil || (c >= 0x40 && c <= 0x7e) {
				return false
			}
		}
	case 'O': // SS3 (F1-F4): one more byte
		_, _ = r.bytes.next(ctx, r.escWait)
	}
	return false // Alt+key: the byte after ESC is dropped
}

// readLines prompts and calls fn with each non-blank trimmed line until the reader returns something other than
// lineOK; that status (lineEscape or lineEOF) is returned. An error from fn is written to errOut as
// "error: <err>" and the loop continues, except an explicit quit or an fn error after ctx is canceled, which
// returns lineEOF silently. Read errors other than EOF are treated as EOF.
func readLines(ctx context.Context, lr *lineReader, prompt string, errOut io.Writer, fn func(line string) error) lineStatus { //nolint:unparam // EOF and Escape are separately tested; prompt callers treat both as success.
	for {
		line, st := lr.ReadLine(ctx, prompt)
		if st != lineOK {
			return st
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if err := fn(line); err != nil {
			if errors.Is(err, errQuitInteractive) {
				return lineEOF
			}
			if ctx.Err() != nil {
				return lineEOF
			}
			fmt.Fprintln(errOut, "error:", err)
		}
	}
}

// forEachIDN prompts for IDNs and calls fn for each until Ctrl-C/Ctrl-D/Esc or q/quit (all exit successfully).
// The line "s"/"status" prints the negotiated timeouts; "h", "?", or "help" prints interactive command help.
// Ctrl-C while a request runs cancels it.
func forEachIDN(client sip.Client, options []sip.RequestOption, fn func(idn uint32, options []sip.RequestOption) error) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	reqOptions := append(slices.Clone(options), sip.WithContext(ctx)) // ParseRequestOptions applies WithTimeout on top of this ctx regardless of option order
	rep := newTimeoutReporter(client, os.Stdout)
	readLines(ctx, newStdinLineReader(), idnPrompt, os.Stderr, func(line string) error {
		if isQuitLine(line) {
			return errQuitInteractive
		}
		if isHelpLine(line) {
			printPromptHelp(os.Stdout)
			return nil
		}
		if isStatusLine(line) {
			rep.print()
			return nil
		}
		idn, err := parseIdn(line)
		if err != nil {
			return err
		}
		err = fn(idn, reqOptions)
		return err
	})
	return nil
}

// lookupReadCommand resolves a command word: "r" (alias of re), a short name or a full name, case-insensitive.
func lookupReadCommand(word string) (readCommand, bool) {
	if strings.EqualFold(word, "r") {
		return readCommands[0], true // readeverything
	}
	for _, rc := range readCommands {
		if strings.EqualFold(word, rc.short) || strings.EqualFold(word, rc.name) {
			return rc, true
		}
	}
	return readCommand{}, false
}

// parseReadLine parses "[command] IDN". Without command, readeverything is used.
// line is trimmed and non-empty.
func parseReadLine(line string) (readCommand, uint32, error) {
	fields := strings.Fields(line)
	var rc readCommand
	var idnStr string
	switch len(fields) {
	case 1:
		if c, ok := lookupReadCommand(fields[0]); ok {
			return readCommand{}, 0, fmt.Errorf("missing IDN for %s", c.name)
		}
		rc, idnStr = readCommands[0], fields[0]
	case 2:
		c, ok := lookupReadCommand(fields[0])
		if !ok {
			return readCommand{}, 0, fmt.Errorf("unknown command %q", fields[0])
		}
		rc, idnStr = c, fields[1]
	default:
		return readCommand{}, 0, errors.New("expected [command] IDN")
	}
	idn, err := parseIdn(idnStr)
	if err != nil {
		return readCommand{}, 0, err
	}
	return rc, idn, nil
}

func cutWord(s string) (word, rest string) {
	s = strings.TrimSpace(s)
	i := strings.IndexFunc(s, unicode.IsSpace)
	if i < 0 {
		return s, ""
	}
	return s[:i], strings.TrimSpace(s[i:])
}

type writeLine struct {
	idn   uint32
	raw   bool
	input string
}

func parseWriteLine(line string) (wl writeLine, isWrite bool, err error) {
	word, rest := cutWord(line)
	switch {
	case strings.EqualFold(word, "w"):
	case strings.EqualFold(word, "wr"):
		wl.raw = true
	default:
		return writeLine{}, false, nil
	}
	idnStr, input := cutWord(rest)
	if idnStr == "" {
		return wl, true, fmt.Errorf("missing IDN for %s", strings.ToLower(word))
	}
	idn, err := parseIdn(idnStr)
	if err != nil {
		return wl, true, err
	}
	if input == "" {
		return wl, true, fmt.Errorf("missing data for %s", strings.ToLower(word))
	}
	wl.idn = idn
	wl.input = input
	return wl, true, nil
}

// isStatusLine reports whether the line asks for the status ("s" or "status", case-insensitive).
func isStatusLine(line string) bool {
	return strings.EqualFold(line, "s") || strings.EqualFold(line, "status")
}

// isHelpLine reports whether the line asks for interactive command help.
func isHelpLine(line string) bool {
	return strings.EqualFold(line, "h") || line == "?" || strings.EqualFold(line, "help")
}

// isQuitLine reports whether the line asks to leave an interactive prompt.
func isQuitLine(line string) bool {
	return strings.EqualFold(line, "q") || strings.EqualFold(line, "quit")
}

// printPromptHelp writes the command and control help used by both interactive prompts.
func printPromptHelp(out io.Writer) {
	fmt.Fprintln(out, `Enter: [command] IDN, e.g. ro S-0-0032.0.0. Without command, re is used.`)
	for _, rc := range readCommands {
		short := rc.short
		if rc.short == "re" {
			short = "re, r"
		}
		fmt.Fprintf(out, "  %-6s %-16s %s\n", short, rc.name, rc.summary)
	}
	fmt.Fprintf(out, "  %-6s %-16s %s\n", "w", "write", "write formatted data: w IDN value [value...]")
	fmt.Fprintf(out, "  %-6s %-16s %s\n", "wr", "write raw", "write raw hex bytes: wr IDN 0a1b2c3d")
	fmt.Fprintf(out, "  %-6s %-16s %s\n", "s", "status", "show the negotiated BusyTimeout and LeaseTimeout")
	fmt.Fprintf(out, "  %-6s %-16s %s\n", "h, ?", "help", "show this help")
	fmt.Fprintf(out, "  %-6s %-16s %s\n", "q", "quit", "quit the prompt")
	fmt.Fprintln(out, `Type h, ?, or help to show this help. q, quit, Esc, or Ctrl-D quits.`)
}

// timeoutSource is the part of sip.Client the reporter needs.
type timeoutSource interface {
	BusyTimeout() time.Duration
	LeaseTimeout() time.Duration
}

// timeoutReporter prints the negotiated BusyTimeout/LeaseTimeout of a client.
// Not safe for concurrent use; every user calls it from the line loop goroutine.
type timeoutReporter struct {
	client timeoutSource
	out    io.Writer
}

func newTimeoutReporter(client timeoutSource, out io.Writer) *timeoutReporter {
	return &timeoutReporter{client: client, out: out}
}

// print writes the timeouts; the client reports 0 until the first request connected it.
func (r *timeoutReporter) print() {
	busy, lease := r.client.BusyTimeout(), r.client.LeaseTimeout()
	if busy <= 0 || lease <= 0 {
		fmt.Fprintln(r.out, "not connected yet")
		return
	}
	fmt.Fprintf(r.out, "BusyTimeout: %v\n", busy)
	fmt.Fprintf(r.out, "LeaseTimeout: %v\n", lease)
}

// runPrompt prompts for "[command] IDN" lines (see parseReadLine), help and status until q/quit, Esc, Ctrl-D or Ctrl-C
// (all end the program with status 0). Always reads slave 0, extension 0.
func runPrompt(client sip.Client, options []sip.RequestOption) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	reqOptions := append(slices.Clone(options), sip.WithContext(ctx))
	rep := newTimeoutReporter(client, os.Stdout)
	printPromptHelp(os.Stdout)
	readLines(ctx, newStdinLineReader(), "gosip> ", os.Stderr, func(line string) error {
		if isQuitLine(line) {
			return errQuitInteractive
		}
		if isHelpLine(line) {
			printPromptHelp(os.Stdout)
			return nil
		}
		if isStatusLine(line) {
			rep.print()
			return nil
		}
		if wl, isWrite, err := parseWriteLine(line); isWrite {
			if err != nil {
				return err
			}
			return writeIDN(client, os.Stdout, 0, 0, wl.idn, wl.input, wl.raw, reqOptions)
		}
		rc, idn, err := parseReadLine(line)
		if err != nil {
			return err
		}
		err = rc.read(client, 0, 0, idn, reqOptions)
		return err
	})
	return nil
}
