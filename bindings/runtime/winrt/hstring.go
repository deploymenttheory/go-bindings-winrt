//go:build windows && (amd64 || arm64)

package winrt

import (
	"fmt"
	"strings"
	"unicode/utf16"
	"unsafe"

	syswinrt "github.com/deploymenttheory/go-bindings-win32/bindings/win32/system/winrt"
)

// HString owns an HSTRING created from a Go string, for passing into WinRT
// calls. Close it when the call returns (a null handle is the canonical
// empty string, so the zero HString is valid and empty).
type HString struct {
	raw syswinrt.HSTRING
}

// NewHString creates an HSTRING from a Go string. An empty string yields the
// null HSTRING. Strings containing NUL are rejected (the underlying
// WindowsCreateString binding marshals through a NUL-terminated buffer).
func NewHString(s string) (HString, error) {
	if s == "" {
		return HString{}, nil
	}
	if strings.IndexByte(s, 0) >= 0 {
		return HString{}, fmt.Errorf("winrt: string contains NUL byte")
	}
	length := uint32(len(utf16.Encode([]rune(s))))
	var h syswinrt.HSTRING
	if err := syswinrt.WindowsCreateString(s, length, &h); err != nil {
		return HString{}, fmt.Errorf("winrt: creating HSTRING: %w", err)
	}
	return HString{raw: h}, nil
}

// Raw returns the underlying HSTRING for passing to WinRT calls.
func (h HString) Raw() syswinrt.HSTRING { return h.raw }

// Close releases the HSTRING. Idempotent; the zero/null handle is a no-op.
func (h *HString) Close() error {
	if h.raw == 0 {
		return nil
	}
	err := syswinrt.WindowsDeleteString(h.raw)
	h.raw = 0
	return err
}

// String returns the Go string value.
func (h HString) String() string { return HStringToString(h.raw) }

// HStringToString reads an HSTRING into a Go string without releasing it.
// The read honors the runtime's length (embedded NULs are legal in HSTRINGs;
// never NUL-scan); the null handle yields "".
func HStringToString(h syswinrt.HSTRING) string {
	if h == 0 {
		return ""
	}
	var length uint32
	buffer := syswinrt.WindowsGetStringRawBuffer(h, &length)
	if buffer == nil || length == 0 {
		return ""
	}
	return string(utf16.Decode(unsafe.Slice((*uint16)(buffer), length)))
}

// TakeHString reads an HSTRING into a Go string and releases it — the
// consumption helper for every [out, retval] HSTRING.
func TakeHString(h syswinrt.HSTRING) string {
	s := HStringToString(h)
	if h != 0 {
		_ = syswinrt.WindowsDeleteString(h) // documented to always succeed
	}
	return s
}
