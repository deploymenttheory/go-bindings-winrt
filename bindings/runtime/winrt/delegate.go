//go:build windows && (amd64 || arm64)

package winrt

import (
	"fmt"
	"sync"
	"sync/atomic"
	"syscall"
	"unsafe"

	win32 "github.com/deploymenttheory/go-bindings-win32/bindings/runtime/win32"
)

// Delegate is a Go-implemented WinRT delegate: a minimal COM object whose
// vtable is {QueryInterface, AddRef, Release, Invoke}. Passing its Ptr to a
// WinRT add_ method lets native code call back into Go.
//
// QueryInterface answers the delegate's own IID, IUnknown, and IAgileObject
// (delegates are agile: the runtime may invoke them from any apartment).
// Instances are pinned in a package registry from NewDelegate until their
// reference count reaches zero, so they stay reachable while native code
// holds them.
type Delegate struct {
	// lpVtbl must be the first word: native code dereferences the object
	// pointer to find the vtable.
	lpVtbl *[4]uintptr
	iid    win32.GUID
	refs   atomic.Int32
	invoke func(args []uintptr) uintptr
}

var (
	iidUnknown     = win32.GUID{Data4: [8]byte{0xc0, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x46}}
	iidAgileObject = win32.GUID{Data1: 0x94ea2b94, Data2: 0xe9cc, Data3: 0x49e0, Data4: [8]byte{0xc0, 0xff, 0xee, 0x64, 0xca, 0x8f, 0x5b, 0x90}}
)

const (
	eNoInterface = uintptr(0x80004002) // E_NOINTERFACE
	ePointer     = uintptr(0x80004003) // E_POINTER
	eFail        = uintptr(0x80004005) // E_FAIL
)

// The registry pins live delegates and validates the native `this` word
// before it is trusted (a released delegate is no longer a member).
var (
	delegateMu       sync.Mutex
	delegateRegistry = map[uintptr]*Delegate{}
)

// Shared callback trampolines: syscall.NewCallback allocations are
// process-permanent, so one per vtable slot shape serves every delegate
// instance (the instance is recovered from `this`). NewCallback passes
// uintptr-sized register words through typed pointer parameters directly.
var (
	callbackQI      = syscall.NewCallback(delegateQI)
	callbackAddRef  = syscall.NewCallback(delegateAddRef)
	callbackRelease = syscall.NewCallback(delegateRelease)
	callbackInvoke1 = syscall.NewCallback(func(this *Delegate, a uintptr) uintptr {
		return dispatchInvoke(this, a)
	})
	callbackInvoke2 = syscall.NewCallback(func(this *Delegate, a, b uintptr) uintptr {
		return dispatchInvoke(this, a, b)
	})
	callbackInvoke3 = syscall.NewCallback(func(this *Delegate, a, b, c uintptr) uintptr {
		return dispatchInvoke(this, a, b, c)
	})
)

// One immutable vtable per Invoke arity, shared across instances.
var delegateVtbls = [4][4]uintptr{
	1: {callbackQI, callbackAddRef, callbackRelease, callbackInvoke1},
	2: {callbackQI, callbackAddRef, callbackRelease, callbackInvoke2},
	3: {callbackQI, callbackAddRef, callbackRelease, callbackInvoke3},
}

// NewDelegate creates a delegate for the given IID whose Invoke receives
// paramCount raw ABI words (the delegate's logical parameters, after the
// implicit this; 1–3 supported). invoke returns an HRESULT — return 0 for
// success. The delegate starts with one reference owned by the caller;
// Release it when no native code can still hold it.
func NewDelegate(iid win32.GUID, paramCount int, invoke func(args []uintptr) uintptr) (*Delegate, error) {
	if paramCount < 1 || paramCount > 3 {
		return nil, fmt.Errorf("winrt: delegate with %d params unsupported (1-3)", paramCount)
	}
	d := &Delegate{lpVtbl: &delegateVtbls[paramCount], iid: iid, invoke: invoke}
	d.refs.Store(1)
	delegateMu.Lock()
	delegateRegistry[uintptr(unsafe.Pointer(d))] = d
	delegateMu.Unlock()
	return d, nil
}

// Ptr is the COM object pointer to pass to add_ methods.
func (d *Delegate) Ptr() uintptr { return uintptr(unsafe.Pointer(d)) }

// Release drops the caller's reference (the native side holds its own).
func (d *Delegate) Release() uint32 {
	return uint32(delegateRelease(d))
}

// registered reports whether this is a live, registered delegate.
func registered(this *Delegate) bool {
	delegateMu.Lock()
	defer delegateMu.Unlock()
	return this != nil && delegateRegistry[uintptr(unsafe.Pointer(this))] == this
}

func delegateQI(this *Delegate, riid *win32.GUID, ppv *uintptr) uintptr {
	if ppv == nil {
		return ePointer
	}
	if !registered(this) || riid == nil {
		*ppv = 0
		return eNoInterface
	}
	if *riid == this.iid || *riid == iidUnknown || *riid == iidAgileObject {
		this.refs.Add(1)
		*ppv = uintptr(unsafe.Pointer(this))
		return 0
	}
	*ppv = 0
	return eNoInterface
}

func delegateAddRef(this *Delegate) uintptr {
	if !registered(this) {
		return 0
	}
	return uintptr(this.refs.Add(1))
}

func delegateRelease(this *Delegate) uintptr {
	if !registered(this) {
		return 0
	}
	remaining := this.refs.Add(-1)
	if remaining == 0 {
		delegateMu.Lock()
		delete(delegateRegistry, uintptr(unsafe.Pointer(this)))
		delegateMu.Unlock()
	}
	return uintptr(remaining)
}

func dispatchInvoke(this *Delegate, args ...uintptr) uintptr {
	if !registered(this) {
		return eFail // invoked after release
	}
	return this.invoke(args)
}
