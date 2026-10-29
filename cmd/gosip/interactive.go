package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"slices"
	"strconv"
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

const maxHistory = 500

var (
	errNoInput         = errors.New("no input")
	errQuitInteractive = errors.New("quit interactive input")
)

type key int

const (
	keyEscape key = iota
	keyIgnored
	keyUp
	keyDown
	keyLeft
	keyRight
	keyHome
	keyEnd
	keyDelete
	keyPasteStart
	keyPasteEnd
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
	history []string
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

type lineEditState struct {
	line    []byte
	pos     int
	histIdx int
	draft   string
}

// ReadLine prints prompt (terminal only) and returns the entered line.
// Keys: Enter accepts; Backspace deletes before the cursor; Delete deletes at it; Left/Right/Home/End and
// Ctrl-B/Ctrl-F/Ctrl-A/Ctrl-E move the cursor; Ctrl-U clears; Up/Down navigate in-memory history;
// bracketed paste inserts text without submitting; Ctrl-D on an empty line, Ctrl-C or a lone Escape exits.
// Only printable ASCII is retained. At EOF, a non-empty partial line is returned as lineOK first.
func (r *lineReader) ReadLine(ctx context.Context, prompt string) (string, lineStatus) {
	if r.out != nil {
		fmt.Fprint(r.out, prompt)
		if r.makeRaw != nil {
			if restore, err := r.makeRaw(); err == nil {
				fmt.Fprint(r.out, "\x1b[?2004h")
				defer func() {
					fmt.Fprint(r.out, "\x1b[?2004l")
					restore()
				}()
			}
		}
	}
	state := lineEditState{histIdx: len(r.history)}
	for {
		b, err := r.bytes.next(ctx, 0)
		if err != nil {
			if len(state.line) > 0 && ctx.Err() == nil {
				return string(state.line), lineOK
			}
			r.echo("\r\n")
			return "", lineEOF
		}
		if line, status, done := r.processByte(ctx, prompt, b, &state); done {
			return line, status
		}
	}
}

func (r *lineReader) processByte(ctx context.Context, prompt string, b byte, state *lineEditState) (string, lineStatus, bool) {
	if line, status, done := r.processTerminator(b, state); done {
		return line, status, true
	}
	switch b {
	case 0x7f, 0x08:
		r.backspace(prompt, state)
	case 0x15:
		state.line = state.line[:0]
		state.pos = 0
		r.redraw(prompt, state.line, state.pos)
	case 0x01, 0x02, 0x05, 0x06:
		r.moveControl(prompt, b, state)
	case 0x1b:
		if r.handleEscape(ctx, prompt, state) {
			r.echo("\r\n")
			return "", lineEscape, true
		}
	default:
		r.insertCharacter(prompt, b, state)
	}
	return "", lineOK, false
}

func (r *lineReader) processTerminator(b byte, state *lineEditState) (string, lineStatus, bool) {
	switch b {
	case '\r', '\n':
		r.recordHistory(state.line)
		r.echo("\r\n")
		return string(state.line), lineOK, true
	case 0x03:
		r.echo("\r\n")
		return "", lineEOF, true
	case 0x04:
		if len(state.line) == 0 {
			r.echo("\r\n")
			return "", lineEOF, true
		}
	}
	return "", lineOK, false
}

func (r *lineReader) insertCharacter(prompt string, b byte, state *lineEditState) {
	if b != '\t' && (b < 0x20 || b >= 0x7f) {
		return
	}
	r.insertByte(prompt, b, state)
}

func (r *lineReader) backspace(prompt string, state *lineEditState) {
	if state.pos == 0 {
		return
	}
	copy(state.line[state.pos-1:], state.line[state.pos:])
	state.line = state.line[:len(state.line)-1]
	state.pos--
	if state.pos == len(state.line) {
		r.echo("\b \b")
	} else {
		r.redraw(prompt, state.line, state.pos)
	}
}

func (r *lineReader) moveControl(prompt string, b byte, state *lineEditState) {
	switch b {
	case 0x01:
		state.pos = 0
	case 0x02:
		if state.pos > 0 {
			state.pos--
		}
	case 0x05:
		state.pos = len(state.line)
	case 0x06:
		if state.pos < len(state.line) {
			state.pos++
		}
	}
	r.redraw(prompt, state.line, state.pos)
}

func (r *lineReader) insertByte(prompt string, b byte, state *lineEditState) {
	if b == '\t' {
		b = ' '
	}
	if state.pos == len(state.line) {
		state.line = append(state.line, b)
		state.pos++
		r.echo(string(b))
		return
	}
	state.line = append(state.line, 0)
	copy(state.line[state.pos+1:], state.line[state.pos:])
	state.line[state.pos] = b
	state.pos++
	r.redraw(prompt, state.line, state.pos)
}

func (r *lineReader) handleEscape(ctx context.Context, prompt string, state *lineEditState) bool {
	k := r.readKey(ctx)
	switch k {
	case keyEscape:
		return true
	case keyUp, keyDown:
		r.navigateHistory(prompt, k, state)
	case keyLeft, keyRight, keyHome, keyEnd, keyDelete:
		r.applyCursorKey(prompt, k, state)
	case keyPasteStart:
		r.insertPaste(ctx, prompt, state)
	}
	return false
}

func (r *lineReader) navigateHistory(prompt string, k key, state *lineEditState) {
	if k == keyUp {
		if state.histIdx == len(r.history) {
			state.draft = string(state.line)
		}
		if state.histIdx == 0 {
			return
		}
		state.histIdx--
		state.line = []byte(r.history[state.histIdx])
	} else {
		if state.histIdx == len(r.history) {
			return
		}
		state.histIdx++
		if state.histIdx == len(r.history) {
			state.line = []byte(state.draft)
		} else {
			state.line = []byte(r.history[state.histIdx])
		}
	}
	state.pos = len(state.line)
	r.redraw(prompt, state.line, state.pos)
}

func (r *lineReader) applyCursorKey(prompt string, k key, state *lineEditState) {
	switch k {
	case keyLeft:
		if state.pos > 0 {
			state.pos--
		}
	case keyRight:
		if state.pos < len(state.line) {
			state.pos++
		}
	case keyHome:
		state.pos = 0
	case keyEnd:
		state.pos = len(state.line)
	case keyDelete:
		if state.pos < len(state.line) {
			copy(state.line[state.pos:], state.line[state.pos+1:])
			state.line = state.line[:len(state.line)-1]
		} else {
			return
		}
	}
	r.redraw(prompt, state.line, state.pos)
}

func (r *lineReader) insertPaste(ctx context.Context, prompt string, state *lineEditState) {
	pasted := r.readPaste(ctx)
	if len(pasted) == 0 {
		return
	}
	oldLen := len(state.line)
	state.line = append(state.line, pasted...)
	copy(state.line[state.pos+len(pasted):], state.line[state.pos:oldLen])
	copy(state.line[state.pos:state.pos+len(pasted)], pasted)
	state.pos += len(pasted)
	r.redraw(prompt, state.line, state.pos)
}

func (r *lineReader) redraw(prompt string, line []byte, pos int) {
	if r.out == nil {
		return
	}
	fmt.Fprint(r.out, "\r\x1b[K", prompt, string(line))
	if remaining := len(line) - pos; remaining > 0 {
		fmt.Fprint(r.out, "\x1b[", strconv.Itoa(remaining), "D")
	}
}

func (r *lineReader) recordHistory(line []byte) {
	entry := strings.TrimSpace(string(line))
	if entry == "" || len(r.history) > 0 && r.history[len(r.history)-1] == entry {
		return
	}
	r.history = append(r.history, entry)
	if len(r.history) > maxHistory {
		r.history = r.history[1:]
	}
}

func (r *lineReader) readPaste(ctx context.Context) []byte {
	var pasted []byte
	newline := false
	for {
		b, err := r.bytes.next(ctx, 0)
		if err != nil {
			return pasted
		}
		if b == 0x1b {
			if r.readKey(ctx) == keyPasteEnd {
				return pasted
			}
			newline = false
			continue
		}
		pasted, newline = appendPasteByte(pasted, b, newline)
	}
}

func appendPasteByte(pasted []byte, b byte, newline bool) ([]byte, bool) {
	switch {
	case b == '\r' || b == '\n':
		if !newline {
			pasted = append(pasted, ' ')
		}
		return pasted, true
	case b == '\t':
		return append(pasted, ' '), false
	case b >= 0x20 && b < 0x7f:
		return append(pasted, b), false
	default:
		return pasted, false
	}
}

// readKey runs after an ESC byte and parses one terminal key sequence.
func (r *lineReader) readKey(ctx context.Context) key {
	b, err := r.bytes.next(ctx, r.escWait)
	if err != nil {
		if ctx.Err() == nil {
			return keyEscape
		}
		return keyIgnored
	}
	switch b {
	case '[':
		return r.readCSIKey(ctx)
	case 'O':
		c, err := r.bytes.next(ctx, r.escWait)
		if err != nil {
			return keyIgnored
		}
		return keyFromFinal(c)
	default:
		return keyIgnored
	}
}

func (r *lineReader) readCSIKey(ctx context.Context) key {
	var params []byte
	for {
		c, err := r.bytes.next(ctx, r.escWait)
		if err != nil {
			return keyIgnored
		}
		if c >= 0x20 && c <= 0x3f {
			params = append(params, c)
			continue
		}
		if c < 0x40 || c > 0x7e {
			return keyIgnored
		}
		if c == '~' {
			return keyFromTilde(params)
		}
		return keyFromFinal(c)
	}
}

func keyFromTilde(params []byte) key {
	switch string(params) {
	case "1", "7":
		return keyHome
	case "4", "8":
		return keyEnd
	case "3":
		return keyDelete
	case "200":
		return keyPasteStart
	case "201":
		return keyPasteEnd
	default:
		return keyIgnored
	}
}

func keyFromFinal(c byte) key {
	switch c {
	case 'A':
		return keyUp
	case 'B':
		return keyDown
	case 'C':
		return keyRight
	case 'D':
		return keyLeft
	case 'H':
		return keyHome
	case 'F':
		return keyEnd
	default:
		return keyIgnored
	}
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
			printPromptHelp(os.Stdout, false)
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

func parseAddressLine(line string) (address string, isAddress bool, err error) {
	fields := strings.Fields(line)
	if len(fields) == 0 || (!strings.EqualFold(fields[0], "a") && !strings.EqualFold(fields[0], "address")) {
		return "", false, nil
	}
	if len(fields) != 2 {
		return "", true, errors.New("expected a <address>")
	}
	return fields[1], true, nil
}

func parseBrowseLine(line string) (interfaceName string, isBrowse bool, err error) {
	fields := strings.Fields(line)
	if len(fields) == 0 || (!strings.EqualFold(fields[0], "b") && !strings.EqualFold(fields[0], "browse")) {
		return "", false, nil
	}
	if len(fields) > 2 {
		return "", true, errors.New("expected b [interface]")
	}
	if len(fields) == 2 {
		return fields[1], true, nil
	}
	return "", true, nil
}

func chooseInterface(ctx context.Context, lr *lineReader, out io.Writer, names []string) (string, bool, error) {
	if len(names) == 0 {
		return "", false, errors.New("no network interface with an IPv4 address is up")
	}
	for i, name := range names {
		fmt.Fprintf(out, "%d) %s\n", i+1, name)
	}
	saved := lr.history
	lr.history = nil
	defer func() { lr.history = saved }()
	line, st := lr.ReadLine(ctx, "interface> ")
	if st != lineOK {
		return "", false, nil
	}
	selection := strings.TrimSpace(line)
	if selection == "" {
		return "", false, nil
	}
	if number, err := strconv.Atoi(selection); err == nil && number >= 1 && number <= len(names) {
		return names[number-1], true, nil
	}
	for _, name := range names {
		if selection == name {
			return name, true, nil
		}
	}
	return "", false, fmt.Errorf("invalid interface selection %q", selection)
}

func promptBrowse(ctx context.Context, lr *lineReader, out io.Writer, interfaceName string) error {
	if interfaceName == "" {
		names, err := availableInterfaces()
		if err != nil {
			return err
		}
		var ok bool
		interfaceName, ok, err = chooseInterface(ctx, lr, out, names)
		if err != nil {
			return err
		}
		if !ok {
			return nil
		}
	}
	browseCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	return browseInterface(browseCtx, interfaceName)
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
func printPromptHelp(out io.Writer, gosipPrompt bool) {
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
	if gosipPrompt {
		fmt.Fprintf(out, "  %-6s %-16s %s\n", "a", "address", "set the target S/IP address: a host[:port]")
		fmt.Fprintf(out, "  %-6s %-16s %s\n", "b", "browse", "list devices answering a UDP Browse: b [interface]; without interface, choose one")
	}
	fmt.Fprintf(out, "  %-6s %-16s %s\n", "s", "status", "show the negotiated BusyTimeout and LeaseTimeout")
	fmt.Fprintf(out, "  %-6s %-16s %s\n", "h, ?", "help", "show this help")
	fmt.Fprintf(out, "  %-6s %-16s %s\n", "q", "quit", "quit the prompt")
	fmt.Fprintln(out, `Type h, ?, or help to show this help. q, quit, Esc, or Ctrl-D quits. Up/Down recall history; cursor keys edit; bracketed paste inserts text.`)
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

// print writes the timeouts, or explains that no request has connected yet.
func (r *timeoutReporter) print() {
	busy, lease := r.client.BusyTimeout(), r.client.LeaseTimeout()
	if busy <= 0 || lease <= 0 {
		fmt.Fprintln(r.out, "not connected yet")
		return
	}
	r.printTimeouts(busy, lease)
}

// printIfConnected writes the negotiated timeouts only when a request established a connection.
func (r *timeoutReporter) printIfConnected() {
	busy, lease := r.client.BusyTimeout(), r.client.LeaseTimeout()
	if busy > 0 && lease > 0 {
		r.printTimeouts(busy, lease)
	}
}

func (r *timeoutReporter) printTimeouts(busy, lease time.Duration) {
	fmt.Fprintf(r.out, "BusyTimeout: %v\n", busy)
	fmt.Fprintf(r.out, "LeaseTimeout: %v\n", lease)
}

// handleAddressLine constructs the replacement client before closing the current one.
func handleAddressLine(line string, client *sip.Client, address *string, reporter *timeoutReporter, dial func(string) (sip.Client, error)) (bool, error) {
	requestedAddress, isAddress, err := parseAddressLine(line)
	if !isAddress {
		return false, nil
	}
	if err != nil {
		return true, err
	}
	newAddress, err := addressWithDefaultPort(requestedAddress)
	if err != nil {
		return true, err
	}
	if newAddress == *address {
		fmt.Fprintf(os.Stdout, "Address already set to %s\n", *address)
		return true, nil
	}
	newClient, err := dial(newAddress)
	if err != nil {
		return true, err
	}
	oldClient := *client
	*client = newClient
	*address = newAddress
	reporter.client = newClient
	_ = oldClient.Close()
	fmt.Fprintf(os.Stdout, "Address set to %s\n", *address)
	return true, nil
}

// runPrompt accepts read/write lines and prompt commands until q/quit, Esc, Ctrl-D or Ctrl-C.
// Address changes replace the lazy client; subsequent reads and writes use the new target.
func runPrompt(client sip.Client, address string, dial func(string) (sip.Client, error), options []sip.RequestOption) error {
	defer func() { _ = client.Close() }()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	reqOptions := append(slices.Clone(options), sip.WithContext(ctx))
	rep := newTimeoutReporter(client, os.Stdout)
	printPromptHelp(os.Stdout, true)
	lr := newStdinLineReader()
	readLines(ctx, lr, "gosip> ", os.Stderr, func(line string) error {
		if isQuitLine(line) {
			return errQuitInteractive
		}
		if isHelpLine(line) {
			printPromptHelp(os.Stdout, true)
			return nil
		}
		if isStatusLine(line) {
			rep.print()
			return nil
		}
		if handled, err := handleAddressLine(line, &client, &address, rep, dial); handled {
			return err
		}
		if name, isBrowse, err := parseBrowseLine(line); isBrowse {
			if err != nil {
				return err
			}
			return promptBrowse(ctx, lr, os.Stdout, name)
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
