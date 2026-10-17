package sip

import (
	"fmt"
	"sync"
	"time"

	"github.com/joomcode/errorx"
)

// NewMux creates a multiplexer which can be used with multiple Serve calls to forward
// the S/IP requests to the underlying source. Reads are optimized by reading only once and
// broadcasting the response to the listeners of all Serve calls. NewMux is useful when the source has
// limited resources and can't handle a larger number of multiple connections.

// When used with Serve, positive negotiated busy and lease timeouts from the
// source are advertised downstream. If the source exposes timeout properties
// that are not negotiated yet, it is pinged before the downstream Connect; a
// Ping error fails that Connect. If no positive values are available, Serve's
// configured timeout values remain in effect.
//
// Example:
//
//	mux := sip.NewMux(existingSyncClient)
func NewMux(source SyncClient) SyncClient {
	return &mux{
		source: source,
		jobs:   make(map[muxJob][]chan Result[any]),
	}
}

type mux struct {
	source SyncClient
	jobs   map[muxJob][]chan Result[any]
	mx     sync.Mutex
}

type timeoutProvider interface {
	BusyTimeout() time.Duration
	LeaseTimeout() time.Duration
}

func (m *mux) negotiatedTimeouts() (busy, lease time.Duration, available bool, err error) {
	provider, ok := m.source.(timeoutProvider)
	if !ok {
		return 0, 0, false, nil
	}

	busy, lease = provider.BusyTimeout(), provider.LeaseTimeout()
	if busy > 0 && lease > 0 {
		return busy, lease, true, nil
	}

	if err := m.source.Ping(); err != nil {
		return 0, 0, false, err
	}

	busy, lease = provider.BusyTimeout(), provider.LeaseTimeout()
	if busy <= 0 || lease <= 0 {
		return busy, lease, false, nil
	}
	return busy, lease, true, nil
}

type muxJob struct {
	Request
	MessageType MessageType
}

func (m *mux) Ping(options ...RequestOption) error {
	result := <-m.enqueue(muxJob{
		MessageType: PingRequestMsgType,
	}, func() (any, error) {
		return nil, m.source.Ping(options...)
	})
	return result.Err
}

func (m *mux) ReadEverything(slaveIndex, slaveExtension int, idn uint32, options ...RequestOption) (ReadEverythingResponse, error) {
	if slaveIndex < 0 || slaveIndex > 0xFFFF {
		return ReadEverythingResponse{}, errorx.EnsureStackTrace(fmt.Errorf("slaveIndex out of range [0-65535]: %v", slaveIndex))
	}
	u16slaveIndex := uint16(slaveIndex)
	if slaveExtension < 0 || slaveExtension > 0xFFFF {
		return ReadEverythingResponse{}, errorx.EnsureStackTrace(fmt.Errorf("slaveExtension out of range [0-65535]: %v", slaveExtension))
	}
	u16slaveExtension := uint16(slaveExtension)
	result := <-m.enqueue(muxJob{
		MessageType: ReadEverythingRequestMsgType,
		Request: Request{
			SlaveIndex:     u16slaveIndex,
			SlaveExtension: u16slaveExtension,
			IDN:            idn,
		},
	}, func() (any, error) {
		return m.source.ReadEverything(slaveIndex, slaveExtension, idn, options...)
	})
	return result.Ok.(ReadEverythingResponse), result.Err
}

func (m *mux) ReadOnlyData(slaveIndex, slaveExtension int, idn uint32, options ...RequestOption) (ReadOnlyDataResponse, error) {
	if slaveIndex < 0 || slaveIndex > 0xFFFF {
		return ReadOnlyDataResponse{}, errorx.EnsureStackTrace(fmt.Errorf("slaveIndex out of range [0-65535]: %v", slaveIndex))
	}
	u16slaveIndex := uint16(slaveIndex)
	if slaveExtension < 0 || slaveExtension > 0xFFFF {
		return ReadOnlyDataResponse{}, errorx.EnsureStackTrace(fmt.Errorf("slaveExtension out of range [0-65535]: %v", slaveExtension))
	}
	u16slaveExtension := uint16(slaveExtension)
	result := <-m.enqueue(muxJob{
		MessageType: ReadOnlyDataRequestMsgType,
		Request: Request{
			SlaveIndex:     u16slaveIndex,
			SlaveExtension: u16slaveExtension,
			IDN:            idn,
		},
	}, func() (any, error) {
		return m.source.ReadOnlyData(slaveIndex, slaveExtension, idn, options...)
	})
	return result.Ok.(ReadOnlyDataResponse), result.Err
}

func (m *mux) ReadDescription(slaveIndex, slaveExtension int, idn uint32, options ...RequestOption) (ReadDescriptionResponse, error) {
	if slaveIndex < 0 || slaveIndex > 0xFFFF {
		return ReadDescriptionResponse{}, errorx.EnsureStackTrace(fmt.Errorf("slaveIndex out of range [0-65535]: %v", slaveIndex))
	}
	u16slaveIndex := uint16(slaveIndex)
	if slaveExtension < 0 || slaveExtension > 0xFFFF {
		return ReadDescriptionResponse{}, errorx.EnsureStackTrace(fmt.Errorf("slaveExtension out of range [0-65535]: %v", slaveExtension))
	}
	u16slaveExtension := uint16(slaveExtension)
	result := <-m.enqueue(muxJob{
		MessageType: ReadDescriptionRequestMsgType,
		Request: Request{
			SlaveIndex:     u16slaveIndex,
			SlaveExtension: u16slaveExtension,
			IDN:            idn,
		},
	}, func() (any, error) {
		return m.source.ReadDescription(slaveIndex, slaveExtension, idn, options...)
	})
	return result.Ok.(ReadDescriptionResponse), result.Err
}

func (m *mux) ReadDataState(slaveIndex, slaveExtension int, idn uint32, options ...RequestOption) (ReadDataStateResponse, error) {
	if slaveIndex < 0 || slaveIndex > 0xFFFF {
		return ReadDataStateResponse{}, errorx.EnsureStackTrace(fmt.Errorf("slaveIndex out of range [0-65535]: %v", slaveIndex))
	}
	u16slaveIndex := uint16(slaveIndex)
	if slaveExtension < 0 || slaveExtension > 0xFFFF {
		return ReadDataStateResponse{}, errorx.EnsureStackTrace(fmt.Errorf("slaveExtension out of range [0-65535]: %v", slaveExtension))
	}
	u16slaveExtension := uint16(slaveExtension)
	result := <-m.enqueue(muxJob{
		MessageType: ReadDataStateRequestMsgType,
		Request: Request{
			SlaveIndex:     u16slaveIndex,
			SlaveExtension: u16slaveExtension,
			IDN:            idn,
		},
	}, func() (any, error) {
		return m.source.ReadDataState(slaveIndex, slaveExtension, idn, options...)
	})
	return result.Ok.(ReadDataStateResponse), result.Err
}

func (m *mux) WriteData(slaveIndex, slaveExtension int, idn uint32, data []byte, options ...RequestOption) error {
	return m.source.WriteData(slaveIndex, slaveExtension, idn, data, options...)
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
