package sip

import "github.com/joomcode/errorx"

// signal sends on the channel returned by getChan. It returns ErrorClosed when
// the channel is gone or done is closed. The channel itself is never closed, so
// the send cannot panic.
func signal(done <-chan struct{}, getChan func() chan struct{}) error {
	ch := getChan()

	if ch == nil {
		return errorx.EnsureStackTrace(ErrorClosed)
	}
	select {
	case ch <- struct{}{}:
		return nil
	case <-done:
		return errorx.EnsureStackTrace(ErrorClosed)
	}
}

// wait receives from the channel returned by getChan. It returns ErrorClosed
// when the channel is gone or done is closed.
func wait(done <-chan struct{}, getChan func() chan struct{}) error {
	ch := getChan()

	if ch == nil {
		return errorx.EnsureStackTrace(ErrorClosed)
	}
	select {
	case <-ch:
		return nil
	case <-done:
		return errorx.EnsureStackTrace(ErrorClosed)
	}
}

func mapChan[T any, V any](chT <-chan T, mapFunc func(T) V, capacity int) <-chan V {
	chV := make(chan V, capacity)
	go func() {
		defer close(chV)

		for t := range chT {
			chV <- mapFunc(t)
		}
	}()
	return chV
}
