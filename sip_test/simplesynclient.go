package sip_test

import (
	"crypto/rand"
	"fmt"
	"time"

	"github.com/philippseith/gosip/sip"
)

type SimpleSyncClient struct {
}

func (s SimpleSyncClient) Ping(...sip.RequestOption) error {
	return nil
}

func (s SimpleSyncClient) ReadEverything(_, _ int, _ uint32, _ ...sip.RequestOption) (sip.ReadEverythingResponse, error) {
	<-time.After(10 * time.Millisecond) // Simulate some delay
	buf := make([]byte, 16)
	_, _ = rand.Read(buf)
	fmt.Printf("ReadEverything: %v\n", buf)
	return sip.ReadEverythingResponse{
		Data: buf,
	}, nil
}

func (s SimpleSyncClient) ReadOnlyData(_, _ int, _ uint32, _ ...sip.RequestOption) (sip.ReadOnlyDataResponse, error) {
	return sip.ReadOnlyDataResponse{}, nil
}

func (s SimpleSyncClient) ReadDescription(_, _ int, _ uint32, _ ...sip.RequestOption) (sip.ReadDescriptionResponse, error) {
	return sip.ReadDescriptionResponse{}, nil
}

func (s SimpleSyncClient) ReadDataState(_, _ int, _ uint32, _ ...sip.RequestOption) (sip.ReadDataStateResponse, error) {
	return sip.ReadDataStateResponse{}, nil
}

func (s SimpleSyncClient) WriteData(_, _ int, _ uint32, _ []byte, _ ...sip.RequestOption) error {
	return nil
}
