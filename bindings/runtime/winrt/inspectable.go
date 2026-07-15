//go:build windows && (amd64 || arm64)

package winrt

import (
	"runtime"
	"slices"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"

	win32 "github.com/deploymenttheory/go-bindings-win32/bindings/runtime/win32"
	systemcom "github.com/deploymenttheory/go-bindings-win32/bindings/win32/system/com"
	systemthreading "github.com/deploymenttheory/go-bindings-win32/bindings/win32/system/threading"
	syswinrt "github.com/deploymenttheory/go-bindings-win32/bindings/win32/system/winrt"
)

// inspectable is the shared core of every Go-implemented WinRT INSPECTABLE
// object (delegates keep their own 4-slot path in delegate.go). It is one
// FACET of a COM object: a vtable pointer plus the bookkeeping the six
// IInspectable slots need. Concrete types embed one facet as their FIRST
// field (so the native `this` word doubles as the Go object pointer) and,
// when they implement a second interface with different method slots, a
// further facet as a tear-off: a second vtable-bearing word inside the same
// allocation, sharing the identity facet's reference count and answering
// QueryInterface(IUnknown/IInspectable) with the identity pointer, which is
// what keeps COM object identity intact.
type inspectable struct {
	// lpVtbl must be the first word: native code dereferences the facet
	// pointer to find the vtable (per-TYPE static arrays; see collections.go).
	lpVtbl *uintptr
	// identity is the facet whose pointer is the COM identity (the first
	// facet passed to initInspectable; self for single-facet objects). The
	// reference count, class name, and facet list live on it.
	identity *inspectable
	// facets (identity only) lists every registered facet, identity first.
	facets []*inspectable
	// refs (identity only) is the object-wide reference count.
	refs atomic.Int32
	// iids are the interface IDs QueryInterface answers with THIS facet's
	// pointer; GetIids reports the union across facets.
	iids []win32.GUID
	// class (identity only) is the GetRuntimeClassName answer.
	class string
	// destroy (identity only, optional) runs exactly once when the reference
	// count reaches zero, after the facets are unregistered. Collections use
	// it to release the element references they retain; it executes on the
	// worker (or inline on the worker thread — see dispatchInspectable) and
	// may itself reenter Go-implemented objects (an element's Release).
	destroy func()
}

const (
	eBounds      = uintptr(0x8000000B) // E_BOUNDS
	eOutOfMemory = uintptr(0x8007000E) // E_OUTOFMEMORY
)

// The registry pins live objects and validates the native `this` word before
// it is trusted (a released object's facets are no longer members). Every
// facet is registered under its own address.
var (
	inspectableMu       sync.Mutex
	inspectableRegistry = map[uintptr]*inspectable{}
)

// Method bodies run on a dedicated worker goroutine, NOT on the goroutine
// the native callback reenters. A native call like the Calendar factory's
// CreateCalendar holds a raw pointer into its Go caller's STACK (the
// `&result` out-param the generated binding passed to SyscallN); if a
// nested callback grows that goroutine's stack, the stack is copied and the
// native side's pointer goes stale — the callee then writes its result into
// freed stack memory (observed live: success HRESULT with a lost out-param,
// ~15% of cold-start Calendar factory calls; 100% with a forced growth).
// The trampolines below therefore only stage arguments in
// pendingInspectableWork and park — allocation-free, small bounded stack —
// while the worker, whose stack may grow harmlessly, executes the body.
// inspectableWorkMu doubles as process-wide serialization of Go-implemented
// inspectable calls.
//
// Since the generic-collection wave, bodies MAY call back into native
// objects: an element codec AddRefs/Releases interface elements
// (collections_core.go). When such an element is itself Go-implemented (a
// delegate, another collection), that nested call reenters Go ON THE WORKER
// THREAD — parking it on the worker would self-deadlock, so
// dispatchInspectable detects that case (the worker is thread-locked and
// publishes its OS thread id) and runs the nested body INLINE instead. The
// inline body's frames sit on the worker's stack, whose growth is harmless
// as long as no in-flight native frame beneath holds a pointer INTO that
// stack — which is why worker-side bodies must never pass a stack address
// as an out-pointer to a native call that can reenter Go (the codec calls
// are AddRef/Release only: no out-pointers).
//
// The worker eliminates GROWTH-driven stack moves, but not SHRINK-driven
// ones: while the callback goroutine is parked here waiting on the worker,
// it has LEFT syscall state (cgocallback ran exitsyscall on reentry), so it
// is an ordinary blocked goroutine and a concurrent GC may shrink — i.e.
// move — its stack. The frames of the original Go caller sit on that same
// stack, so any out-param address they passed to SyscallN would go stale
// exactly as under a growth. The invariant that closes this hole lives on
// the CALLER side: generated bindings and the hand-written runtime paths
// heap-escape every native-written out-param (retvals, [out] parameters,
// event tokens) via winrt.OutParam / heapNew — see outparam.go — so the
// pointer the native side holds targets the non-moving heap and no stack
// move, growth or shrink, can strand it.
type inspectableWork struct {
	fn         func(*inspectableWork) uintptr
	this       *inspectable
	p0, p1, p2 unsafe.Pointer
	u0, u1     uintptr
	result     uintptr
}

var (
	inspectableWorkMu      sync.Mutex
	pendingInspectableWork inspectableWork // guarded by inspectableWorkMu
	inspectableWorkReady   = make(chan struct{})
	inspectableWorkDone    = make(chan struct{})
	inspectableWorkerOnce  sync.Once
	// inspectableWorkerTID is the worker's OS thread id (0 until the worker
	// has locked its thread and published it). dispatchInspectable compares
	// it against the calling thread to detect worker-side reentrancy.
	inspectableWorkerTID atomic.Uint32
)

func inspectableWorker() {
	// Pin the worker to one OS thread and publish its id: a callback that
	// arrives ON this thread can only be a nested reentry from a body the
	// worker is already running (a codec AddRef/Release on a Go-implemented
	// element), and dispatchInspectable must run it inline rather than park.
	// LockOSThread makes the id stable for the goroutine's lifetime.
	runtime.LockOSThread()
	inspectableWorkerTID.Store(systemthreading.GetCurrentThreadId())
	for range inspectableWorkReady {
		pendingInspectableWork.result = pendingInspectableWork.fn(&pendingInspectableWork)
		inspectableWorkDone <- struct{}{}
	}
}

// startInspectableWorker launches the worker plus a keepalive goroutine.
//
// The keepalive defuses a runtime false positive: a program whose only
// pending wake-up is a WinRT callback (e.g. blocked in a generated Await
// with no other runnable goroutines or timers) trips Go's deadlock detector
// — checkdead cannot see native threadpool threads, so it declares
// "all goroutines are asleep" and kills the process. A goroutine parked in
// time.Sleep keeps a runtime timer registered at all times, which is
// exactly the signal checkdead accepts as "someone can still be woken".
// Cost: one goroutine waking hourly.
func startInspectableWorker() {
	go inspectableWorker()
	go func() {
		for {
			time.Sleep(time.Hour)
		}
	}()
}

// dispatchInspectable hands one staged call to the worker and waits for its
// result. Runs on the callback goroutine: no allocations, minimal frames.
//
// When the call arrives ON the worker's own thread it is a nested reentry —
// a body the worker is executing called a native AddRef/Release that landed
// back in a Go-implemented object's trampoline. Parking would self-deadlock
// (the worker cannot pick up new work while it waits for itself), so the
// body runs INLINE on the worker thread. That is safe: the outer body and
// the nested one run sequentially on one thread (the serialization the
// worker exists to provide still holds), pendingInspectableWork is not
// touched, and stack growth here moves only the WORKER's stack — harmless
// because worker-side bodies never hand native code a pointer into it (see
// the invariant above).
func dispatchInspectable(work inspectableWork) uintptr {
	if tid := inspectableWorkerTID.Load(); tid != 0 && tid == systemthreading.GetCurrentThreadId() {
		return work.fn(&work)
	}
	inspectableWorkMu.Lock()
	pendingInspectableWork = work
	inspectableWorkReady <- struct{}{}
	<-inspectableWorkDone
	result := pendingInspectableWork.result
	pendingInspectableWork = inspectableWork{} // drop staged pointers
	inspectableWorkMu.Unlock()
	return result
}

// Shared IInspectable trampolines: syscall.NewCallback allocations are
// process-permanent, so one per slot serves every object of every type (the
// facet is recovered from `this`). Concrete types append their own method
// trampolines after these six (see collections.go).
var (
	inspectableCallbackQI                  = syscall.NewCallback(inspectableQI)
	inspectableCallbackAddRef              = syscall.NewCallback(inspectableAddRef)
	inspectableCallbackRelease             = syscall.NewCallback(inspectableRelease)
	inspectableCallbackGetIids             = syscall.NewCallback(inspectableGetIids)
	inspectableCallbackGetRuntimeClassName = syscall.NewCallback(inspectableGetRuntimeClassName)
	inspectableCallbackGetTrustLevel       = syscall.NewCallback(inspectableGetTrustLevel)
)

// initInspectable wires an object's facets and registers them. The first
// facet is the COM identity; every facet must already carry its lpVtbl and
// iids. The object starts with one reference owned by the Go caller (which
// a constructor may hand to native code, e.g. through an out-param).
func initInspectable(class string, facets ...*inspectable) {
	inspectableWorkerOnce.Do(startInspectableWorker)
	identity := facets[0]
	identity.facets = facets
	identity.class = class
	identity.refs.Store(1)
	inspectableMu.Lock()
	for _, facet := range facets {
		facet.identity = identity
		inspectableRegistry[uintptr(unsafe.Pointer(facet))] = facet
	}
	inspectableMu.Unlock()
}

// registeredInspectable reports whether this is a live, registered facet.
func registeredInspectable(this *inspectable) bool {
	inspectableMu.Lock()
	defer inspectableMu.Unlock()
	return this != nil && inspectableRegistry[uintptr(unsafe.Pointer(this))] == this
}

func inspectableQI(this *inspectable, riid *win32.GUID, ppv *uintptr) uintptr {
	return dispatchInspectable(inspectableWork{
		fn: inspectableQIBody, this: this,
		p0: unsafe.Pointer(riid), p1: unsafe.Pointer(ppv),
	})
}

func inspectableQIBody(w *inspectableWork) uintptr {
	this, riid, ppv := w.this, (*win32.GUID)(w.p0), (*uintptr)(w.p1)
	if ppv == nil {
		return ePointer
	}
	if !registeredInspectable(this) || riid == nil {
		*ppv = 0
		return eNoInterface
	}
	identity := this.identity
	// Identity interfaces always resolve to the identity facet, from ANY
	// facet — the COM identity rule tear-offs must preserve.
	if *riid == iidUnknown || *riid == syswinrt.IID_IInspectable || *riid == iidAgileObject {
		identity.refs.Add(1)
		*ppv = uintptr(unsafe.Pointer(identity))
		return 0
	}
	for _, facet := range identity.facets {
		if slices.Contains(facet.iids, *riid) {
			identity.refs.Add(1)
			*ppv = uintptr(unsafe.Pointer(facet))
			return 0
		}
	}
	*ppv = 0
	return eNoInterface
}

func inspectableAddRef(this *inspectable) uintptr {
	return dispatchInspectable(inspectableWork{fn: inspectableAddRefBody, this: this})
}

func inspectableAddRefBody(w *inspectableWork) uintptr {
	this := w.this
	if !registeredInspectable(this) {
		return 0
	}
	return uintptr(this.identity.refs.Add(1))
}

func inspectableRelease(this *inspectable) uintptr {
	return dispatchInspectable(inspectableWork{fn: inspectableReleaseBody, this: this})
}

func inspectableReleaseBody(w *inspectableWork) uintptr {
	this := w.this
	if !registeredInspectable(this) {
		return 0
	}
	identity := this.identity
	remaining := identity.refs.Add(-1)
	if remaining == 0 {
		inspectableMu.Lock()
		for _, facet := range identity.facets {
			delete(inspectableRegistry, uintptr(unsafe.Pointer(facet)))
		}
		inspectableMu.Unlock()
		// Run the destructor after unregistration (a reentrant call into the
		// dead object now fails cleanly) and outside inspectableMu (releasing
		// a retained element may reenter registeredInspectable).
		if destroy := identity.destroy; destroy != nil {
			identity.destroy = nil
			destroy()
		}
	}
	return uintptr(remaining)
}

func inspectableGetIids(this *inspectable, iidCount *uint32, iids **win32.GUID) uintptr {
	return dispatchInspectable(inspectableWork{
		fn: inspectableGetIidsBody, this: this,
		p0: unsafe.Pointer(iidCount), p1: unsafe.Pointer(iids),
	})
}

// inspectableGetIidsBody reports the union of every facet's IIDs (identity
// interfaces excluded, per the IInspectable contract). The array is
// CoTaskMemAlloc'd — the WinRT contract is callee-allocates, caller frees.
func inspectableGetIidsBody(w *inspectableWork) uintptr {
	this, iidCount, iids := w.this, (*uint32)(w.p0), (**win32.GUID)(w.p1)
	if iidCount == nil || iids == nil {
		return ePointer
	}
	*iidCount = 0
	*iids = nil
	if !registeredInspectable(this) {
		return eFail
	}
	var all []win32.GUID
	for _, facet := range this.identity.facets {
		all = append(all, facet.iids...)
	}
	if len(all) == 0 {
		return 0
	}
	mem := systemcom.CoTaskMemAlloc(uintptr(len(all)) * unsafe.Sizeof(win32.GUID{}))
	if mem == nil {
		return eOutOfMemory
	}
	copy(unsafe.Slice((*win32.GUID)(mem), len(all)), all)
	*iidCount = uint32(len(all))
	*iids = (*win32.GUID)(mem)
	return 0
}

func inspectableGetRuntimeClassName(this *inspectable, className *syswinrt.HSTRING) uintptr {
	return dispatchInspectable(inspectableWork{
		fn: inspectableGetRuntimeClassNameBody, this: this, p0: unsafe.Pointer(className),
	})
}

func inspectableGetRuntimeClassNameBody(w *inspectableWork) uintptr {
	this, className := w.this, (*syswinrt.HSTRING)(w.p0)
	if className == nil {
		return ePointer
	}
	*className = 0
	if !registeredInspectable(this) {
		return eFail
	}
	h, err := NewHString(this.identity.class)
	if err != nil {
		return eFail
	}
	*className = h.Raw() // ownership passes to the caller; do not Close
	return 0
}

func inspectableGetTrustLevel(this *inspectable, trustLevel *syswinrt.TrustLevel) uintptr {
	return dispatchInspectable(inspectableWork{
		fn: inspectableGetTrustLevelBody, this: this, p0: unsafe.Pointer(trustLevel),
	})
}

func inspectableGetTrustLevelBody(w *inspectableWork) uintptr {
	this, trustLevel := w.this, (*syswinrt.TrustLevel)(w.p0)
	if trustLevel == nil {
		return ePointer
	}
	if !registeredInspectable(this) {
		return eFail
	}
	*trustLevel = syswinrt.BaseTrust
	return 0
}
