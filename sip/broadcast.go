package sip

import (
	"context"
	"net"
	"sync"

	"github.com/joomcode/errorx"
)

func broadcast[TResp any](ctx context.Context, conns []*net.UDPConn, request []byte, listen func(*net.UDPConn, chan<- Result[*TResp]) bool) chan Result[*TResp] {

	ch := make(chan Result[*TResp], 512) // Such many devices should be a pretty uncommon case
	var wg sync.WaitGroup

	for _, conn := range conns {
		wg.Add(1)

		go func() {
			defer wg.Done()

			localPort := conn.LocalAddr().(*net.UDPAddr).Port
			_, err := conn.Write(request)
			if err != nil {
				ch <- Err[*TResp](errorx.EnsureStackTrace(err))
				return
			}
			// The drives are responding on our local port but with the broadcast address.
			// To allow listening on the port, we need to close the sending connection
			// and open a new one for listening.
			conn.Close()

			listenAddr := &net.UDPAddr{IP: net.IPv4zero, Port: localPort}
			respConn, err := net.ListenUDP("udp", listenAddr)
			if err != nil {
				ch <- Err[*TResp](errorx.EnsureStackTrace(err))
				return
			}
			defer respConn.Close()

			for {
				select {
				case <-ctx.Done():
					return
				default:
					// Blocks until a reponse comes in or a 1 sec timeout elapses
					if !listen(respConn, ch) {
						return
					}
				}
			}

		}()
	}

	go func() {
		wg.Wait()
		close(ch)
	}()

	return ch
}
