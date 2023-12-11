package sip

func signal(getChan func() chan struct{}) error {
	ch := getChan()

	if ch == nil {
		return ErrorClosed
	}
	ch <- struct{}{}
	return nil
}

func wait(getChan func() chan struct{}) error {
	ch := getChan()

	if ch == nil {
		return ErrorClosed
	}
	_, ok := <-ch
	if !ok {
		return ErrorClosed
	}
	return nil
}
