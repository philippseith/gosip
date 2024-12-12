package sip

import (
	"bufio"
	"context"
	"errors"
	"io"
	"log"
	"net"

	"github.com/joomcode/errorx"
)

// Serve creates a server which listens on the given listener and forwards the S/IP requests to the source.
func Serve(ctx context.Context, listener net.Listener, source SyncClient, options ...ConnOption) error {
	server := &connServer{
		connOptions: connOptions{
			userBusyTimeout:  2000,
			userLeaseTimeout: 10000,
		},
		source: source,
	}

	for _, option := range options {
		if err := option(&server.connOptions); err != nil {
			return errorx.EnsureStackTrace(err)
		}
	}

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			default:
				conn, err := listener.Accept()
				if ctx.Err() != nil {
					if err := conn.Close(); err != nil {
						logger.Printf("accept, close connection: %+v", err)
					}
					return
				}
				if err != nil {
					log.Printf("accept: %+v", err)
					continue
				}
				server.conn = conn
				go server.serve(ctx)
			}
		}
	}()
	return nil
}

type connServer struct {
	connOptions
	conn   net.Conn
	source SyncClient
}

func (c connServer) serve(ctx context.Context) {
	defer func() {
		if err := c.conn.Close(); err != nil {
			logger.Printf("end serve: %+v", err)
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return
		default:
			if ctx.Err() != nil {
				return
			}
			if err := c.handleMessages(); err != nil {
				logger.Printf("%v: %+v", c.conn.RemoteAddr(), err)
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
		return c.handleConnect(h.TransactionID)
	case PingRequestMsgType:
		return c.handlePing(h.TransactionID)
	case ReadDataStateRequestMsgType:
		return c.handleDataState(h.TransactionID)
	case ReadDescriptionRequestMsgType:
		return c.handleDescription(h.TransactionID)
	case ReadEverythingRequestMsgType:
		return c.handleReadEverything(h.TransactionID)
	case ReadOnlyDataRequestMsgType:
		return c.handleReadOnlyData(h.TransactionID)
	case WriteDataRequestMsgType:
		return c.handleWriteData(h.TransactionID)
	default:
		return ErrorInvalidRequestMessageType
	}
}

func (c connServer) handleConnect(transactionID uint32) error {
	req := ConnectRequest{}
	err := req.Read(c.conn)
	if err != nil {
		return err
	}
	resp := &ConnectResponse{
		connectResponse: connectResponse{
			Version:        1,
			BusyTimeout:    c.userBusyTimeout,
			LeaseTimeout:   c.userLeaseTimeout,
			NoMessageTypes: 6,
		},
		MessageTypes: []uint32{
			uint32(PingRequestMsgType),
			uint32(ReadDataStateRequestMsgType),
			uint32(ReadDescriptionRequestMsgType),
			uint32(ReadEverythingRequestMsgType),
			uint32(ReadOnlyDataRequestMsgType),
			uint32(WriteDataRequestMsgType),
		},
	}
	return c.writeWithHeader(resp, transactionID)
}

func (c connServer) handlePing(transactionID uint32) error {
	req := PingRequest{}
	if err := req.Read(c.conn); err != nil {
		return err
	}
	resp := &PingResponse{}
	return c.writeWithHeader(resp, transactionID)
}

func (c connServer) handleDataState(transactionID uint32) error {
	req := ReadDataStateRequest{}
	return c.parseAndReadRequestAndWriteResponse(c.conn, &req, Request(req),
		func(slaveIndex, slaveExtension uint16, idn uint32) (PDU, error) {
			resp, err := c.source.ReadDataState(int(slaveIndex), int(slaveExtension), idn)
			return &resp, err
		}, transactionID)
}

func (c connServer) handleDescription(transactionID uint32) error {
	req := ReadDescriptionRequest{}
	return c.parseAndReadRequestAndWriteResponse(c.conn, &req, Request(req),
		func(slaveIndex, slaveExtension uint16, idn uint32) (PDU, error) {
			resp, err := c.source.ReadDescription(int(slaveIndex), int(slaveExtension), idn)
			return &resp, err
		}, transactionID)
}

func (c connServer) handleReadEverything(transactionID uint32) error {
	req := ReadEverythingRequest{}
	return c.parseAndReadRequestAndWriteResponse(c.conn, &req, Request(req),
		func(slaveIndex, slaveExtension uint16, idn uint32) (PDU, error) {
			resp, err := c.source.ReadEverything(int(slaveIndex), int(slaveExtension), idn)
			return &resp, err
		}, transactionID)
}

func (c connServer) handleReadOnlyData(transactionID uint32) error {
	req := ReadOnlyDataRequest{}
	return c.parseAndReadRequestAndWriteResponse(c.conn, &req, Request(req),
		func(slaveIndex, slaveExtension uint16, idn uint32) (PDU, error) {
			resp, err := c.source.ReadOnlyData(int(slaveIndex), int(slaveExtension), idn)
			return &resp, err
		}, transactionID)
}

func (c connServer) handleWriteData(transactionID uint32) error {
	req := WriteDataRequest{}
	if err := req.Read(c.conn); err != nil {
		return err
	}
	err := c.source.WriteData(int(req.SlaveIndex), int(req.SlaveExtension), req.IDN, req.Data)
	ex := Exception{}
	if errors.As(err, &ex) {
		return ex.Write(c.conn)
	}
	if err == nil {
		resp := &WriteDataResponse{}
		return c.writeWithHeader(resp, transactionID)
	}
	return err
}

func (c connServer) parseAndReadRequestAndWriteResponse(conn io.ReadWriteCloser, reqPDU PDU,
	req Request, read func(uint16, uint16, uint32) (PDU, error), transactionID uint32) error {
	err := reqPDU.Read(conn)
	if err != nil {
		return err
	}
	resp, err := read(req.SlaveIndex, req.SlaveExtension, req.IDN)
	ex := Exception{}
	if errors.As(err, &ex) {
		return ex.Write(c.conn)
	}
	if err == nil {
		return c.writeWithHeader(resp, transactionID)
	}
	return err
}

func (c connServer) writeWithHeader(pdu PDU, transactionID uint32) error {
	writer := bufio.NewWriterSize(c.conn, 1460) // TODO Use MSS of socket
	header := Header{TransactionID: transactionID, MessageType: pdu.MessageType()}
	if err := header.Write(writer); err != nil {
		return err
	}
	return errors.Join(pdu.Write(writer), writer.Flush())
}
