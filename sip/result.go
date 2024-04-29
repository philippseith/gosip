package sip

type Result[T any] struct {
	Ok  T
	Err error
}

func NewResult[T any](ok T, err error) Result[T] {
	return Result[T]{Ok: ok, Err: err}
}

func Ok[T any](t T) Result[T] {
	return Result[T]{Ok: t}
}

func Err[T any](err error) Result[T] {
	return Result[T]{Err: err}
}
