//go:build windows && amd64

package winrt

import (
	"fmt"
	"sync"
	"sync/atomic"
	"syscall"
	"unsafe"

	win32 "github.com/deploymenttheory/go-bindings-win32/bindings/runtime/win32"
	systemthreading "github.com/deploymenttheory/go-bindings-win32/bindings/win32/system/threading"
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
//
// Stack-growth discipline (see inspectable.go for the full story): a native
// call such as add_Closed(&token) holds raw pointers into its Go caller's
// stack while the runtime reenters Go here (QI/AddRef on the handler being
// registered). Growing that goroutine's stack would move the caller's
// frames and strand the native side's pointers, so QI/AddRef/Release stage
// their tiny bodies on the shared inspectable worker. Invoke runs arbitrary
// user handler code — which may itself call back into WinRT and must not
// hold the worker — so it runs on a fresh goroutine instead, keeping the
// callback goroutine's frames shallow either way.
var (
	callbackQI = syscall.NewCallback(func(this *Delegate, riid *win32.GUID, ppv *uintptr) uintptr {
		return dispatchInspectable(inspectableWork{
			fn: func(w *inspectableWork) uintptr {
				return delegateQI((*Delegate)(w.p0), (*win32.GUID)(w.p1), (*uintptr)(w.p2))
			},
			p0: unsafe.Pointer(this), p1: unsafe.Pointer(riid), p2: unsafe.Pointer(ppv),
		})
	})
	callbackAddRef = syscall.NewCallback(func(this *Delegate) uintptr {
		return dispatchInspectable(inspectableWork{
			fn: func(w *inspectableWork) uintptr { return delegateAddRef((*Delegate)(w.p0)) },
			p0: unsafe.Pointer(this),
		})
	})
	callbackRelease = syscall.NewCallback(func(this *Delegate) uintptr {
		return dispatchInspectable(inspectableWork{
			fn: func(w *inspectableWork) uintptr { return delegateRelease((*Delegate)(w.p0)) },
			p0: unsafe.Pointer(this),
		})
	})
	callbackInvoke0 = syscall.NewCallback(func(this *Delegate) uintptr {
		return dispatchInvoke(this)
	})
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

// One immutable vtable per Invoke arity, shared across instances. Arity 0 is
// not a degenerate case to skip: DispatcherQueueHandler — the delegate every
// TryEnqueue takes, and so the only way to marshal work onto a UI thread —
// has a parameterless Invoke.
var delegateVtbls = [4][4]uintptr{
	0: {callbackQI, callbackAddRef, callbackRelease, callbackInvoke0},
	1: {callbackQI, callbackAddRef, callbackRelease, callbackInvoke1},
	2: {callbackQI, callbackAddRef, callbackRelease, callbackInvoke2},
	3: {callbackQI, callbackAddRef, callbackRelease, callbackInvoke3},
}

// NewDelegate creates a delegate for the given IID whose Invoke receives
// paramCount raw ABI words (the delegate's logical parameters, after the
// implicit this; 0–3 supported). invoke returns an HRESULT — return 0 for
// success. The delegate starts with one reference owned by the caller;
// Release it when no native code can still hold it.
func NewDelegate(iid win32.GUID, paramCount int, invoke func(args []uintptr) uintptr) (*Delegate, error) {
	if paramCount < 0 || paramCount > 3 {
		return nil, fmt.Errorf("winrt: delegate with %d params unsupported (0-3)", paramCount)
	}
	// The delegate's QI/AddRef/Release trampolines stage onto the shared
	// worker (see inspectable.go); make sure it — and the deadlock-detector
	// keepalive — is running.
	inspectableWorkerOnce.Do(startInspectableWorker)
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

// inlineInvokeTID, when non-zero, names the OS thread on which Invoke bodies
// run on the invoking thread instead of a fresh goroutine. Zero — the
// default — means every handler takes the goroutine hop.
var inlineInvokeTID atomic.Uint32

// SetInlineThread declares an OS thread whose delegate Invoke bodies must run
// synchronously, on the thread the framework invoked them from, rather than
// being handed to a fresh goroutine. Pass 0 to clear.
//
// Apartment-affine frameworks require this. XAML holds its state in
// thread-local storage keyed to the UI thread, and generated bindings dispatch
// straight through vtable slots with no COM proxy in between, so a handler
// that touches a XAML object from any other thread is an unmarshalled
// cross-apartment call — RPC_E_WRONG_THREAD at best. Parking on a goroutine
// loses the very affinity the framework demands.
//
// The caller is responsible for runtime.LockOSThread on the declared thread:
// the goroutine must stay put for its id to keep meaning what it said. This
// is the same shape dispatchInspectable already uses for worker-thread
// reentry, and it is safe for the same reason plus one more — the stack-growth
// hazard that motivated the goroutine hop is closed on the caller side, where
// every native-written out-param is heap-escaped through OutParam (see
// outparam.go), so a body growing this thread's stack strands nothing.
func SetInlineThread(tid uint32) { inlineInvokeTID.Store(tid) }

// CurrentThreadID is the calling goroutine's current OS thread id, for
// pairing with SetInlineThread.
func CurrentThreadID() uint32 { return systemthreading.GetCurrentThreadId() }

// dispatchInvoke runs the user handler on a fresh goroutine and parks the
// callback goroutine until it finishes: the handler's stack can grow (and
// nest further WinRT calls) without ever moving the frames a surrounding
// native call may still point into.
//
// On a thread declared by SetInlineThread the body runs INLINE instead,
// because thread identity is the thing the caller is protecting.
func dispatchInvoke(this *Delegate, args ...uintptr) uintptr {
	if tid := inlineInvokeTID.Load(); tid != 0 && tid == systemthreading.GetCurrentThreadId() {
		if !registered(this) {
			return eFail // invoked after release
		}
		return invokeContained(this, args)
	}
	done := make(chan uintptr, 1)
	go func() {
		if !registered(this) {
			done <- eFail // invoked after release
			return
		}
		done <- invokeContained(this, args)
	}()
	return <-done
}

// invokeContained runs the delegate body with a panic barrier. See panic.go.
//
// The barrier is needed on BOTH paths above, and for different reasons. On the inline
// path the native frames are directly beneath, so an escaping panic terminates the
// process. On the goroutine path the panic is on a goroutine nobody recovers, which
// also terminates the process — and there it additionally leaves dispatchInvoke waiting
// on a channel that will never be written.
func invokeContained(this *Delegate, args []uintptr) (result uintptr) {
	defer containPanic("a delegate body (Invoke)", &result)
	return this.invoke(args)
}
