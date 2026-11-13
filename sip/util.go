package sip

import "github.com/joomcode/errorx"

// signal sends a token on the channel returned by getChan. It gives up with
// ErrorClosed when the connection is torn down. The channel itself is never
// closed, because a send racing with a close of the channel would panic.
func signal(getChan func() (ch, closed chan struct{})) error {
	ch, closed := getChan()

	if ch == nil {
		return errorx.EnsureStackTrace(ErrorClosed)
	}
	select {
	case ch <- struct{}{}:
		return nil
	case <-closed:
		return errorx.EnsureStackTrace(ErrorClosed)
	}
}

// wait receives a token from the channel returned by getChan. It gives up with
// ErrorClosed when the connection is torn down.
func wait(getChan func() (ch, closed chan struct{})) error {
	ch, closed := getChan()

	if ch == nil {
		return errorx.EnsureStackTrace(ErrorClosed)
	}
	select {
	case <-ch:
		return nil
	case <-closed:
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
