package sip

import (
	"braces.dev/errtrace"
	"io"
	"sync"
	"time"
)

type timeoutReader struct {
	reader    io.Reader
	timeout   time.Duration
	mxTimeout sync.RWMutex
}

type readResult struct {
	p   []byte
	n   int
	err error
}

func (t *timeoutReader) Read(p []byte) (n int, err error) {
	read := make(chan readResult, 1)
	go func() {
		r := readResult{p: make([]byte, len(p))}
		r.n, r.err = t.reader.Read(r.p)
		read <- r
	}()
	select {
	case r := <-read:
		copy(p, r.p)
		return r.n, errtrace.Wrap(r.err)
	case <-time.After(t.Timeout()):
		return 0, errtrace.Wrap(ErrorTimeout)
	}
}

func (t *timeoutReader) Timeout() time.Duration {
	t.mxTimeout.RLock()
	defer t.mxTimeout.RUnlock()

	return t.timeout
}

func (t *timeoutReader) SetTimeout(timeout time.Duration) {
	t.mxTimeout.Lock()
	defer t.mxTimeout.Unlock()

	t.timeout = timeout
}
