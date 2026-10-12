package sip

import (
	"context"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/joomcode/errorx"
)

type requestConn struct {
	conn          *net.UDPConn
	broadcastAddr *net.UDPAddr
}

// Broadcast sends request on all broadcast addresses of interfaceName and collects
// the responses of type TResp. timeout is the interval after which listening is
// interrupted to check whether ctx has been canceled.
func Broadcast[TResp PDU, TReq PDU](ctx context.Context, interfaceName string, request TReq, timeout time.Duration) (chan Result[TResp], error) {

	requestBytes, err := MarshalPDU(request)

	if err != nil {
		return nil, err
	}

	reqConns, err := getReqConnsForIfc(interfaceName)
	if err != nil {
		return nil, err
	}

	ch := make(chan Result[TResp], 512) // Such many devices should be a pretty uncommon case
	var wg sync.WaitGroup

	for _, conn := range reqConns {
		wg.Add(1)
		go broadcast(ctx, &wg, conn, requestBytes, ch, timeout)
	}

	go func() {
		wg.Wait()
		close(ch)
	}()

	return ch, nil
}

func getReqConnsForIfc(interfaceName string) (reqConns []*requestConn, err error) {
	ifcs, err := net.Interfaces()
	if err != nil {
		return nil, errorx.EnsureStackTrace(fmt.Errorf("%w: Can not read system interfaces %w", Error, err))
	}

	for _, ifc := range ifcs {
		if ifc.Name != interfaceName {
			continue
		}
		addrs, err := ifc.Addrs()
		if err != nil {
			return nil, errorx.EnsureStackTrace(fmt.Errorf("%w: Can not read addresses of interface %s: %w", Error, interfaceName, err))
		}
		for _, addr := range addrs {
			reqConn, err := addrToReqConn(addr)
			if err != nil {
				return nil, err
			}
			if reqConn == nil {
				continue
			}
			reqConns = append(reqConns, reqConn)
		}
	}

	if len(reqConns) == 0 {
		return nil, errorx.EnsureStackTrace(fmt.Errorf("interface %s has no ipv4 addresses", interfaceName))
	}
	return reqConns, nil
}

func addrToReqConn(addr net.Addr) (*requestConn, error) {
	ipAddr, ok := addr.(*net.IPNet)
	if !ok {
		return nil, nil
	}
	ip := ipAddr.IP.To4()
	if ip == nil {
		return nil, nil
	}
	mask := ipAddr.Mask
	broadcast := make(net.IP, 4)
	for i := range 4 {
		broadcast[i] = ip[i] | ^mask[i]
	}
	broadcastAddr := &net.UDPAddr{IP: broadcast, Port: 35021}

	// Keep the socket bound while sending so fast replies cannot arrive between
	// closing the sender and binding a receiver to its ephemeral port.
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4zero})
	if err != nil {
		return nil, errorx.EnsureStackTrace(err)
	}
	return &requestConn{conn: conn, broadcastAddr: broadcastAddr}, nil
}

func broadcast[TResp PDU](ctx context.Context, wg *sync.WaitGroup, conn *requestConn, requestBytes []byte, ch chan Result[TResp], timeout time.Duration) {
	defer wg.Done()
	defer conn.conn.Close()

	_, err := conn.conn.WriteToUDP(requestBytes, conn.broadcastAddr)
	if err != nil {
		ch <- Err[TResp](errorx.EnsureStackTrace(err))
		return
	}

	for {
		select {
		case <-ctx.Done():
			return
		default:
			// Blocks until a reponse comes in or the timeout elapses
			if !listenUDP(conn.conn, timeout, ch) {
				return
			}
		}
	}
}
