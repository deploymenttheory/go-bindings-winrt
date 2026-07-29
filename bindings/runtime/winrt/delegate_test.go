//go:build windows && (amd64 || arm64)

package winrt

import (
	"runtime"
	"slices"
	"syscall"
	"testing"
	"unsafe"

	win32 "github.com/deploymenttheory/go-bindings-win32/bindings/runtime/win32"
)

// call drives a vtable slot exactly as native WinRT code would.
func call(t *testing.T, d *Delegate, slot int, args ...uintptr) uintptr {
	t.Helper()
	r1, _, _ := syscall.SyscallN(d.lpVtbl[slot], append([]uintptr{d.Ptr()}, args...)...)
	return r1
}

func TestDelegateVtableDispatch(t *testing.T) {
	iid := MustGUID("64e12a45-973b-4a3a-b260-91898a49a82c")
	var got []uintptr
	d, err := NewDelegate(iid, 2, func(args []uintptr) uintptr {
		got = slices.Clone(args)
		return 0
	})
	if err != nil {
		t.Fatalf("NewDelegate: %v", err)
	}
	obj := d.Ptr()

	// Invoke (slot 3) with two ABI words.
	if hr := call(t, d, 3, 0x1111, 0x2222); hr != 0 {
		t.Fatalf("Invoke HRESULT = %#x", hr)
	}
	if len(got) != 2 || got[0] != 0x1111 || got[1] != 0x2222 {
		t.Fatalf("Invoke args = %#v", got)
	}

	// QueryInterface (slot 0) for the delegate IID, IUnknown, IAgileObject.
	for _, wanted := range []win32.GUID{iid, iidUnknown, iidAgileObject} {
		var out uintptr
		if hr := call(t, d, 0, uintptr(unsafe.Pointer(&wanted)), uintptr(unsafe.Pointer(&out))); hr != 0 {
			t.Fatalf("QI(%s) HRESULT = %#x", wanted, hr)
		}
		if out != obj {
			t.Fatalf("QI(%s) returned %#x, want self", wanted, out)
		}
		call(t, d, 2) // Release the QI reference.
	}

	// QI for a foreign IID must fail with E_NOINTERFACE and a nil out-ptr.
	foreign := MustGUID("ca30221d-86d9-40fb-a26b-d44eb7cf08ea")
	var out uintptr = 0xdead
	if hr := call(t, d, 0, uintptr(unsafe.Pointer(&foreign)), uintptr(unsafe.Pointer(&out))); hr != eNoInterface {
		t.Fatalf("foreign QI HRESULT = %#x, want E_NOINTERFACE", hr)
	}
	if out != 0 {
		t.Fatalf("foreign QI out = %#x, want 0", out)
	}

	// AddRef/Release bookkeeping; the final release unregisters.
	if refs := call(t, d, 1); refs != 2 {
		t.Fatalf("AddRef = %d, want 2", refs)
	}
	if refs := call(t, d, 2); refs != 1 {
		t.Fatalf("Release = %d, want 1", refs)
	}
	if refs := d.Release(); refs != 0 {
		t.Fatalf("final Release = %d, want 0", refs)
	}
	if registered(d) {
		t.Fatal("delegate still registered after final release")
	}

	// Invoking a released delegate fails instead of crashing.
	if hr := call(t, d, 3, 1, 2); hr == 0 {
		t.Fatal("Invoke after release succeeded")
	}
}

func TestDelegateParamCountBounds(t *testing.T) {
	iid := MustGUID("64e12a45-973b-4a3a-b260-91898a49a82c")
	for _, n := range []int{-1, 4} {
		if _, err := NewDelegate(iid, n, func([]uintptr) uintptr { return 0 }); err == nil {
			t.Errorf("NewDelegate(%d params) succeeded, want error", n)
		}
	}
}

// TestDelegateZeroParams covers the parameterless Invoke shape.
// DispatcherQueueHandler has one, and it is the delegate every TryEnqueue
// takes — without it there is no way to marshal work onto a UI thread.
func TestDelegateZeroParams(t *testing.T) {
	iid := MustGUID("2e0872a9-4e29-5f14-b688-fb96d5f9d5f8")
	invoked := 0
	var got []uintptr
	d, err := NewDelegate(iid, 0, func(args []uintptr) uintptr {
		invoked++
		got = slices.Clone(args)
		return 0
	})
	if err != nil {
		t.Fatalf("NewDelegate(0 params): %v", err)
	}
	defer d.Release()

	if hr := call(t, d, 3); hr != 0 {
		t.Fatalf("Invoke HRESULT = %#x", hr)
	}
	if invoked != 1 {
		t.Fatalf("handler ran %d times, want 1", invoked)
	}
	if len(got) != 0 {
		t.Fatalf("Invoke args = %#v, want none", got)
	}
}

// TestDelegateInlineThread pins the affinity contract SetInlineThread exists
// for: on the declared thread the handler must observe the invoking thread,
// not a goroutine the scheduler chose. XAML keys its state to the UI thread,
// so a handler that ran anywhere else could not legally touch it.
func TestDelegateInlineThread(t *testing.T) {
	iid := MustGUID("64e12a45-973b-4a3a-b260-91898a49a82c")

	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	callerTID := CurrentThreadID()

	var handlerTID uint32
	d, err := NewDelegate(iid, 1, func([]uintptr) uintptr {
		handlerTID = CurrentThreadID()
		return 0
	})
	if err != nil {
		t.Fatalf("NewDelegate: %v", err)
	}
	defer d.Release()

	// Default: the body is handed to a fresh goroutine, so it is not
	// guaranteed to observe the invoking thread.
	if hr := call(t, d, 3, 0); hr != 0 {
		t.Fatalf("Invoke HRESULT = %#x", hr)
	}

	SetInlineThread(callerTID)
	defer SetInlineThread(0)

	handlerTID = 0
	if hr := call(t, d, 3, 0); hr != 0 {
		t.Fatalf("inline Invoke HRESULT = %#x", hr)
	}
	if handlerTID != callerTID {
		t.Fatalf("handler ran on thread %d, want the invoking thread %d", handlerTID, callerTID)
	}
}

// TestDelegateInlineThreadOtherThread confirms the declaration is scoped to
// the named thread: a callback arriving elsewhere still takes the goroutine
// hop, so declaring a UI thread does not change dispatch for the rest of the
// process.
func TestDelegateInlineThreadOtherThread(t *testing.T) {
	iid := MustGUID("64e12a45-973b-4a3a-b260-91898a49a82c")

	// Declare a thread id that is not the one invoking below.
	SetInlineThread(^uint32(0))
	defer SetInlineThread(0)

	var handlerTID uint32
	d, err := NewDelegate(iid, 1, func([]uintptr) uintptr {
		handlerTID = CurrentThreadID()
		return 0
	})
	if err != nil {
		t.Fatalf("NewDelegate: %v", err)
	}
	defer d.Release()

	if hr := call(t, d, 3, 0); hr != 0 {
		t.Fatalf("Invoke HRESULT = %#x", hr)
	}
	if handlerTID == 0 {
		t.Fatal("handler did not run")
	}
}

// TestDelegateInlineReleasedFails keeps the released-delegate guard on the
// inline path: it must fail cleanly there exactly as it does on the parked
// path, rather than calling into a dead handler.
func TestDelegateInlineReleasedFails(t *testing.T) {
	iid := MustGUID("64e12a45-973b-4a3a-b260-91898a49a82c")

	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	SetInlineThread(CurrentThreadID())
	defer SetInlineThread(0)

	d, err := NewDelegate(iid, 1, func([]uintptr) uintptr { return 0 })
	if err != nil {
		t.Fatalf("NewDelegate: %v", err)
	}
	if refs := d.Release(); refs != 0 {
		t.Fatalf("final Release = %d, want 0", refs)
	}
	if hr := call(t, d, 3, 0); hr == 0 {
		t.Fatal("inline Invoke after release succeeded")
	}
}
