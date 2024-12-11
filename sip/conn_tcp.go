package sip

import (
	"bufio"
	"fmt"
	"io"
	"net"

	"github.com/joomcode/errorx"
	"github.com/mikioh/tcp"
	"github.com/mikioh/tcpopt"
)

// newCorkWriter creates a bufio.Writer to a net.Conn which initially corks the
// socket, uncorks it when the writer is flushed and corks it again when the
// buffer has been written to the net.Conn
func newCorkWriter(conn io.ReadWriteCloser, mtu int, onFlush func()) (*bufio.Writer, error) {
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
	return bufio.NewWriterSize(&flushGuard{tcpConn: tcpConn, onFlush: onFlush}, mtu), nil
}

type flushGuard struct {
	tcpConn *tcp.Conn
	onFlush func()
}

func (f *flushGuard) Write(p []byte) (n int, err error) {
	err = f.tcpConn.SetOption(tcpopt.Cork(false))
	if err != nil {
		return 0, err
	}

	defer func() {
		if err := f.tcpConn.SetOption(tcpopt.Cork(true)); err != nil {
			logger.Printf("can not cork: %v", err)
		}
		f.onFlush()
	}()

	return f.tcpConn.Write(p)
}
