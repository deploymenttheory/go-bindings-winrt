//go:build windows && (amd64 || arm64)

package winrt

import (
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
	for _, n := range []int{0, 4} {
		if _, err := NewDelegate(iid, n, func([]uintptr) uintptr { return 0 }); err == nil {
			t.Errorf("NewDelegate(%d params) succeeded, want error", n)
		}
	}
}
