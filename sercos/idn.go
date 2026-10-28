package sercos

import "fmt"

type Idn uint32

func (i Idn) String() string {
	idnType := "S"
	if i&0x8000 == 0x8000 {
		idnType = "P"
	}
	return fmt.Sprintf("%s-%1d-%04d.%d.%d", idnType, (uint8)((i>>12)&0x7), (uint16)(i&0x0fff), (uint8)((i>>24)&0xff), (uint8)((i>>16)&0xff)) // nolint:gosec
}

type IdnS2 uint16

func (i IdnS2) String() string {
	idnType := "S"
	if i&0x8000 == 0x8000 {
		idnType = "P"
	}
	return fmt.Sprintf("%s-%1d-%04d", idnType, (uint8)((i>>12)&0x7), (uint16)(i&0x0fff)) // nolint:gosec
}