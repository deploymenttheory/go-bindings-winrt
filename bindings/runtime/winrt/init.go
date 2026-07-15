//go:build windows && (amd64 || arm64)

package winrt

import (
	"errors"
	"sync"

	win32 "github.com/deploymenttheory/go-bindings-win32/bindings/runtime/win32"
	syswinrt "github.com/deploymenttheory/go-bindings-win32/bindings/win32/system/winrt"
)

// rpcEChangedMode is RPC_E_CHANGED_MODE: RoInitialize was called on a thread
// already initialized in a different apartment model. The runtime is usable
// on such a thread, so Initialize treats it as success.
const rpcEChangedMode = 0x80010106

var (
	initOnce sync.Once
	initErr  error
)

// Initialize initializes the Windows Runtime for the process (multithreaded
// apartment), once. Every activation path calls it implicitly; calling it
// directly is only needed to surface initialization errors early.
func Initialize() error {
	initOnce.Do(func() {
		err := syswinrt.RoInitialize(syswinrt.RO_INIT_MULTITHREADED)
		var hr win32.HRESULT
		if errors.As(err, &hr) && uint32(hr) == rpcEChangedMode {
			err = nil
		}
		initErr = err
	})
	return initErr
}

// Uninitialize closes the Windows Runtime on the current thread. Rarely
// needed: the process-wide Initialize guard never calls it, and the runtime
// is torn down at process exit.
func Uninitialize() {
	syswinrt.RoUninitialize()
}
