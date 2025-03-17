package sip_test

import (
	"crypto/rand"

	"github.com/philippseith/gosip/sip"
)

type SimpleSyncClient struct {
}

func (s SimpleSyncClient) Ping(options ...sip.RequestOption) error {
	return nil
}

func (s SimpleSyncClient) ReadEverything(slaveIndex, slaveExtension int, idn uint32, options ...sip.RequestOption) (sip.ReadEverythingResponse, error) {
	buf := make([]byte, 16)
	_, _ = rand.Read(buf)
	return sip.ReadEverythingResponse{
		Data: buf,
	}, nil
}

func (s SimpleSyncClient) ReadOnlyData(slaveIndex, slaveExtension int, idn uint32, options ...sip.RequestOption) (sip.ReadOnlyDataResponse, error) {
	return sip.ReadOnlyDataResponse{}, nil
}

func (s SimpleSyncClient) ReadDescription(slaveIndex, slaveExtension int, idn uint32, options ...sip.RequestOption) (sip.ReadDescriptionResponse, error) {
	return sip.ReadDescriptionResponse{}, nil
}

func (s SimpleSyncClient) ReadDataState(slaveIndex, slaveExtension int, idn uint32, options ...sip.RequestOption) (sip.ReadDataStateResponse, error) {
	return sip.ReadDataStateResponse{}, nil
}

func (s SimpleSyncClient) WriteData(slaveIndex, slaveExtension int, idn uint32, data []byte, options ...sip.RequestOption) error {
	return nil
}
