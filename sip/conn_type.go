package sip

import (
	"bufio"
	"context"
	"io"
	"sync"
	"time"
)

type conn struct {
	Conn io.ReadWriteCloser
	connOptions
	address       string
	timeoutReader *timeoutReader
	mxRecv        sync.Mutex

	transactionID uint32

	reqCh                chan request
	reqChWaitCount       int32 // number of goroutines waiting for a response at reqCh
	transactionStartedCh chan struct{}

	respChans map[uint32]chan func(PDU) error
	mxRC      sync.RWMutex

	connectResponse ConnectResponse
	mxCR            sync.RWMutex

	cancel       context.CancelCauseFunc
	lastReceived time.Time

	mxState sync.RWMutex
	closed  bool

	mtuWriter *bufio.Writer
}

type request struct {
	write func(conn io.Writer) (transactionId uint32, err error)
	ch    chan func(PDU) error
}
