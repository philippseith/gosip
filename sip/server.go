package sip

import (
	"context"
	"io"
	"net"
)

func Serve(ctx context.Context, listener net.Listener, source SyncClient) {
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			default:
				conn, err := listener.Accept()
				if ctx.Err() != nil {
					conn.Close()
					return
				}
				if err != nil {
					continue
				}
				go connServer{conn: conn, source: source}.serve(ctx)
			}
		}
	}()
}

type connServer struct {
	conn   net.Conn
	source SyncClient
}

func (c connServer) serve(ctx context.Context) {
	defer c.conn.Close()

	for {
		select {
		case <-ctx.Done():
			return
		default:
			if ctx.Err() != nil {
				return
			}
			if err := c.handleMessages(); err != nil {
				logger.Println(err)
				return
			}
		}
	}
}

func (c connServer) handleMessages() error {
	h := &Header{}
	err := h.Read(c.conn)
	if err != nil {
		return err
	}
	switch h.MessageType {
	case ConnectRequestMsgType:
		return c.handleDataState()
	case PingRequestMsgType:
		return c.handleDataState()
	case ReadDataStateRequestMsgType:
		return c.handleDataState()
	case ReadDescriptionRequestMsgType:
		return c.handleDescription()
	case ReadEverythingRequestMsgType:
		return c.handleReadEverything()
	case ReadOnlyDataRequestMsgType:
		return c.handleReadOnlyData()
	case WriteDataRequestMsgType:
		return c.handleDataState()
	default:
		return ErrorInvalidRequestMessageType
	}
}

func (c connServer) handleDataState() error {
	req := ReadDataStateRequest{}
	return c.parseAndReadRequestAndWriteResponse(c.conn, &req, Request(req),
		func(slaveIndex, slaveExtension uint16, idn uint32) (PDU, error) {
			resp, err := c.source.ReadDataState(int(slaveIndex), int(slaveExtension), idn)
			return &resp, err
		})
}

func (c connServer) handleDescription() error {
	req := ReadDescriptionRequest{}
	return c.parseAndReadRequestAndWriteResponse(c.conn, &req, Request(req),
		func(slaveIndex, slaveExtension uint16, idn uint32) (PDU, error) {
			resp, err := c.source.ReadDescription(int(slaveIndex), int(slaveExtension), idn)
			return &resp, err
		})
}

func (c connServer) handleReadEverything() error {
	req := ReadEverythingRequest{}
	return c.parseAndReadRequestAndWriteResponse(c.conn, &req, Request(req),
		func(slaveIndex, slaveExtension uint16, idn uint32) (PDU, error) {
			resp, err := c.source.ReadEverything(int(slaveIndex), int(slaveExtension), idn)
			return &resp, err
		})
}

func (c connServer) handleReadOnlyData() error {
	req := ReadOnlyDataRequest{}
	return c.parseAndReadRequestAndWriteResponse(c.conn, &req, Request(req),
		func(slaveIndex, slaveExtension uint16, idn uint32) (PDU, error) {
			resp, err := c.source.ReadOnlyData(int(slaveIndex), int(slaveExtension), idn)
			return &resp, err
		})
}

func (c connServer) parseAndReadRequestAndWriteResponse(conn io.ReadWriteCloser, reqPDU PDU, req Request, read func(uint16, uint16, uint32) (PDU, error)) error {
	err := reqPDU.Read(conn)
	if err != nil {
		return err
	}
	resp, err := read(req.SlaveIndex, req.SlaveExtension, req.IDN)
	if err != nil {
		return err
	}
	err = resp.Write(conn)
	if err != nil {
		return err
	}
	return nil
}
