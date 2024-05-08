package sip

import "fmt"

type Idn uint32

func (i Idn) String() string {
	idnType := "S"
	if i&0x8000 == 0x8000 {
		idnType = "P"
	}
	return fmt.Sprintf("%s-%1d-%04d.%d.%d", idnType, (uint8)(i>>12)&0x7, (uint16)(i&0x0fff), (uint8)(i>>24), (uint8)(i>>16))
}
