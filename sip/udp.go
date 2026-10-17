package sip

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"time"

	"github.com/joomcode/errorx"
)

func listenUDP[T PDU](ctx context.Context, conn net.PacketConn, timeout time.Duration, ch chan<- Result[T]) bool {
	err := conn.SetReadDeadline(time.Now().Add(timeout))
	if err != nil {
		_ = sendUDPResult(ctx, ch, Err[T](errorx.EnsureStackTrace(err)))
		// If setting the deadline does not work, the go func might not end.
		return false
	}
	buf := make([]byte, 65507) // maximum IPv4 UDP payload; ReadFrom silently truncates larger datagrams
	n, _, err := conn.ReadFrom(buf)
	if err != nil {
		// Timeouts are expected and used to check ctx.Done() in regular intervals
		// See https://pkg.go.dev/net@go1.18.3#Conn.SetDeadline
		if errors.Is(err, os.ErrDeadlineExceeded) {
			return true
		}
		_ = sendUDPResult(ctx, ch, Err[T](errorx.EnsureStackTrace(err)))
		return false
	}
	reader := bytes.NewReader(buf[:n])
	hdr := Header{}
	err = hdr.Read(reader)
	msgType := MessageTypeOf[T]()
	if err != nil || hdr.MessageType != msgType {
		return true
	}
	resp := NewResponse[T](msgType)
	err = resp.Read(reader)
	if err == nil {
		return sendUDPResult(ctx, ch, Ok(resp))
	}
	if !sendUDPResult(ctx, ch, Err[T](errorx.EnsureStackTrace(fmt.Errorf(
		"%w: Can not parse UDP packet type %d with length %d: %w", Error, hdr.MessageType, n, err)))) {
		return false
	}
	// Do not end the listening, there might come more (valid) responses
	return true
}

func sendUDPResult[T any](ctx context.Context, ch chan<- Result[T], result Result[T]) bool {
	select {
	case ch <- result:
		return true
	case <-ctx.Done():
		return false
	}
}
