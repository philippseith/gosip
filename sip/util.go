package sip

import "github.com/joomcode/errorx"

func signal(getChan func() chan struct{}) error {
	ch := getChan()

	if ch == nil {
		return errorx.EnsureStackTrace(ErrorClosed)
	}
	ch <- struct{}{}
	return nil
}

func wait(getChan func() chan struct{}) error {
	ch := getChan()

	if ch == nil {
		return errorx.EnsureStackTrace(ErrorClosed)
	}
	_, ok := <-ch
	if !ok {
		return errorx.EnsureStackTrace(ErrorClosed)
	}
	return nil
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
