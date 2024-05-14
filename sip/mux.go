package sip

import (
	"context"
	"net"
	"sync"
)

// Mux creates a multiplexer which listens on the given listener and forwards
// the S/IP requests to the target. Reads are optimized by reading only once and
// broadcasting the response to all listeners. Mux is useful when the source has
// limited resources and can't handle a larger number of multiple connections.
func Mux(ctx context.Context, listener net.Listener, source Services, options ...ConnOption) error {
	return Serve(ctx, listener, NewMuxServices(source), options...)
}

// NewMuxServices creates a new multiplexer which forwards the requests to the source.
func NewMuxServices(source Services) Services {
	return &mux{
		source: source,
		jobs:   make(map[muxJob][]chan Result[any]),
	}
}

type mux struct {
	source Services
	jobs   map[muxJob][]chan Result[any]
	mx     sync.Mutex
}

type muxJob struct {
	Request
	MessageType MessageType
}

func (m *mux) Ping() error {
	result := <-m.enqueue(muxJob{
		MessageType: PingRequestMsgType,
	}, func() (any, error) {
		return nil, m.source.Ping()
	})
	return result.Err
}

func (m *mux) ReadEverything(slaveIndex, slaveExtension int, idn uint32) (ReadEverythingResponse, error) {
	result := <-m.enqueue(muxJob{
		MessageType: ReadEverythingRequestMsgType,
		Request: Request{
			SlaveIndex:     uint16(slaveIndex),
			SlaveExtension: uint16(slaveExtension),
			IDN:            idn,
		},
	}, func() (any, error) {
		return m.source.ReadEverything(slaveIndex, slaveExtension, idn)
	})
	return result.Ok.(ReadEverythingResponse), result.Err
}

func (m *mux) ReadOnlyData(slaveIndex, slaveExtension int, idn uint32) (ReadOnlyDataResponse, error) {
	result := <-m.enqueue(muxJob{
		MessageType: ReadOnlyDataRequestMsgType,
		Request: Request{
			SlaveIndex:     uint16(slaveIndex),
			SlaveExtension: uint16(slaveExtension),
			IDN:            idn,
		},
	}, func() (any, error) {
		return m.source.ReadOnlyData(slaveIndex, slaveExtension, idn)
	})
	return result.Ok.(ReadOnlyDataResponse), result.Err
}

func (m *mux) ReadDescription(slaveIndex, slaveExtension int, idn uint32) (ReadDescriptionResponse, error) {
	result := <-m.enqueue(muxJob{
		MessageType: ReadDescriptionRequestMsgType,
		Request: Request{
			SlaveIndex:     uint16(slaveIndex),
			SlaveExtension: uint16(slaveExtension),
			IDN:            idn,
		},
	}, func() (any, error) {
		return m.source.ReadDescription(slaveIndex, slaveExtension, idn)
	})
	return result.Ok.(ReadDescriptionResponse), result.Err
}

func (m *mux) ReadDataState(slaveIndex, slaveExtension int, idn uint32) (ReadDataStateResponse, error) {
	result := <-m.enqueue(muxJob{
		MessageType: ReadDataStateRequestMsgType,
		Request: Request{
			SlaveIndex:     uint16(slaveIndex),
			SlaveExtension: uint16(slaveExtension),
			IDN:            idn,
		},
	}, func() (any, error) {
		return m.source.ReadDataState(slaveIndex, slaveExtension, idn)
	})
	return result.Ok.(ReadDataStateResponse), result.Err
}

func (m *mux) WriteData(slaveIndex, slaveExtension int, idn uint32, data []byte) error {
	return m.source.WriteData(slaveIndex, slaveExtension, idn, data)
}

func (m *mux) enqueue(job muxJob, do func() (any, error)) <-chan Result[any] {
	m.mx.Lock()
	defer m.mx.Unlock()

	ch := make(chan Result[any], 1)
	if chans, ok := m.jobs[job]; !ok {
		m.jobs[job] = []chan Result[any]{ch}

		go func() {
			result := NewResult(do())

			m.mx.Lock()
			defer m.mx.Unlock()

			if chans, ok := m.jobs[job]; ok {
				for _, ch := range chans {
					ch <- result
					close(ch)
				}
				delete(m.jobs, job)
			}
		}()
	} else {
		m.jobs[job] = append(chans, ch)
	}
	return ch
}
