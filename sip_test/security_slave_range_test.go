package sip_test

import (
	"context"
	"io"
	"testing"

	"github.com/philippseith/gosip/sip"
	"github.com/stretchr/testify/require"
)

type recordingSource struct {
	pingSource
	slaves chan [2]int
}

func (s recordingSource) ReadOnlyData(slave, ext int, _ uint32, _ ...sip.RequestOption) (sip.ReadOnlyDataResponse, error) {
	s.slaves <- [2]int{slave, ext}
	return sip.ReadOnlyDataResponse{}, nil
}

// Conn.WriteData rejects slave indexes and extensions outside 0-65535; the read
// methods must do the same instead of silently addressing another slave
// (65536 -> 0, -1 -> 65535).
func TestConnReadsRejectOutOfRangeSlaveAddress(t *testing.T) {
	listener := NewListener()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	source := recordingSource{slaves: make(chan [2]int, 4)}
	require.NoError(t, sip.Serve(ctx, listener, source))
	conn, err := sip.Dial(ctx, "", "", sip.WithDial(func(context.Context, string, string) (io.ReadWriteCloser, error) {
		return listener.Dial("", "")
	}))
	require.NoError(t, err)
	defer conn.Close()

	for _, addr := range [][2]int{{65536, 0}, {0, 65536}, {-1, 0}, {0, -1}} {
		_, err := conn.ReadOnlyData(ctx, addr[0], addr[1], 1)
		select {
		case got := <-source.slaves:
			t.Errorf("ReadOnlyData(%d, %d) was sent to slave %d extension %d; err = %v", addr[0], addr[1], got[0], got[1], err)
		default:
			require.Error(t, err, "ReadOnlyData(%d, %d)", addr[0], addr[1])
		}
	}
}
