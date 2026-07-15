//go:build windows && (amd64 || arm64)

package winrt

import (
	"syscall"
	"testing"
	"unsafe"

	win32 "github.com/deploymenttheory/go-bindings-win32/bindings/runtime/win32"
	systemcom "github.com/deploymenttheory/go-bindings-win32/bindings/win32/system/com"
	syswinrt "github.com/deploymenttheory/go-bindings-win32/bindings/win32/system/winrt"
)

// callSlot drives a facet's vtable slot exactly as native WinRT code would:
// the facet word is `this`, its vtable is indexed at slot.
func callSlot(t *testing.T, this *inspectable, slot int, args ...uintptr) uintptr {
	t.Helper()
	vtbl := unsafe.Slice(this.lpVtbl, slot+1)
	r1, _, _ := syscall.SyscallN(vtbl[slot], append([]uintptr{uintptr(unsafe.Pointer(this))}, args...)...)
	return r1
}

// facetAt recovers the live facet registered under a native object word
// (e.g. a pointer a QI or First out-param produced).
func facetAt(t *testing.T, ptr uintptr) *inspectable {
	t.Helper()
	inspectableMu.Lock()
	defer inspectableMu.Unlock()
	facet := inspectableRegistry[ptr]
	if facet == nil {
		t.Fatalf("no live facet registered at %#x", ptr)
	}
	return facet
}

func qiSlot(t *testing.T, this *inspectable, iid win32.GUID) (uintptr, uintptr) {
	t.Helper()
	var out uintptr
	hr := callSlot(t, this, 0, uintptr(unsafe.Pointer(&iid)), uintptr(unsafe.Pointer(&out)))
	return hr, out
}

func TestStringIterableInspectableSlots(t *testing.T) {
	iterable := NewStringIterable([]string{"a"})
	hdr := &iterable.hdr
	obj := iterable.Ptr()

	// QI matrix: own IID + the identity set answer with self; foreign IIDs
	// (including IIterator<String>, which this object does NOT implement)
	// are rejected with E_NOINTERFACE and a nil out-ptr.
	for _, wanted := range []win32.GUID{IIDIterableOfString, iidUnknown, syswinrt.IID_IInspectable, iidAgileObject} {
		hr, out := qiSlot(t, hdr, wanted)
		if hr != 0 {
			t.Fatalf("QI(%s) HRESULT = %#x", wanted, hr)
		}
		if out != obj {
			t.Fatalf("QI(%s) returned %#x, want self", wanted, out)
		}
		callSlot(t, hdr, 2) // Release the QI reference.
	}
	for _, foreign := range []win32.GUID{IIDIteratorOfString, IIDVectorViewOfString, MustGUID("ca30221d-86d9-40fb-a26b-d44eb7cf08ea")} {
		hr, out := qiSlot(t, hdr, foreign)
		if hr != eNoInterface {
			t.Fatalf("QI(%s) HRESULT = %#x, want E_NOINTERFACE", foreign, hr)
		}
		if out != 0 {
			t.Fatalf("QI(%s) out = %#x, want 0", foreign, out)
		}
	}

	// GetIids (slot 3): callee CoTaskMemAlloc-allocates; count and content
	// must be exactly the non-identity set.
	var count uint32
	var iids *win32.GUID
	if hr := callSlot(t, hdr, 3, uintptr(unsafe.Pointer(&count)), uintptr(unsafe.Pointer(&iids))); hr != 0 {
		t.Fatalf("GetIids HRESULT = %#x", hr)
	}
	if count != 1 || iids == nil || *iids != IIDIterableOfString {
		t.Fatalf("GetIids = %d IIDs at %p, want [IIterable<String>]", count, iids)
	}
	systemcom.CoTaskMemFree(unsafe.Pointer(iids))

	// GetRuntimeClassName (slot 4): a NEW HSTRING the caller owns.
	var className syswinrt.HSTRING
	if hr := callSlot(t, hdr, 4, uintptr(unsafe.Pointer(&className))); hr != 0 {
		t.Fatalf("GetRuntimeClassName HRESULT = %#x", hr)
	}
	if got := TakeHString(className); got != "Windows.Foundation.Collections.IIterable`1<String>" {
		t.Errorf("runtime class name = %q", got)
	}

	// GetTrustLevel (slot 5): BaseTrust.
	trust := syswinrt.TrustLevel(-1)
	if hr := callSlot(t, hdr, 5, uintptr(unsafe.Pointer(&trust))); hr != 0 {
		t.Fatalf("GetTrustLevel HRESULT = %#x", hr)
	}
	if trust != syswinrt.BaseTrust {
		t.Errorf("trust level = %d, want BaseTrust", trust)
	}

	// AddRef/Release bookkeeping; the final release unregisters.
	if refs := callSlot(t, hdr, 1); refs != 2 {
		t.Fatalf("AddRef = %d, want 2", refs)
	}
	if refs := callSlot(t, hdr, 2); refs != 1 {
		t.Fatalf("Release = %d, want 1", refs)
	}
	if refs := iterable.Release(); refs != 0 {
		t.Fatalf("final Release = %d, want 0", refs)
	}
	if registeredInspectable(hdr) {
		t.Fatal("iterable still registered after final release")
	}

	// Dispatch after release fails instead of crashing.
	var dead uintptr
	if hr := callSlot(t, hdr, 6, uintptr(unsafe.Pointer(&dead))); hr != eFail {
		t.Fatalf("First after release HRESULT = %#x, want E_FAIL", hr)
	}
}

func TestStringIteratorSequence(t *testing.T) {
	items := []string{"en-US", "fr-FR", "日本語 ✓"}
	iterable := NewStringIterable(items)
	defer iterable.Release()

	// First (slot 6) hands out a NEW iterator; the reference is ours.
	var iterObj uintptr
	if hr := callSlot(t, &iterable.hdr, 6, uintptr(unsafe.Pointer(&iterObj))); hr != 0 {
		t.Fatalf("First HRESULT = %#x", hr)
	}
	if iterObj == 0 {
		t.Fatal("First returned a null iterator")
	}
	iter := facetAt(t, iterObj)

	// Walk the full get_HasCurrent(7)/get_Current(6)/MoveNext(8) protocol.
	var got []string
	for {
		var has byte
		if hr := callSlot(t, iter, 7, uintptr(unsafe.Pointer(&has))); hr != 0 {
			t.Fatalf("get_HasCurrent HRESULT = %#x", hr)
		}
		if has == 0 {
			break
		}
		var current syswinrt.HSTRING
		if hr := callSlot(t, iter, 6, uintptr(unsafe.Pointer(&current))); hr != 0 {
			t.Fatalf("get_Current HRESULT = %#x", hr)
		}
		got = append(got, TakeHString(current))
		var more byte
		if hr := callSlot(t, iter, 8, uintptr(unsafe.Pointer(&more))); hr != 0 {
			t.Fatalf("MoveNext HRESULT = %#x", hr)
		}
		if (more != 0) != (len(got) < len(items)) {
			t.Fatalf("MoveNext after %d items = %d", len(got), more)
		}
	}
	if len(got) != len(items) {
		t.Fatalf("iterated %v, want %v", got, items)
	}
	for i := range items {
		if got[i] != items[i] {
			t.Errorf("item %d = %q, want %q", i, got[i], items[i])
		}
	}

	// Exhausted: get_Current and a further MoveNext both fail with E_BOUNDS.
	var current syswinrt.HSTRING
	if hr := callSlot(t, iter, 6, uintptr(unsafe.Pointer(&current))); hr != eBounds {
		t.Fatalf("exhausted get_Current HRESULT = %#x, want E_BOUNDS", hr)
	}
	var more byte
	if hr := callSlot(t, iter, 8, uintptr(unsafe.Pointer(&more))); hr != eBounds {
		t.Fatalf("exhausted MoveNext HRESULT = %#x, want E_BOUNDS", hr)
	}

	if refs := callSlot(t, iter, 2); refs != 0 {
		t.Fatalf("iterator Release = %d, want 0", refs)
	}
}

func TestStringIteratorGetMany(t *testing.T) {
	iterator := NewStringIterator([]string{"one", "two", "three"})
	defer iterator.Release()
	hdr := &iterator.hdr

	getMany := func(capacity int) []string {
		t.Helper()
		buffer := make([]syswinrt.HSTRING, max(capacity, 1))
		var actual uint32
		hr := callSlot(t, hdr, 9, uintptr(capacity), uintptr(unsafe.Pointer(&buffer[0])), uintptr(unsafe.Pointer(&actual)))
		if hr != 0 {
			t.Fatalf("GetMany(%d) HRESULT = %#x", capacity, hr)
		}
		out := make([]string, actual)
		for i := range out {
			out[i] = TakeHString(buffer[i])
		}
		return out
	}

	// Partial read, then the remainder, then the empty tail.
	if got := getMany(2); len(got) != 2 || got[0] != "one" || got[1] != "two" {
		t.Fatalf("GetMany(2) = %v", got)
	}
	if got := getMany(2); len(got) != 1 || got[0] != "three" {
		t.Fatalf("GetMany(2) after partial = %v", got)
	}
	if got := getMany(2); len(got) != 0 {
		t.Fatalf("GetMany(2) when exhausted = %v", got)
	}

	// Zero capacity never touches the buffer.
	if got := getMany(0); len(got) != 0 {
		t.Fatalf("GetMany(0) = %v", got)
	}
}

func TestStringIteratorEmpty(t *testing.T) {
	iterator := NewStringIterator(nil)
	defer iterator.Release()
	hdr := &iterator.hdr

	var has byte = 0xff
	if hr := callSlot(t, hdr, 7, uintptr(unsafe.Pointer(&has))); hr != 0 {
		t.Fatalf("get_HasCurrent HRESULT = %#x", hr)
	}
	if has != 0 {
		t.Errorf("empty iterator HasCurrent = %d, want 0", has)
	}
	var current syswinrt.HSTRING
	if hr := callSlot(t, hdr, 6, uintptr(unsafe.Pointer(&current))); hr != eBounds {
		t.Fatalf("empty get_Current HRESULT = %#x, want E_BOUNDS", hr)
	}
	var more byte
	if hr := callSlot(t, hdr, 8, uintptr(unsafe.Pointer(&more))); hr != eBounds {
		t.Fatalf("empty MoveNext HRESULT = %#x, want E_BOUNDS", hr)
	}
}

func TestStringVectorViewSlots(t *testing.T) {
	items := []string{"alpha", "beta", "gamma"}
	view := NewStringVectorView(items)
	hdr := &view.hdr

	// get_Size (slot 7).
	var size uint32
	if hr := callSlot(t, hdr, 7, uintptr(unsafe.Pointer(&size))); hr != 0 {
		t.Fatalf("get_Size HRESULT = %#x", hr)
	}
	if size != 3 {
		t.Fatalf("Size = %d, want 3", size)
	}

	// GetAt (slot 6), in and out of bounds.
	var h syswinrt.HSTRING
	if hr := callSlot(t, hdr, 6, 1, uintptr(unsafe.Pointer(&h))); hr != 0 {
		t.Fatalf("GetAt(1) HRESULT = %#x", hr)
	}
	if got := TakeHString(h); got != "beta" {
		t.Errorf("GetAt(1) = %q, want beta", got)
	}
	if hr := callSlot(t, hdr, 6, 3, uintptr(unsafe.Pointer(&h))); hr != eBounds {
		t.Fatalf("GetAt(3) HRESULT = %#x, want E_BOUNDS", hr)
	}

	// IndexOf (slot 8): hit and miss (input HSTRING is borrowed).
	needle, err := NewHString("gamma")
	if err != nil {
		t.Fatalf("NewHString: %v", err)
	}
	defer needle.Close()
	var index uint32
	var found byte
	if hr := callSlot(t, hdr, 8, uintptr(needle.Raw()), uintptr(unsafe.Pointer(&index)), uintptr(unsafe.Pointer(&found))); hr != 0 {
		t.Fatalf("IndexOf HRESULT = %#x", hr)
	}
	if found == 0 || index != 2 {
		t.Errorf("IndexOf(gamma) = %d at %d, want found at 2", found, index)
	}
	miss, err := NewHString("delta")
	if err != nil {
		t.Fatalf("NewHString: %v", err)
	}
	defer miss.Close()
	index, found = 99, 0xff
	if hr := callSlot(t, hdr, 8, uintptr(miss.Raw()), uintptr(unsafe.Pointer(&index)), uintptr(unsafe.Pointer(&found))); hr != 0 {
		t.Fatalf("IndexOf(miss) HRESULT = %#x", hr)
	}
	if found != 0 || index != 0 {
		t.Errorf("IndexOf(delta) = %d at %d, want not found at 0", found, index)
	}

	// GetMany (slot 9): a middle window, then past the end.
	buffer := make([]syswinrt.HSTRING, 4)
	var actual uint32
	if hr := callSlot(t, hdr, 9, 1, 4, uintptr(unsafe.Pointer(&buffer[0])), uintptr(unsafe.Pointer(&actual))); hr != 0 {
		t.Fatalf("GetMany(1, 4) HRESULT = %#x", hr)
	}
	if actual != 2 {
		t.Fatalf("GetMany(1, 4) actual = %d, want 2", actual)
	}
	if got := TakeHString(buffer[0]); got != "beta" {
		t.Errorf("GetMany[0] = %q, want beta", got)
	}
	if got := TakeHString(buffer[1]); got != "gamma" {
		t.Errorf("GetMany[1] = %q, want gamma", got)
	}
	if hr := callSlot(t, hdr, 9, 3, 4, uintptr(unsafe.Pointer(&buffer[0])), uintptr(unsafe.Pointer(&actual))); hr != 0 {
		t.Fatalf("GetMany(3, 4) HRESULT = %#x", hr)
	}
	if actual != 0 {
		t.Fatalf("GetMany(3, 4) actual = %d, want 0", actual)
	}

	if refs := view.Release(); refs != 0 {
		t.Fatalf("final Release = %d, want 0", refs)
	}
}

// TestStringVectorViewTearOffIdentity pins the tear-off contract: the
// IIterable facet is a DIFFERENT pointer with its own vtable, its identity
// QIs resolve back to the primary pointer, both share one reference count,
// and GetIids/GetRuntimeClassName agree across facets.
func TestStringVectorViewTearOffIdentity(t *testing.T) {
	view := NewStringVectorView([]string{"x", "y"})
	defer view.Release()
	primaryHdr := &view.hdr
	primary := view.Ptr()

	// QI(IVectorView<String>) answers the primary facet.
	hr, got := qiSlot(t, primaryHdr, IIDVectorViewOfString)
	if hr != 0 || got != primary {
		t.Fatalf("QI(IVectorView) = %#x, %#x; want self", hr, got)
	}
	callSlot(t, primaryHdr, 2)

	// QI(IIterable<String>) answers the tear-off facet — a different pointer
	// inside the same allocation.
	hr, tearOff := qiSlot(t, primaryHdr, IIDIterableOfString)
	if hr != 0 {
		t.Fatalf("QI(IIterable) HRESULT = %#x", hr)
	}
	if tearOff == primary || tearOff == 0 {
		t.Fatalf("QI(IIterable) = %#x, want a distinct tear-off facet", tearOff)
	}
	tearOffHdr := facetAt(t, tearOff)

	// COM identity: QI(IUnknown)/QI(IInspectable) on the tear-off return the
	// PRIMARY pointer, and QI(IVectorView) recovers the primary too.
	for _, identityIID := range []win32.GUID{iidUnknown, syswinrt.IID_IInspectable, IIDVectorViewOfString} {
		hr, back := qiSlot(t, tearOffHdr, identityIID)
		if hr != 0 || back != primary {
			t.Fatalf("tear-off QI(%s) = %#x, %#x; want the primary facet", identityIID, hr, back)
		}
		callSlot(t, tearOffHdr, 2)
	}

	// GetIids on either facet reports the full pair.
	for _, facet := range []*inspectable{primaryHdr, tearOffHdr} {
		var count uint32
		var iids *win32.GUID
		if hr := callSlot(t, facet, 3, uintptr(unsafe.Pointer(&count)), uintptr(unsafe.Pointer(&iids))); hr != 0 {
			t.Fatalf("GetIids HRESULT = %#x", hr)
		}
		pair := unsafe.Slice(iids, count)
		if count != 2 || pair[0] != IIDVectorViewOfString || pair[1] != IIDIterableOfString {
			t.Fatalf("GetIids = %v, want [IVectorView<String>, IIterable<String>]", pair)
		}
		systemcom.CoTaskMemFree(unsafe.Pointer(iids))

		var className syswinrt.HSTRING
		if hr := callSlot(t, facet, 4, uintptr(unsafe.Pointer(&className))); hr != 0 {
			t.Fatalf("GetRuntimeClassName HRESULT = %#x", hr)
		}
		if got := TakeHString(className); got != "Windows.Foundation.Collections.IVectorView`1<String>" {
			t.Errorf("runtime class name via facet %p = %q", facet, got)
		}
	}

	// The tear-off's First (slot 6) yields a working iterator.
	var iterObj uintptr
	if hr := callSlot(t, tearOffHdr, 6, uintptr(unsafe.Pointer(&iterObj))); hr != 0 {
		t.Fatalf("tear-off First HRESULT = %#x", hr)
	}
	iter := facetAt(t, iterObj)
	var current syswinrt.HSTRING
	if hr := callSlot(t, iter, 6, uintptr(unsafe.Pointer(&current))); hr != 0 {
		t.Fatalf("iterator get_Current HRESULT = %#x", hr)
	}
	if got := TakeHString(current); got != "x" {
		t.Errorf("iterator Current = %q, want x", got)
	}
	if refs := callSlot(t, iter, 2); refs != 0 {
		t.Fatalf("iterator Release = %d, want 0", refs)
	}

	// AddRef through the tear-off and Release through the primary land on the
	// SAME shared count (1 caller ref + 1 QI ref still held on the tear-off).
	if refs := callSlot(t, tearOffHdr, 1); refs != 3 {
		t.Fatalf("tear-off AddRef = %d, want 3", refs)
	}
	if refs := callSlot(t, primaryHdr, 2); refs != 2 {
		t.Fatalf("primary Release = %d, want 2", refs)
	}
	if refs := callSlot(t, tearOffHdr, 2); refs != 1 { // the QI reference
		t.Fatalf("tear-off Release = %d, want 1", refs)
	}
}

// TestStringIterableSourceIsolation pins the copy semantics: mutating the
// source slice after construction must not leak into the object.
func TestStringIterableSourceIsolation(t *testing.T) {
	source := []string{"before"}
	iterator := NewStringIterator(source)
	defer iterator.Release()
	source[0] = "after"

	var current syswinrt.HSTRING
	if hr := callSlot(t, &iterator.hdr, 6, uintptr(unsafe.Pointer(&current))); hr != 0 {
		t.Fatalf("get_Current HRESULT = %#x", hr)
	}
	if got := TakeHString(current); got != "before" {
		t.Errorf("Current = %q, want the copied value", got)
	}
}
