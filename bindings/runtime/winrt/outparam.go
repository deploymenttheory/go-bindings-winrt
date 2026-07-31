//go:build windows && amd64

package winrt

import "unsafe"

// Native out-parameters must never point into a Go stack.
//
// A WinRT call can reenter Go on the SAME goroutine before it returns: the
// callee QIs/AddRefs a Go-implemented argument (a delegate, a collection),
// the callback parks on the dispatch worker (see inspectable.go), and while
// it is parked the goroutine is an ordinary blocked goroutine — cgocallback
// ran exitsyscall, so the runtime no longer treats it as "in a syscall" and
// a concurrent GC may SHRINK (move) its stack. Any raw pointer the native
// side still holds into that stack — the out-param word the binding passed
// to SyscallN — then goes stale, and the callee writes its result into freed
// stack memory. Go's heap does not move, so the invariant (generated and
// hand-written code alike) is: every pointer handed to native code for
// WRITING is heap-allocated.
//
// Plain `new(T)` is not enough: escape analysis keeps a non-escaping new on
// the stack, and syscall.SyscallN is //go:uintptrkeepalive (liveness only),
// not //go:uintptrescapes. The helpers below defeat escape analysis the same
// way runtime.Escape does — a never-taken store to a package-level sink —
// which the inliner preserves and which propagates through call summaries,
// so even a CALLER's `&local` passed as an out-pointer parameter is moved to
// the heap at the caller.

// alwaysFalse is never set; reading it keeps the compiler from proving the
// sink stores below dead, which is what forces the escapes.
var alwaysFalse bool

// outParamSink is written only on never-taken branches.
var outParamSink unsafe.Pointer

// OutParam returns p unchanged while forcing the allocation p points to onto
// the heap. Generated bindings route every native-written pointer — retvals,
// [out] parameters, event registration tokens — through it as
// `uintptr(winrt.OutParam(unsafe.Pointer(result)))`, keeping the
// pointer-to-uintptr conversion in the SyscallN argument list (the sanctioned
// keepalive pattern) while guaranteeing the pointee is heap-allocated and so
// survives any stack move. Inlined: one global load and a never-taken branch.
func OutParam(p unsafe.Pointer) unsafe.Pointer {
	if alwaysFalse {
		outParamSink = p
	}
	return p
}

// heapNew is new(T) guaranteed to allocate on the heap — the hand-written
// runtime paths' form of the out-param invariant above for out-pointers they
// pass to native calls through typed APIs (RoActivateInstance,
// IUnknown.QueryInterface) rather than raw SyscallN words.
func heapNew[T any]() *T {
	p := new(T)
	if alwaysFalse {
		outParamSink = unsafe.Pointer(p)
	}
	return p
}

// ---------------------------------------------------------------------------
// The inbound direction

// InParam converts an address NATIVE code gave Go to write through into a pointer.
//
// This is the mirror of OutParam and the two must not be confused:
//
//	OutParam  Go allocates, native writes.  The hazard is Go's stack MOVING under a
//	          pointer native code still holds, so the pointee is forced onto the heap.
//	InParam   Native allocates, Go writes.  There is no such hazard — the address is
//	          not in any Go stack and the collector neither moves nor tracks it.
//
// It exists because a Go body implementing a WinRT method receives its out-parameters
// as raw uintptrs (winrt.Method takes []uintptr), and writing through one means
// converting uintptr to unsafe.Pointer — which `go vet` flags as "possible misuse of
// unsafe.Pointer" every time. The heuristic is right to be suspicious in general and
// wrong here specifically: the word IS a native address, and there is no Go pointer it
// could have come from.
//
// Routing those conversions through one named function does not silence vet — nothing
// can, short of not converting — but it puts the justification in ONE place that can be
// read, instead of once per call site or, worse, nowhere:
//
//	func getValue(args []uintptr) uintptr {
//		if len(args) < 1 || args[0] == 0 {
//			return ePointer
//		}
//		*(*float64)(winrt.InParam(args[0])) = 42
//		return 0
//	}
//
// The caller must still check the address is non-zero. A WinRT caller passing null for
// an out-parameter is a caller bug, and the documented answer is E_POINTER — not a
// crash, which is what writing through nil would give.
func InParam(address uintptr) unsafe.Pointer {
	return unsafe.Pointer(address) //nolint:govet // see the doc comment: a native address, not a Go pointer
}
