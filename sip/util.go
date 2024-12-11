package sip

import "github.com/joomcode/errorx"

func signal(getChan func() chan struct{}) error {
	return signalN(getChan, 1)
}

func signalN(getChan func() chan struct{}, n int) error {
	ch := getChan()

	if ch == nil {
		return errorx.EnsureStackTrace(ErrorClosed)
	}
	for i := 0; i < n; i++ {
		ch <- struct{}{}
	}
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
