//go:build windows && (amd64 || arm64)

package winrt

import (
	"errors"

	win32 "github.com/deploymenttheory/go-bindings-win32/bindings/runtime/win32"
	systemcom "github.com/deploymenttheory/go-bindings-win32/bindings/win32/system/com"
	syswinrt "github.com/deploymenttheory/go-bindings-win32/bindings/win32/system/winrt"
)

// rpcEChangedMode is RPC_E_CHANGED_MODE: RoInitialize was called on a thread
// already initialized in a different apartment model. The runtime is usable
// on such a thread, so Initialize treats it as success.
const rpcEChangedMode = 0x80010106

// Initialize initializes the Windows Runtime on the CALLING THREAD, in the
// multithreaded apartment, unless the thread already belongs to one.
//
// Apartment initialization is per-thread, which is why this is not guarded by
// a process-wide sync.Once: a Once consumed by whichever thread activated
// first would leave every other thread uninitialized, and that thread's first
// activation would fail with CO_E_NOTINITIALIZED. Every activation path calls
// this implicitly; calling it directly is only needed to surface
// initialization errors early.
//
// A thread that already has an apartment keeps it, whatever its model. That
// matters for apartment-affine frameworks: a UI thread that entered a
// single-threaded apartment before activating anything — XAML requires one —
// is left alone rather than fought over.
func Initialize() error {
	if threadHasApartment() {
		return nil
	}
	err := syswinrt.RoInitialize(syswinrt.RO_INIT_MULTITHREADED)
	var hr win32.HRESULT
	if errors.As(err, &hr) && uint32(hr) == rpcEChangedMode {
		err = nil
	}
	return err
}

// threadHasApartment reports whether the calling thread already belongs to an
// apartment.
//
// CoGetApartmentType is the probe because it has no side effect: it answers
// CO_E_NOTINITIALIZED on a thread that has never been initialized and
// succeeds otherwise. RoInitialize cannot serve here — its "already
// initialized" answer is the SUCCESS code S_FALSE, indistinguishable from
// S_OK through a binding that maps every success to a nil error, and it takes
// a reference the caller would then have to balance. Probing first means the
// per-thread initialization count is incremented at most once and never needs
// a matching RoUninitialize.
func threadHasApartment() bool {
	var apartmentType systemcom.APTTYPE
	var qualifier systemcom.APTTYPEQUALIFIER
	return systemcom.CoGetApartmentType(&apartmentType, &qualifier) == nil
}

// Uninitialize closes the Windows Runtime on the current thread. Rarely
// needed: Initialize takes at most one reference per thread and never
// releases it, and the runtime is torn down at process exit.
func Uninitialize() {
	syswinrt.RoUninitialize()
}
