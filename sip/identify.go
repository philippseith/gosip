package sip

import (
	"context"
	"encoding/binary"
	"io"
	"time"

	"github.com/joomcode/errorx"
)

// Identify broadcasts an IdentifyRequest on the specified network interface
// and listens for IdentifyResponses from devices. Listening ends when ctx is canceled.
//
// Example:
//
//	ctx, cancel := context.WithTimeout(context.Background(), time.Second*3)
//	defer cancel()
//	resCh, err := sip.Identify(ctx, "en0", [6]byte{1,2,3,4,5,6})
//	if err != nil { log.Fatal(err) }
//	for res := range resCh { fmt.Println(res) }
func Identify(ctx context.Context, interfaceName string, nodeIdentifier [6]byte) (chan Result[*IdentifyResponse], error) {
	return Broadcast[*IdentifyResponse](ctx, interfaceName, &IdentifyRequest{
		NodeIdentifier: nodeIdentifier,
	}, time.Second)
}

type IdentifyRequest struct {
	NodeIdentifier [6]byte
}

func (i *IdentifyRequest) Read(reader io.Reader) error {
	if err := binary.Read(reader, binary.LittleEndian, i); err != nil {
		return errorx.EnsureStackTrace(err)
	}
	return nil
}

func (i *IdentifyRequest) Write(writer io.Writer) error {
	if err := binary.Write(writer, binary.LittleEndian, i.NodeIdentifier); err != nil {
		return errorx.EnsureStackTrace(err)
	}
	return nil
}

func (i *IdentifyRequest) MessageType() MessageType {
	return IdentifyRequestMsgType
}

type IdentifyResponse struct {
}

func (i *IdentifyResponse) Read(io.Reader) error {
	return nil
}

func (i *IdentifyResponse) Write(io.Writer) error {
	return nil
}

func (i *IdentifyResponse) MessageType() MessageType {
	return IdentifyResponseMsgType
}
