//go:build windows && (amd64 || arm64)

package winrt

import (
	"fmt"

	win32 "github.com/deploymenttheory/go-bindings-win32/bindings/runtime/win32"
)

// Windows.Foundation.AsyncStatus values as they reach the runtime layer;
// kept as raw int32 so this package stays independent of the generated enum
// types (every generated Await passes its status through int32).
const (
	asyncStarted   int32 = 0
	asyncCompleted int32 = 1
	asyncCanceled  int32 = 2
	asyncError     int32 = 3
)

// asyncStatusName names an AsyncStatus value for error text.
func asyncStatusName(status int32) string {
	switch status {
	case asyncStarted:
		return "Started"
	case asyncCompleted:
		return "Completed"
	case asyncCanceled:
		return "Canceled"
	case asyncError:
		return "Error"
	}
	return fmt.Sprintf("AsyncStatus(%d)", status)
}

// AsyncError is the terminal-failure error a generated Await returns when a
// WinRT async operation ends in a status other than Completed. It names the
// status and wraps the operation's IAsyncInfo error code as a win32.HRESULT,
// so errors.Is/errors.As reach the underlying HRESULT.
func AsyncError(status int32, hresult int32) error {
	return fmt.Errorf("winrt: async operation ended with status %s: %w",
		asyncStatusName(status), win32.HRESULT(hresult))
}
