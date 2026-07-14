//go:build windows && (amd64 || arm64)

package winrt

import (
	"fmt"
	"strconv"
	"strings"

	win32 "github.com/deploymenttheory/go-bindings-win32/bindings/runtime/win32"
)

// ParseGUID parses a canonical GUID string ("xxxxxxxx-xxxx-xxxx-xxxx-
// xxxxxxxxxxxx", case-insensitive, optionally brace-wrapped) into a
// win32.GUID. Generated code embeds GUIDs as struct literals; this is the
// convenience for hand-written IIDs.
func ParseGUID(s string) (win32.GUID, error) {
	trimmed := s
	if strings.HasPrefix(trimmed, "{") && strings.HasSuffix(trimmed, "}") {
		trimmed = trimmed[1 : len(trimmed)-1]
	}
	if len(trimmed) != 36 || trimmed[8] != '-' || trimmed[13] != '-' || trimmed[18] != '-' || trimmed[23] != '-' {
		return win32.GUID{}, fmt.Errorf("winrt: malformed GUID %q", s)
	}
	parse := func(hex string, bits int) (uint64, error) {
		v, err := strconv.ParseUint(hex, 16, bits)
		if err != nil {
			return 0, fmt.Errorf("winrt: malformed GUID %q: %w", s, err)
		}
		return v, nil
	}
	data1, err := parse(trimmed[0:8], 32)
	if err != nil {
		return win32.GUID{}, err
	}
	data2, err := parse(trimmed[9:13], 16)
	if err != nil {
		return win32.GUID{}, err
	}
	data3, err := parse(trimmed[14:18], 16)
	if err != nil {
		return win32.GUID{}, err
	}
	var guid win32.GUID
	guid.Data1 = uint32(data1)
	guid.Data2 = uint16(data2)
	guid.Data3 = uint16(data3)
	bytesHex := trimmed[19:23] + trimmed[24:]
	for i := 0; i < 8; i++ {
		b, err := parse(bytesHex[i*2:i*2+2], 8)
		if err != nil {
			return win32.GUID{}, err
		}
		guid.Data4[i] = byte(b)
	}
	return guid, nil
}

// MustGUID is ParseGUID that panics on error, for package-level IID vars.
func MustGUID(s string) win32.GUID {
	guid, err := ParseGUID(s)
	if err != nil {
		panic(err)
	}
	return guid
}
