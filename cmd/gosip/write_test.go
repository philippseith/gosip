package main

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/philippseith/gosip/sip"
)

type fakeDataClient struct {
	response sip.ReadOnlyDataResponse
	readErr  error
	writeErr error
	reads    int
	writes   int
	written  []byte
}

func (f *fakeDataClient) ReadOnlyData(_, _ int, _ uint32, _ ...sip.RequestOption) (sip.ReadOnlyDataResponse, error) {
	f.reads++
	return f.response, f.readErr
}

func (f *fakeDataClient) WriteData(_, _ int, _ uint32, data []byte, _ ...sip.RequestOption) error {
	f.writes++
	f.written = append([]byte(nil), data...)
	return f.writeErr
}

func newFakeDataClient(attr uint32, current []byte) *fakeDataClient {
	response := sip.ReadOnlyDataResponse{Data: append([]byte(nil), current...)}
	response.Attribute = attr
	return &fakeDataClient{response: response}
}

func TestWriteIDN(t *testing.T) {
	readErr := errors.New("read failed")
	tests := []struct {
		name       string
		input      string
		raw        bool
		attr       uint32
		current    []byte
		readErr    error
		want       []byte
		wantErr    string
		wantOutput string
	}{
		{
			name:       "formatted decimal",
			input:      "1.000",
			attr:       0x03110001,
			current:    []byte{0, 0},
			want:       []byte{0xe8, 0x03},
			wantOutput: "wrote 2 bytes to S-0-0001.0.0\n",
		},
		{
			name:    "raw separated bytes",
			input:   "e8 03",
			raw:     true,
			attr:    0x00110001,
			current: []byte{0, 0},
			want:    []byte{0xe8, 0x03},
		},
		{
			name:    "raw prefixed bytes",
			input:   "0xe803",
			raw:     true,
			attr:    0x00110001,
			current: []byte{0, 0},
			want:    []byte{0xe8, 0x03},
		},
		{
			name:    "raw wrong length",
			input:   "e8",
			raw:     true,
			attr:    0x00110001,
			current: []byte{0, 0},
			wantErr: "does not match",
		},
		{
			name:    "formatted wrong length",
			input:   "1 2",
			attr:    0x00110001,
			current: []byte{0, 0},
			wantErr: "does not match",
		},
		{
			name:    "formatted list elements",
			input:   "1 2 3",
			attr:    0x00150001,
			current: []byte{0, 0},
			want:    []byte{1, 0, 2, 0, 3, 0},
		},
		{
			name:    "raw incomplete list element",
			input:   "00 01 02",
			raw:     true,
			attr:    0x00150001,
			current: []byte{0, 0},
			wantErr: "multiple of",
		},
		{
			name:    "text preserves inner spaces",
			input:   "hello  world",
			attr:    0x00410001,
			current: []byte{'a', 'b'},
			want:    []byte("hello  world"),
		},
		{
			name:    "empty raw input",
			raw:     true,
			attr:    0x00110001,
			current: []byte{0, 0},
			wantErr: "no data to write",
		},
		{
			name:    "invalid formatted input",
			input:   "abc",
			attr:    0x00110001,
			current: []byte{0, 0},
			wantErr: "sercos:",
		},
		{
			name:    "read failure",
			input:   "1",
			attr:    0x00110001,
			current: []byte{0, 0},
			readErr: readErr,
			wantErr: "read failed",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := newFakeDataClient(tt.attr, tt.current)
			client.readErr = tt.readErr
			var out bytes.Buffer
			err := writeIDN(client, &out, 0, 0, 1, tt.input, tt.raw, nil)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("writeIDN() error = %v, want substring %q", err, tt.wantErr)
				}
				if client.writes != 0 {
					t.Fatalf("writes = %d, want 0", client.writes)
				}
				return
			}
			if err != nil {
				t.Fatalf("writeIDN(): %v", err)
			}
			if !bytes.Equal(client.written, tt.want) {
				t.Errorf("written = %v, want %v", client.written, tt.want)
			}
			if got := out.String(); tt.wantOutput != "" && got != tt.wantOutput {
				t.Errorf("output = %q, want %q", got, tt.wantOutput)
			}
		})
	}
}

func TestDoWriteData(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		want    []byte
		reads   int
		writes  int
		wantErr string
	}{
		{
			name:   "formatted default",
			args:   []string{"--idn", "1", "--data", "1.000"},
			want:   []byte{0xe8, 0x03},
			reads:  1,
			writes: 1,
		},
		{
			name:   "raw bypasses read",
			args:   []string{"--idn", "1", "--raw", "--data", "e803"},
			want:   []byte{0xe8, 0x03},
			reads:  0,
			writes: 1,
		},
		{
			name:    "data required",
			args:    []string{"--idn", "1"},
			wantErr: "--data is required",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := newFakeDataClient(0x03110001, []byte{0, 0})
			err := doWriteData(client, tt.args, nil)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("doWriteData() error = %v, want substring %q", err, tt.wantErr)
				}
			} else if err != nil {
				t.Fatalf("doWriteData(): %v", err)
			}
			if client.reads != tt.reads {
				t.Errorf("reads = %d, want %d", client.reads, tt.reads)
			}
			if client.writes != tt.writes {
				t.Errorf("writes = %d, want %d", client.writes, tt.writes)
			}
			if !bytes.Equal(client.written, tt.want) {
				t.Errorf("written = %v, want %v", client.written, tt.want)
			}
		})
	}
}
