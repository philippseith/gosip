package sip

import (
	"net"
	"time"
)

type deadlineReader struct {
	net.Conn
	timeout time.Duration
}

func (d *deadlineReader) Read(p []byte) (n int, err error) {
	if err := d.Conn.SetReadDeadline(time.Now().Add(d.timeout)); err != nil {
		return 0, err
	}
	return d.Conn.Read(p)
}
