package sip

import (
	"bufio"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"sync"
	"time"

	"github.com/joomcode/errorx"
	"github.com/mikioh/tcp"
	"github.com/mikioh/tcpopt"
)

// newCorkWriter creates a bufio.Writer to a net.Conn which initially corks the
// socket, uncorks it when the writer is flushed and corks it again when the
// buffer has been written to the net.Conn
func newCorkWriter(ctx context.Context, conn io.ReadWriteCloser, corkInterval time.Duration, transactionStarted func() chan struct{}) (io.Writer, error) {
	netConn, ok := conn.(net.Conn)
	if !ok {
		return nil, errorx.EnsureStackTrace(fmt.Errorf("%w: conn is not a net.Conn", Error))
	}
	tcpConn, err := tcp.NewConn(netConn)
	if err != nil {
		return nil, errorx.EnsureStackTrace(err)
	}

	b := []byte{0, 0, 0, 0}
	mss := 1460
	_, err = tcpConn.Option(tcpopt.MSS(0).Level(), tcpopt.MSS(0).Name(), b)
	if err != nil {
		logger.Printf("can not get MSS: %v", err)
	} else {
		mss = int(binary.LittleEndian.Uint32(b))
	}

	writer := &corkWriter{}
	flushGuard := &flushGuard{
		cw:                 writer,
		tcpConn:            tcpConn,
		transactionStarted: transactionStarted,
	}

	writer.bufWriter = bufio.NewWriterSize(flushGuard, mss)

	go func() {
		ticker := time.NewTicker(corkInterval)
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := writer.bufWriter.Flush(); err != nil {
					logger.Printf("can not flush writer: %v", err)
				}
			}
		}
	}()

	return writer, nil
}

type corkWriter struct {
	unflushedMessageCount int
	bufWriter             *bufio.Writer
	mx                    sync.Mutex
}

type flushGuard struct {
	cw                 *corkWriter
	tcpConn            *tcp.Conn
	transactionStarted func() chan struct{}
}

func (fg *flushGuard) Write(p []byte) (n int, err error) {
	fg.cw.mx.Lock()
	defer fg.cw.mx.Unlock()

	log.Printf("flushed %v", len(p))

	if err := signalN(fg.transactionStarted, fg.cw.unflushedMessageCount); err != nil {
		logger.Printf("can not signal transaction started: %v", errorx.EnsureStackTrace(err))
	}
	fg.cw.unflushedMessageCount = 0

	return fg.tcpConn.Write(p)
}

func (cw *corkWriter) Write(p []byte) (n int, err error) {
	func() {
		cw.mx.Lock()
		defer cw.mx.Unlock()

		cw.unflushedMessageCount++
	}()

	return cw.bufWriter.Write(p)
}

func newSingleTransactionWriter(conn io.ReadWriteCloser, transactionStarted func() chan struct{}) io.Writer {
	return &singleTransactionWriter{
		writer:             bufio.NewWriterSize(conn, 66000),
		transactionStarted: transactionStarted,
	}
}

type singleTransactionWriter struct {
	writer             *bufio.Writer
	transactionStarted func() chan struct{}
}

func (stw *singleTransactionWriter) Write(p []byte) (n int, err error) {
	n, err = stw.writer.Write(p)
	err = errors.Join(err, stw.writer.Flush(), signal(stw.transactionStarted))
	return n, err
}
