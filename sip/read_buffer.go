package sip

import (
	"bytes"
	"io"
)

func readBuffer(reader io.Reader, length uint32) ([]byte, error) {
	buf := make([]byte, min(length, 4096))

	lr := io.LimitedReader{R: reader, N: int64(length)}
	var b bytes.Buffer
	for {
		n, err := lr.Read(buf)
		if n > 0 {
			b.Write(buf[:n])
		}
		if err != nil {
			if err == io.EOF {
				break
			}
			return nil, err
		}
		if lr.N == 0 {
			break
		}
	}
	return b.Bytes(), nil
}
