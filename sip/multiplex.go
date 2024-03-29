package sip

import (
	"context"
	"log"
	"net"
)

func Multiplex(ctx context.Context, network, address, targetNetwork, targetAddress string, options ...ConnOption) (context.CancelFunc, error) {
	conn, err := dial(ctx, targetNetwork, targetAddress, options...)
	// TODO execute Connect to get the BusyTimeout and LeaseTimeout
	if err != nil {
		return nil, err
	}
	m := &multiplexer{
		target: conn,
		jobs:   map[multiplexJob][]chan PDU{},
	}
	listener, err := net.Listen(network, address)
	if err != nil {
		return nil, err
	}
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			default:
				conn, err := listener.Accept()
				if ctx.Err() != nil {
					return
				}
				if err != nil {
					continue
				}
				go m.handleConnection(ctx, conn)
			}
		}
	}()
	return nil, nil
}

type multiplexer struct {
	target *conn
	jobs   map[multiplexJob][]chan PDU
}

type multiplexJob struct {
	Request
	MessageType MessageType
}

func (m *multiplexer) handleConnection(ctx context.Context, conn net.Conn) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
			job, err := readPDU(conn)
			if ctx.Err() != nil {
				return
			}
			if err != nil {
				return
			}
			// TODO Handle Connect PDU
			m.listenToTarget(ctx, conn, job)
		}
	}
}

func readPDU(net.Conn) (multiplexJob, error) {
	// TODO: Implement this
	return multiplexJob{}, nil
}

func (m *multiplexer) listenToTarget(ctx context.Context, conn net.Conn, job multiplexJob) {
	ch := make(chan PDU, 1)
	if chans, ok := m.jobs[job]; !ok {
		m.jobs[job] = []chan PDU{ch}

		go m.sendReadToTarget(ctx, job)
		// TODO: Writes must not be multiplexed

	} else {
		m.jobs[job] = append(chans, ch)
	}
	// TODO Init a timer loop which sends busies and is cancelled when the response broadcasted

	go func() {
		defer close(ch)

		select {
		case <-ctx.Done():
			return
		case resp := <-ch:
			if err := resp.Write(conn); err != nil {
				log.Println(err)
			}
		}
	}()
}

func (m *multiplexer) sendReadToTarget(ctx context.Context, job multiplexJob) {
	switch job.MessageType {
	case PingRequestMsgType:
		sendReceiveAndBroadcast(ctx, m, job, &PingRequest{}, &PingResponse{})
	case ReadDataStateRequestMsgType:
		req := ReadDataStateRequest(job.Request)
		sendReceiveAndBroadcast(ctx, m, job, &req, &ReadDataStateResponse{})
	case ReadDescriptionRequestMsgType:
		req := ReadDescriptionRequest(job.Request)
		sendReceiveAndBroadcast(ctx, m, job, &req, &ReadDescriptionResponse{})
	case ReadEverythingRequestMsgType:
		req := ReadEverythingRequest(job.Request)
		sendReceiveAndBroadcast(ctx, m, job, &req, &ReadEverythingResponse{})
	case ReadOnlyDataRequestMsgType:
		req := ReadOnlyDataRequest(job.Request)
		sendReceiveAndBroadcast(ctx, m, job, &req, &ReadOnlyDataResponse{})
	}
}

func sendReceiveAndBroadcast[Response PDU](ctx context.Context, m *multiplexer, job multiplexJob, req PDU, resp Response) {
	if err := sendRequestWaitForResponseAndRead(ctx, m.target, req, resp); err != nil {
		log.Println(err)
	}
	m.broadcastResponse(job, resp)
}

func (m *multiplexer) broadcastResponse(job multiplexJob, resp PDU) {
	if chans, ok := m.jobs[job]; ok {
		for _, ch := range chans {
			cch := ch
			go func() { cch <- resp }()
		}
		delete(m.jobs, job)
	}
}
