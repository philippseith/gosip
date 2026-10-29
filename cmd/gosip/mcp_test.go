package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/philippseith/gosip/sip"
)

type fakeMCPClient struct {
	sip.SyncClient
	mu              sync.Mutex
	lease           time.Duration
	closed          bool
	closeCalls      int
	pingErr         error
	readErr         error
	writeErr        error
	readEverything  sip.ReadEverythingResponse
	readOnlyData    sip.ReadOnlyDataResponse
	readDescription sip.ReadDescriptionResponse
	readDataState   sip.ReadDataStateResponse
	writeCalls      int
	written         []byte
}

func newFakeMCPClient(lease time.Duration) *fakeMCPClient {
	client := &fakeMCPClient{lease: lease}
	client.readOnlyData = sip.ReadOnlyDataResponse{Data: []byte{0xe8, 0x03}}
	client.readOnlyData.Attribute = 0x03110001
	client.readOnlyData.DataLength = 2
	return client
}

func (c *fakeMCPClient) LeaseTimeout() time.Duration {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.lease
}

func (c *fakeMCPClient) Closed() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.closed
}

func (c *fakeMCPClient) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closed = true
	c.closeCalls++
	return nil
}

func (c *fakeMCPClient) setClosed() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closed = true
}

func (c *fakeMCPClient) closeCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.closeCalls
}

func (c *fakeMCPClient) Ping(...sip.RequestOption) error { return c.pingErr }

func (c *fakeMCPClient) ReadEverything(int, int, uint32, ...sip.RequestOption) (sip.ReadEverythingResponse, error) {
	return c.readEverything, c.readErr
}

func (c *fakeMCPClient) ReadOnlyData(int, int, uint32, ...sip.RequestOption) (sip.ReadOnlyDataResponse, error) {
	return c.readOnlyData, c.readErr
}

func (c *fakeMCPClient) ReadDescription(int, int, uint32, ...sip.RequestOption) (sip.ReadDescriptionResponse, error) {
	return c.readDescription, c.readErr
}

func (c *fakeMCPClient) ReadDataState(int, int, uint32, ...sip.RequestOption) (sip.ReadDataStateResponse, error) {
	return c.readDataState, c.readErr
}

func (c *fakeMCPClient) WriteData(_ int, _ int, _ uint32, data []byte, _ ...sip.RequestOption) error {
	c.writeCalls++
	c.written = append(c.written[:0], data...)
	return c.writeErr
}

type cacheDialState struct {
	dials     int
	addresses []string
	clients   []*fakeMCPClient
}

func newCacheForTest(defaultAddress string, defaultLease, clientLease time.Duration, now *time.Time) (*clientCache, *cacheDialState) {
	state := &cacheDialState{}
	cache := newClientCache(defaultAddress, defaultLease, func(address string) (mcpClient, error) {
		state.dials++
		state.addresses = append(state.addresses, address)
		client := newFakeMCPClient(clientLease)
		state.clients = append(state.clients, client)
		return client, nil
	})
	if now != nil {
		cache.now = func() time.Time { return *now }
	}
	return cache, state
}

func cacheSize(cache *clientCache) int {
	cache.mu.Lock()
	defer cache.mu.Unlock()
	return len(cache.clients)
}

func TestClientCache(t *testing.T) {
	t.Run("address normalization, closed clients, and close", func(t *testing.T) {
		now := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
		cache, state := newCacheForTest("10.0.0.6", 5*time.Second, 10*time.Second, &now)
		first, release, err := cache.get("10.0.0.5")
		if err != nil {
			t.Fatal(err)
		}
		release()
		second, release, err := cache.get("10.0.0.5:35021")
		if err != nil {
			t.Fatal(err)
		}
		release()
		if first != second || state.dials != 1 {
			t.Fatalf("normalized addresses used %d dials and clients %p/%p, want one shared client", state.dials, first, second)
		}

		_, release, err = cache.get("")
		if err != nil {
			t.Fatal(err)
		}
		release()
		if state.dials != 2 || state.addresses[1] != "10.0.0.6:35021" {
			t.Fatalf("empty address dialed %q; dials=%d", state.addresses[1], state.dials)
		}

		state.clients[0].setClosed()
		replacement, release, err := cache.get("10.0.0.5")
		if err != nil {
			t.Fatal(err)
		}
		release()
		if replacement == first || state.dials != 3 {
			t.Fatalf("closed client was not replaced: dials=%d", state.dials)
		}
		inUse, releaseInUse, err := cache.get("10.0.0.5")
		if err != nil {
			t.Fatal(err)
		}
		if err := cache.Close(); err != nil {
			t.Fatal(err)
		}
		releaseInUse()
		if state.clients[1].closeCount() != 1 || state.clients[2].closeCount() != 1 {
			t.Fatalf("Close did not close every cached client: default=%d replacement=%d", state.clients[1].closeCount(), state.clients[2].closeCount())
		}
		if inUse != replacement {
			t.Fatal("cache returned a different client for an active address")
		}
	})

	t.Run("dial errors are not cached", func(t *testing.T) {
		wantErr := errors.New("dial failed")
		attempts := 0
		cache := newClientCache("10.0.0.5", time.Second, func(string) (mcpClient, error) {
			attempts++
			if attempts == 1 {
				return nil, wantErr
			}
			return newFakeMCPClient(time.Second), nil
		})
		t.Cleanup(func() { _ = cache.Close() })
		if _, _, err := cache.get(""); !errors.Is(err, wantErr) {
			t.Fatalf("first get error = %v, want %v", err, wantErr)
		}
		client, release, err := cache.get("")
		if err != nil || client == nil || attempts != 2 {
			t.Fatalf("second get = (%v, %v), attempts=%d", client, err, attempts)
		}
		release()
	})

	t.Run("idle threshold and refreshed usage", func(t *testing.T) {
		now := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
		cache, state := newCacheForTest("10.0.0.5", 5*time.Second, 10*time.Second, &now)
		t.Cleanup(func() { _ = cache.Close() })
		_, release, err := cache.get("")
		if err != nil {
			t.Fatal(err)
		}
		release()
		now = now.Add(19 * time.Second)
		cache.evictIdle()
		if cacheSize(cache) != 1 || state.clients[0].closeCount() != 0 {
			t.Fatal("client expired before twice its lease timeout")
		}
		now = now.Add(time.Second)
		cache.evictIdle()
		if cacheSize(cache) != 0 || state.clients[0].closeCount() != 1 {
			t.Fatal("client was not evicted at twice its lease timeout")
		}
		_, release, err = cache.get("")
		if err != nil {
			t.Fatal(err)
		}
		release()
		if state.dials != 2 {
			t.Fatalf("get after expiry used %d dials, want 2", state.dials)
		}

		_, release, err = cache.get("")
		if err != nil {
			t.Fatal(err)
		}
		release()
		now = now.Add(15 * time.Second)
		_, release, err = cache.get("")
		if err != nil {
			t.Fatal(err)
		}
		release()
		now = now.Add(19 * time.Second)
		cache.evictIdle()
		if cacheSize(cache) != 1 {
			t.Fatal("refreshed client expired before 20 seconds of idle time")
		}
		now = now.Add(time.Second)
		cache.evictIdle()
		if cacheSize(cache) != 0 {
			t.Fatal("refreshed client was not evicted after 20 seconds of idle time")
		}
	})

	t.Run("in-use entries and fallback lease", func(t *testing.T) {
		now := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
		cache, state := newCacheForTest("10.0.0.5", 5*time.Second, 10*time.Second, &now)
		t.Cleanup(func() { _ = cache.Close() })
		_, release, err := cache.get("")
		if err != nil {
			t.Fatal(err)
		}
		now = now.Add(time.Hour)
		cache.evictIdle()
		if cacheSize(cache) != 1 || state.clients[0].closeCount() != 0 {
			t.Fatal("in-use client was evicted")
		}
		release()
		cache.evictIdle()
		now = now.Add(19 * time.Second)
		cache.evictIdle()
		if cacheSize(cache) != 1 {
			t.Fatal("client expired before a full idle lease")
		}
		now = now.Add(time.Second)
		cache.evictIdle()
		if cacheSize(cache) != 0 {
			t.Fatal("released client was not evicted after its idle timeout")
		}

		fallbackNow := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
		fallback, fallbackState := newCacheForTest("10.0.0.5", 5*time.Second, 0, &fallbackNow)
		t.Cleanup(func() { _ = fallback.Close() })
		_, release, err = fallback.get("")
		if err != nil {
			t.Fatal(err)
		}
		release()
		release()
		fallbackNow = fallbackNow.Add(9 * time.Second)
		fallback.evictIdle()
		if fallbackSize := cacheSize(fallback); fallbackSize != 1 {
			t.Fatalf("fallback client expired at 9 seconds: cache size %d", fallbackSize)
		}
		fallbackNow = fallbackNow.Add(time.Second)
		fallback.evictIdle()
		if cacheSize(fallback) != 0 || fallbackState.clients[0].closeCount() != 1 {
			t.Fatal("fallback timeout or once-only release did not evict at 10 seconds")
		}
	})
}

func TestClientCacheTimerEvicts(t *testing.T) {
	var clients []*fakeMCPClient
	dials := 0
	cache := newClientCache("10.0.0.5", time.Second, func(string) (mcpClient, error) {
		dials++
		client := newFakeMCPClient(10 * time.Millisecond)
		clients = append(clients, client)
		return client, nil
	})
	t.Cleanup(func() { _ = cache.Close() })
	_, release, err := cache.get("")
	if err != nil {
		t.Fatal(err)
	}
	release()
	deadline := time.After(2 * time.Second)
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	for clients[0].closeCount() == 0 {
		select {
		case <-ticker.C:
		case <-deadline:
			t.Fatal("idle timer did not close the client")
		}
	}
	_, release, err = cache.get("")
	if err != nil {
		t.Fatal(err)
	}
	release()
	if dials != 2 {
		t.Fatalf("get after timer eviction used %d dials, want 2", dials)
	}
}

func connectMCP(t *testing.T, cache *clientCache) *mcp.ClientSession {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	serverSession, err := newMCPServer(cache, nil).Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatalf("server Connect: %v", err)
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "test"}, nil)
	clientSession, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		_ = serverSession.Close()
		t.Fatalf("client Connect: %v", err)
	}
	t.Cleanup(func() {
		_ = clientSession.Close()
		_ = serverSession.Close()
	})
	return clientSession
}

func callMCPTool(t *testing.T, session *mcp.ClientSession, name string, args map[string]any) *mcp.CallToolResult {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("CallTool(%s): %v", name, err)
	}
	return result
}

func structuredMCPContent(t *testing.T, result *mcp.CallToolResult) map[string]any {
	t.Helper()
	encoded, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatalf("marshal structured content: %v", err)
	}
	var content map[string]any
	if err := json.Unmarshal(encoded, &content); err != nil {
		t.Fatalf("unmarshal structured content: %v", err)
	}
	return content
}

func mcpErrorText(t *testing.T, result *mcp.CallToolResult) string {
	t.Helper()
	if !result.IsError {
		t.Fatalf("tool result is not an error: %#v", result)
	}
	var messages []string
	for _, item := range result.Content {
		if text, ok := item.(*mcp.TextContent); ok {
			messages = append(messages, text.Text)
		}
	}
	return strings.Join(messages, "\n")
}

func TestMCPListTools(t *testing.T) {
	cache, _ := newCacheForTest("10.0.0.5", time.Second, time.Second, nil)
	t.Cleanup(func() { _ = cache.Close() })
	session := connectMCP(t, cache)
	result, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, tool := range result.Tools {
		names = append(names, tool.Name)
	}
	sort.Strings(names)
	want := []string{"browse", "identify", "ping", "readdatastate", "readdescription", "readeverything", "readonlydata", "setip", "writedata"}
	if !bytes.Equal([]byte(strings.Join(names, "\n")), []byte(strings.Join(want, "\n"))) {
		t.Fatalf("tools = %v, want %v", names, want)
	}
}

func TestMCPReadOnlyData(t *testing.T) {
	client := newFakeMCPClient(0)
	now := time.Now()
	cache, _ := newCacheForTest("10.0.0.5", time.Second, 0, &now)
	cache.dial = func(string) (mcpClient, error) { return client, nil }
	t.Cleanup(func() { _ = cache.Close() })
	session := connectMCP(t, cache)
	result := callMCPTool(t, session, "readonlydata", map[string]any{"idn": "S-0-0001"})
	if result.IsError {
		t.Fatalf("readonlydata returned error: %s", mcpErrorText(t, result))
	}
	content := structuredMCPContent(t, result)
	if content["idn"] != "S-0-0001.0.0" {
		t.Errorf("idn = %v", content["idn"])
	}
	attribute := content["attribute"].(map[string]any)
	if attribute["format"] != "unsigned" || attribute["decimalPlaces"] != float64(3) {
		t.Errorf("attribute = %#v", attribute)
	}
	data := content["data"].(map[string]any)
	if data["length"] != float64(2) || data["raw"] != "e8 03" {
		t.Errorf("data = %#v", data)
	}
	elements := data["elements"].([]any)
	if len(elements) != 1 || elements[0] != "1.000" {
		t.Errorf("elements = %#v, want [1.000]", elements)
	}
}

func TestMCPReadEverythingGating(t *testing.T) {
	client := newFakeMCPClient(0)
	client.readEverything.ValidElements = sip.ElmName | sip.ElmAttribute | sip.ElmData
	client.readEverything.Attribute = 0x03110001
	client.readEverything.Name = []byte("sample")
	client.readEverything.Data = []byte{0xe8, 0x03}
	now := time.Now()
	cache, _ := newCacheForTest("10.0.0.5", time.Second, 0, &now)
	cache.dial = func(string) (mcpClient, error) { return client, nil }
	t.Cleanup(func() { _ = cache.Close() })
	session := connectMCP(t, cache)
	result := callMCPTool(t, session, "readeverything", map[string]any{"idn": "S-0-0001"})
	if result.IsError {
		t.Fatalf("readeverything returned error: %s", mcpErrorText(t, result))
	}
	content := structuredMCPContent(t, result)
	valid := content["validElements"].([]any)
	if len(valid) != 3 || valid[0] != "Name" || valid[1] != "Attribute" || valid[2] != "Data" {
		t.Errorf("validElements = %#v", valid)
	}
	if content["name"] != "sample" {
		t.Errorf("name = %v", content["name"])
	}
	for _, absent := range []string{"unit", "dataState"} {
		if _, ok := content[absent]; ok {
			t.Errorf("%q is present without its valid-element bit", absent)
		}
	}
}

func TestMCPWriteData(t *testing.T) {
	client := newFakeMCPClient(0)
	now := time.Now()
	cache, _ := newCacheForTest("10.0.0.5", time.Second, 0, &now)
	cache.dial = func(string) (mcpClient, error) { return client, nil }
	t.Cleanup(func() { _ = cache.Close() })
	session := connectMCP(t, cache)

	result := callMCPTool(t, session, "writedata", map[string]any{"idn": "S-0-0001", "data": "1.000"})
	if result.IsError {
		t.Fatalf("formatted write returned error: %s", mcpErrorText(t, result))
	}
	content := structuredMCPContent(t, result)
	if content["bytesWritten"] != float64(2) || !bytes.Equal(client.written, []byte{0xe8, 0x03}) {
		t.Fatalf("formatted write result=%#v bytes=%v", content, client.written)
	}

	result = callMCPTool(t, session, "writedata", map[string]any{"idn": "S-0-0001", "data": "e8", "raw": true})
	if message := mcpErrorText(t, result); !strings.Contains(message, "does not match") {
		t.Fatalf("raw length error = %q", message)
	}
	if client.writeCalls != 1 {
		t.Fatalf("invalid raw data caused %d writes, want 1", client.writeCalls)
	}

	result = callMCPTool(t, session, "writedata", map[string]any{"idn": "S-0-0001", "data": "e8 03", "raw": true})
	if result.IsError {
		t.Fatalf("raw write returned error: %s", mcpErrorText(t, result))
	}
	if client.writeCalls != 2 || !bytes.Equal(client.written, []byte{0xe8, 0x03}) {
		t.Fatalf("raw write calls=%d bytes=%v", client.writeCalls, client.written)
	}
}

func TestMCPValidation(t *testing.T) {
	cache, _ := newCacheForTest("10.0.0.5", time.Second, 0, nil)
	t.Cleanup(func() { _ = cache.Close() })
	session := connectMCP(t, cache)
	cases := []struct {
		name string
		tool string
		args map[string]any
		want string
	}{
		{"missing IDN", "readonlydata", map[string]any{}, "idn is required"},
		{"invalid IDN", "readonlydata", map[string]any{"idn": "bogus"}, "invalid IDN"},
		{"slave range", "readonlydata", map[string]any{"idn": "S-0-0001", "slave": 70000}, "slave must be 0..65535"},
		{"missing data", "writedata", map[string]any{"idn": "S-0-0001"}, "data is required"},
		{"missing node", "identify", map[string]any{"interface": "lo"}, "node is required"},
		{"missing interface", "browse", map[string]any{}, "interface is required"},
		{"invalid listen", "browse", map[string]any{"interface": "lo", "listen": "abc"}, "invalid listen duration"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result := callMCPTool(t, session, tc.tool, tc.args)
			if message := mcpErrorText(t, result); !strings.Contains(message, tc.want) {
				t.Fatalf("error = %q, want substring %q", message, tc.want)
			}
		})
	}
}

func TestMCPAddressUsesCache(t *testing.T) {
	now := time.Now()
	cache, state := newCacheForTest("10.0.0.4", time.Second, 0, &now)
	t.Cleanup(func() { _ = cache.Close() })
	session := connectMCP(t, cache)
	for _, args := range []map[string]any{
		{"idn": "S-0-0001"},
		{"idn": "S-0-0001", "address": "10.0.0.5"},
		{"idn": "S-0-0001"},
	} {
		result := callMCPTool(t, session, "readonlydata", args)
		if result.IsError {
			t.Fatalf("readonlydata returned error: %s", mcpErrorText(t, result))
		}
	}
	if state.dials != 2 {
		t.Fatalf("addressed calls caused %d dials, want 2", state.dials)
	}
}
