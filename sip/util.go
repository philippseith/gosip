package sip

func signal(getChan func() chan struct{}) error {
	ch := getChan()

	if ch == nil {
		return errorx.Wrap(ErrorClosed)
	}
	ch <- struct{}{}
	return nil
}

func wait(getChan func() chan struct{}) error {
	ch := getChan()

	if ch == nil {
		return errorx.Wrap(ErrorClosed)
	}
	_, ok := <-ch
	if !ok {
		return errorx.Wrap(ErrorClosed)
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
