package sip

import (
	"bytes"
	"errors"
	"fmt"
	"net"
	"os"
	"time"

	"braces.dev/errtrace"
)

func newUDPConn(ip net.IP) (*net.UDPConn, error) {
	listenAddr, err := net.ResolveUDPAddr("udp4", fmt.Sprintf("%s:0", ip.String()))
	if err != nil {
		return nil, err
	}
	return net.ListenUDP("udp4", listenAddr)
}

func sendUDP(conn net.PacketConn, address string, pdu PDU) error {
	writer := bytes.NewBuffer(make([]byte, 0, 17))
	// Write header to buffer
	hdr := Header{
		TransactionID: 1,
		MessageType:   BrowseRequestMsgType,
	}
	err := hdr.Write(writer)
	if err != nil {
		return errtrace.Wrap(err)
	}
	err = pdu.Write(writer)
	if err != nil {
		return errtrace.Wrap(err)
	}
	// Create target address
	targetAddr, err := net.ResolveUDPAddr("udp4", address+":35021")
	if err != nil {
		return errtrace.Wrap(err)
	}
	// Send the request
	_, err = conn.WriteTo(writer.Bytes(), targetAddr)
	return errtrace.Wrap(err)
}

func listenUDP[T PDU](conn net.PacketConn, timeout time.Duration, newResponse func() T, ch chan<- Result[T]) bool {
	err := conn.SetReadDeadline(time.Now().Add(timeout))
	if err != nil {
		ch <- Err[T](errtrace.Wrap(err))
		// If setting the deadline does not work,
		// the go func might not end. We break here.
		return false
	}
	buf := make([]byte, 1024)
	n, _, err := conn.ReadFrom(buf)
	if err != nil {
		// Timeouts are expected and used to check ctx.Done() in regular intervals
		// See https://pkg.go.dev/net@go1.18.3#Conn.SetDeadline
		if errors.Is(err, os.ErrDeadlineExceeded) {
			return true
		}
		ch <- Err[T](errtrace.Wrap(err))
		// If ReadFrom errored with something different we need to stop
		return false
	}
	reader := bytes.NewReader(buf[:n])
	hdr := Header{}
	err = hdr.Read(reader)
	if err != nil || hdr.MessageType != BrowseResponseMsgType {
		return true
	}
	resp := newResponse()
	err = resp.Read(reader)
	if err == nil {
		ch <- Ok[T](resp)
	} else {
		ch <- Err[T](errtrace.Wrap(fmt.Errorf(
			"%w: Can not parse packet %v: %w", Error, buf[:n], err)))
		// Do not end the listening, there might come more (valid) responses
	}
	return true
}
