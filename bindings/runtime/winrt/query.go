//go:build windows && (amd64 || arm64)

package winrt

import (
	"fmt"
	"unsafe"

	win32 "github.com/deploymenttheory/go-bindings-win32/bindings/runtime/win32"
)

// QueryInterface queries a COM object for the interface identified by iid
// and returns it as *T. T must be a generated interface struct — a single
// vtable-pointer word layout-compatible with win32.IUnknown (see the
// package doc) — which is what makes the unsafe casts here sound. The
// returned reference is owned by the caller.
func QueryInterface[T any](obj unsafe.Pointer, iid *win32.GUID) (*T, error) {
	if obj == nil {
		return nil, fmt.Errorf("winrt: QueryInterface on nil object")
	}
	unknown := (*win32.IUnknown)(obj)
	var out *win32.IUnknown
	if err := unknown.QueryInterface(iid, &out); err != nil {
		return nil, err
	}
	if out == nil {
		return nil, fmt.Errorf("winrt: QueryInterface returned null for %s", iid)
	}
	return (*T)(unsafe.Pointer(out)), nil
}
