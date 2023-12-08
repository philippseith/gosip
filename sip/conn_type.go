package sip

import (
	"context"
	"io"
	"net"
	"sync"
)

type conn struct {
	net.Conn
	connOptions
	timeoutReader *timeoutReader
	mxRecv        sync.Mutex

	transactionID uint32

	reqCh                chan request
	transactionStartedCh chan struct{}

	respChans map[uint32]chan func(PDU) error
	mxRC      sync.RWMutex

	connectResponse ConnectResponse
	mxCR            sync.RWMutex

	cancel context.CancelCauseFunc

	mxState sync.RWMutex
}

type request struct {
	write func(conn io.Writer) (transactionId uint32, err error)
	ch    chan func(PDU) error
}
