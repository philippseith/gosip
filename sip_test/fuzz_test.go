package sip_test

import (
	"bytes"
	"encoding/binary"
	"io"
	"runtime"
	"testing"

	"github.com/philippseith/gosip/sip"
)

// Allocation budgets of one PDU decode, in bytes. maxPDUFieldLength is 0xFFFF
// (sip/sip.go). The widest byte-field PDU (ReadEverythingResponse: name, unit,
// data) needs 3 x 0xFFFF; 4 x 0xFFFF leaves headroom. 64 KiB is runtime noise
// from the fuzz harness. ConnectResponse needs 2 x 4 x 0xFFFF because
// binary.Read of a []uint32 allocates a byte buffer next to the slice
// (measured: 525448 bytes for a 16 byte input).
const (
	decodeAlloc        = 4*0xFFFF + 64*1024
	decodeAllocConnect = 8*0xFFFF + 64*1024
)

func le(vs ...any) []byte {
	var b bytes.Buffer
	for _, v := range vs {
		if err := binary.Write(&b, binary.LittleEndian, v); err != nil {
			panic(err)
		}
	}
	return b.Bytes()
}

// fuzzDecode checks, for an arbitrary byte stream:
//   - Read never panics and returns,
//   - Read allocates at most maxDecodeAlloc,
//   - a successful Read is canonical: Write of the decoded PDU reproduces
//     exactly the bytes that Read consumed (parsing differential check).
func fuzzDecode(f *testing.F, budget uint64, newPDU func() sip.PDU, seeds ...[]byte) {
	f.Helper()
	for _, s := range seeds {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		reader := bytes.NewReader(data)
		pdu := newPDU()

		var before, after runtime.MemStats
		runtime.ReadMemStats(&before)
		err := pdu.Read(io.LimitReader(reader, int64(len(data))))
		runtime.ReadMemStats(&after)
		if alloc := after.TotalAlloc - before.TotalAlloc; alloc > budget {
			t.Fatalf("Read allocated %d bytes for %d input bytes (budget %d)", alloc, len(data), budget)
		}
		if err != nil {
			return
		}
		consumed := len(data) - reader.Len()
		var out bytes.Buffer
		if err := pdu.Write(&out); err != nil {
			t.Fatalf("Write after successful Read: %v", err)
		}
		if !bytes.Equal(out.Bytes(), data[:consumed]) {
			t.Fatalf("re-encoding differs from consumed input:\n in  %x\n out %x", data[:consumed], out.Bytes())
		}
	})
}

type headerPDU struct{ sip.Header }

func (h *headerPDU) MessageType() sip.MessageType { return h.Header.MessageType }

func FuzzHeader(f *testing.F) {
	fuzzDecode(f, decodeAlloc, func() sip.PDU { return &headerPDU{} }, le(uint32(1), uint32(sip.PingRequestMsgType)))
}

func FuzzConnectRequest(f *testing.F) {
	fuzzDecode(f, decodeAlloc, func() sip.PDU { return &sip.ConnectRequest{} }, le(uint32(1), uint32(2000), uint32(10000)))
}

func FuzzConnectResponse(f *testing.F) {
	fuzzDecode(f, decodeAllocConnect, func() sip.PDU { return &sip.ConnectResponse{} },
		le(uint32(1), uint32(2000), uint32(10000), uint32(2), uint32(65), uint32(69)),
		le(uint32(1), uint32(2000), uint32(10000), uint32(0xFFFF)),
		le(uint32(1), uint32(2000), uint32(10000), uint32(0xFFFFFFFF)))
}

func readEverythingSeed(nameLen, unitLen uint16, dataLen uint32, name, unit, data []byte) []byte {
	b := le(uint16(0x7F), uint16(1), nameLen, uint32(0x00910002), unitLen,
		[8]byte{1}, [8]byte{2}, uint32(100), dataLen)
	b = append(b, name...)
	b = append(b, unit...)
	return append(b, data...)
}

func FuzzReadEverythingResponse(f *testing.F) {
	fuzzDecode(f, decodeAlloc, func() sip.PDU { return &sip.ReadEverythingResponse{} },
		readEverythingSeed(4, 2, 4, []byte("name"), []byte("mm"), []byte{1, 2, 3, 4}),
		readEverythingSeed(0, 0, 0xFFFF, nil, nil, nil),
		readEverythingSeed(0xFFFF, 0xFFFF, 0xFFFF, nil, nil, nil),
		readEverythingSeed(0, 0, 0xFFFFFFFF, nil, nil, nil))
}

func FuzzReadOnlyDataResponse(f *testing.F) {
	fuzzDecode(f, decodeAlloc, func() sip.PDU { return &sip.ReadOnlyDataResponse{} },
		append(le(uint32(0x00910002), uint32(4)), 1, 2, 3, 4),
		le(uint32(0x00910002), uint32(0xFFFF)),
		le(uint32(0x00910002), uint32(0xFFFFFFFF)))
}

func FuzzReadDescriptionResponse(f *testing.F) {
	seed := func(nameLen, unitLen uint16) []byte {
		return le(uint16(0x3F), nameLen, uint32(0x00910002), unitLen, [8]byte{1}, [8]byte{2}, uint32(100))
	}
	fuzzDecode(f, decodeAlloc, func() sip.PDU { return &sip.ReadDescriptionResponse{} },
		append(seed(4, 2), []byte("namemm")...),
		seed(0xFFFF, 0xFFFF))
}

func FuzzBrowseResponse(f *testing.F) {
	seed := func(display, host uint32) []byte {
		b := le(uint32(1), [6]byte{1}, [6]byte{2}, byte(0), byte(1), [4]byte{10, 0, 0, 1}, [4]byte{255, 255, 255, 0},
			[4]byte{10, 0, 0, 254}, display)
		b = append(b, bytes.Repeat([]byte{'d'}, int(min(display, 8)))...)
		return append(b, le(host)...)
	}
	fuzzDecode(f, decodeAlloc, func() sip.PDU { return &sip.BrowseResponse{} },
		seed(0, 0), seed(4, 0), seed(0xFFFF, 0xFFFF), seed(0xFFFFFFFF, 0))
}

func FuzzSetIPRequest(f *testing.F) {
	seed := func(host uint32) []byte {
		return le([6]byte{1}, [6]byte{2}, byte(1), [4]byte{10, 0, 0, 1}, [4]byte{255, 255, 255, 0},
			[4]byte{10, 0, 0, 254}, byte(0), host)
	}
	fuzzDecode(f, decodeAlloc, func() sip.PDU { return &sip.SetIPRequest{} },
		append(seed(4), []byte("host")...), seed(0xFFFF), seed(0xFFFFFFFF))
}

func FuzzWriteDataRequest(f *testing.F) {
	seed := func(n uint32) []byte {
		return le(uint16(0), uint16(0), uint32(0x00910002), n)
	}
	fuzzDecode(f, decodeAlloc, func() sip.PDU { return &sip.WriteDataRequest{} },
		append(seed(4), 1, 2, 3, 4), seed(0xFFFF), seed(0xFFFFFFFF))
}

func FuzzException(f *testing.F) {
	fuzzDecode(f, decodeAlloc, func() sip.PDU { return &sip.Exception{} }, le(uint16(1), uint32(0x1001)))
}
