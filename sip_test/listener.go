package sip_test

import (
	"errors"
	"net"
)

type Listener struct {
	state       chan struct{}
	connections chan net.Conn
}

func NewListener() *Listener {
	return &Listener{
		state:       make(chan struct{}),
		connections: make(chan net.Conn),
	}
}

func (l *Listener) Close() error {
	close(l.state)
	return nil
}

func (l *Listener) Addr() net.Addr {
	return nil
}

func (l *Listener) Accept() (net.Conn, error) {
	select {
	case newConnection := <-l.connections:
		return newConnection, nil
	case <-l.state:
		return nil, errors.New("Listener closed")
	}
}

func (l *Listener) Dial(string, string) (net.Conn, error) {
	// Check for being closed
	select {
	case <-l.state:
		return nil, errors.New("Listener closed")
	default:
	}
	// Create the in memory transport
	serverSide, clientSide := net.Pipe()
	// Pass half to the server
	l.connections <- serverSide
	// Return the other half to the client
	return clientSide, nil
}
