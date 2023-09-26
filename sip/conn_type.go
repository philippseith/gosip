package sip

import (
	"context"
	"io"
	"net"
	"sync"
)

type Conn struct {
	net.Conn
	timeoutReader *timeoutReader
	mxRecv        sync.Mutex

	transactionID uint32

	reqCh        chan request
	concurrentCh chan struct{}

	respChans map[uint32]chan func(PDU) error
	mxRC      sync.RWMutex

	userBusyTimeout  uint32
	userLeaseTimeout uint32

	connectResponse ConnectResponse
	mxCR            sync.RWMutex

	cancel context.CancelCauseFunc

	mxState sync.RWMutex
}

type request struct {
	write func(conn io.Writer) (transactionId uint32, err error)
	ch    chan func(PDU) error
}
