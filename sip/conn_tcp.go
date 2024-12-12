package sip

import (
	"bufio"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"time"

	"github.com/joomcode/errorx"
	"github.com/mikioh/tcp"
	"github.com/mikioh/tcpopt"
)

// newCorkWriter creates a bufio.Writer to a net.Conn which initially corks the
// socket, uncorks it when the writer is flushed and corks it again when the
// buffer has been written to the net.Conn
func newCorkWriter(ctx context.Context, conn io.ReadWriteCloser, corkInterval time.Duration, onFlush func()) (*bufio.Writer, error) {
	netConn, ok := conn.(net.Conn)
	if !ok {
		return nil, errorx.EnsureStackTrace(fmt.Errorf("%w: conn is not a net.Conn", Error))
	}
	tcpConn, err := tcp.NewConn(netConn)
	if err != nil {
		return nil, errorx.EnsureStackTrace(err)
	}
	// Initial corking
	if err = tcpConn.SetOption(tcpopt.Cork(true)); err != nil {
		return nil, err
	}

	b := []byte{0, 0, 0, 0}
	mss := 1460
	_, err = tcpConn.Option(tcpopt.MSS(0).Level(), tcpopt.MSS(0).Name(), b)
	if err != nil {
		logger.Printf("can not get MSS: %v", err)
	} else {
		mss = int(binary.LittleEndian.Uint32(b))
	}

	writer := bufio.NewWriterSize(&flushGuard{tcpConn: tcpConn, onFlush: onFlush}, mss)

	go func() {
		ticker := time.NewTicker(corkInterval)
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := writer.Flush(); err != nil {
					logger.Printf("can not flush writer: %v", err)
				}
			}
		}
	}()

	return writer, nil
}

type flushGuard struct {
	tcpConn *tcp.Conn
	onFlush func()
}

func (f *flushGuard) Write(p []byte) (n int, err error) {
	defer f.onFlush()

	logger.Printf("write: %v", len(p))
	return f.tcpConn.Write(p)
}
