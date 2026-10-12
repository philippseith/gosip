package sip

import (
	"context"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/joomcode/errorx"
)

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

func getReqConnsForIfc(interfaceName string) (reqConns []*net.UDPConn, err error) {
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

func addrToReqConn(addr net.Addr) (*net.UDPConn, error) {
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
	localAddr := &net.UDPAddr{IP: ip, Port: 0}
	broadcastAddr := &net.UDPAddr{IP: broadcast, Port: 35021}

	reqConn, err := net.DialUDP("udp", localAddr, broadcastAddr)
	if err != nil {
		return nil, errorx.EnsureStackTrace(err)
	}
	return reqConn, nil
}

func broadcast[TResp PDU](ctx context.Context, wg *sync.WaitGroup, conn *net.UDPConn, requestBytes []byte, ch chan Result[TResp], timeout time.Duration) {
	defer wg.Done()

	localPort := conn.LocalAddr().(*net.UDPAddr).Port
	_, err := conn.Write(requestBytes)
	if err != nil {
		ch <- Err[TResp](errorx.EnsureStackTrace(err))
		return
	}
	// The drives are responding on our local port but with the broadcast address.
	// To allow listening on the port, we need to close the sending connection
	// and open a new one for listening.
	if err := conn.Close(); err != nil {
		ch <- Err[TResp](errorx.EnsureStackTrace(err))
		return
	}

	listenAddr := &net.UDPAddr{IP: net.IPv4zero, Port: localPort}
	respConn, err := net.ListenUDP("udp", listenAddr)
	if err != nil {
		ch <- Err[TResp](errorx.EnsureStackTrace(err))
		return
	}
	defer respConn.Close()

	for {
		select {
		case <-ctx.Done():
			return
		default:
			// Blocks until a reponse comes in or the timeout elapses
			if !listenUDP(respConn, timeout, ch) {
				return
			}
		}
	}
}
