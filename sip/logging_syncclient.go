package sip

import "log"

// LoggingSyncClient is a wrapper around SyncClient that logs all method calls
// and forwards them to the inner SyncClient.
type LoggingSyncClient struct {
	inner SyncClient
}

// NewLoggingSyncClient creates a new LoggingSyncClient that wraps the given SyncClient.
func NewLoggingSyncClient(inner SyncClient) *LoggingSyncClient {
	return &LoggingSyncClient{inner: inner}
}

// Ping logs the call and forwards it to the inner SyncClient.
func (l *LoggingSyncClient) Ping(options ...RequestOption) error {
	log.Println("Ping called")
	return l.inner.Ping(options...)
}

// ReadEverything logs the call and forwards it to the inner SyncClient.
func (l *LoggingSyncClient) ReadEverything(slaveIndex, slaveExtension int, idn uint32, options ...RequestOption) (ReadEverythingResponse, error) {
	log.Printf("ReadEverything called with slaveIndex=%d, slaveExtension=%d, idn=%d", slaveIndex, slaveExtension, idn)
	return l.inner.ReadEverything(slaveIndex, slaveExtension, idn, options...)
}

// ReadOnlyData logs the call and forwards it to the inner SyncClient.
func (l *LoggingSyncClient) ReadOnlyData(slaveIndex, slaveExtension int, idn uint32, options ...RequestOption) (ReadOnlyDataResponse, error) {
	log.Printf("ReadOnlyData called with slaveIndex=%d, slaveExtension=%d, idn=%d", slaveIndex, slaveExtension, idn)
	return l.inner.ReadOnlyData(slaveIndex, slaveExtension, idn, options...)
}

// ReadDescription logs the call and forwards it to the inner SyncClient.
func (l *LoggingSyncClient) ReadDescription(slaveIndex, slaveExtension int, idn uint32, options ...RequestOption) (ReadDescriptionResponse, error) {
	log.Printf("ReadDescription called with slaveIndex=%d, slaveExtension=%d, idn=%d", slaveIndex, slaveExtension, idn)
	return l.inner.ReadDescription(slaveIndex, slaveExtension, idn, options...)
}

// ReadDataState logs the call and forwards it to the inner SyncClient.
func (l *LoggingSyncClient) ReadDataState(slaveIndex, slaveExtension int, idn uint32, options ...RequestOption) (ReadDataStateResponse, error) {
	log.Printf("ReadDataState called with slaveIndex=%d, slaveExtension=%d, idn=%d", slaveIndex, slaveExtension, idn)
	return l.inner.ReadDataState(slaveIndex, slaveExtension, idn, options...)
}

// WriteData logs the call and forwards it to the inner SyncClient.
func (l *LoggingSyncClient) WriteData(slaveIndex, slaveExtension int, idn uint32, data []byte, options ...RequestOption) error {
	log.Printf("WriteData called with slaveIndex=%d, slaveExtension=%d, idn=%d, data=%v", slaveIndex, slaveExtension, idn, data)
	return l.inner.WriteData(slaveIndex, slaveExtension, idn, data, options...)
}
