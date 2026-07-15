//go:build windows && (amd64 || arm64)

package winrt

import (
	"testing"
	"unsafe"

	win32 "github.com/deploymenttheory/go-bindings-win32/bindings/runtime/win32"
	syswinrt "github.com/deploymenttheory/go-bindings-win32/bindings/win32/system/winrt"
)

// Test IIDs (arbitrary but distinct — the runtime never interprets them
// beyond QI equality).
var testCollectionIIDs = CollectionIIDs{
	Iterable:   MustGUID("11111111-1111-1111-1111-111111111111"),
	Iterator:   MustGUID("22222222-2222-2222-2222-222222222222"),
	VectorView: MustGUID("33333333-3333-3333-3333-333333333333"),
	Vector:     MustGUID("44444444-4444-4444-4444-444444444444"),
}

// --- Codec matrix -----------------------------------------------------------

func TestCodecStringRoundTrip(t *testing.T) {
	var slot syswinrt.HSTRING
	if hr := CodecString.MarshalOut("héllo ✓", unsafe.Pointer(&slot)); hr != 0 {
		t.Fatalf("MarshalOut HRESULT = %#x", hr)
	}
	if slot == 0 {
		t.Fatal("MarshalOut wrote a null HSTRING for a non-empty string")
	}
	elem, hr := CodecString.UnmarshalSlot(unsafe.Pointer(&slot))
	if hr != 0 || elem.(string) != "héllo ✓" {
		t.Fatalf("UnmarshalSlot = %v, %#x", elem, hr)
	}
	if !CodecString.EqualWord("héllo ✓", uintptr(slot)) {
		t.Error("EqualWord(same value) = false")
	}
	if CodecString.EqualWord("other", uintptr(slot)) {
		t.Error("EqualWord(different value) = true")
	}
	word, hr := CodecString.UnmarshalWord(uintptr(slot))
	if hr != 0 || word.(string) != "héllo ✓" {
		t.Fatalf("UnmarshalWord = %v, %#x", word, hr)
	}
	CodecString.FreeOut(unsafe.Pointer(&slot))
	if slot != 0 {
		t.Error("FreeOut left the slot non-zero")
	}
	if CodecString.ABISize() != unsafe.Sizeof(syswinrt.HSTRING(0)) {
		t.Errorf("ABISize = %d", CodecString.ABISize())
	}
}

func TestCodecScalarMasking(t *testing.T) {
	codec := CodecScalar(4)
	if codec.ABISize() != 4 {
		t.Fatalf("ABISize = %d, want 4", codec.ABISize())
	}
	// A sign-extended boxed element (uint64(int32(-5))) must compare equal
	// to the zero-extended raw word the ABI delivers.
	boxed := uint64(0xFFFFFFFF_FFFFFFFB) // int32(-5) sign-extended
	raw := uintptr(0xFFFFFFFB)           // the low 32 bits as a register word
	if !codec.EqualWord(boxed, raw) {
		t.Error("EqualWord(sign-extended elem, zero-extended word) = false")
	}
	if codec.EqualWord(boxed, uintptr(5)) {
		t.Error("EqualWord(-5, 5) = true")
	}
	var slot [8]byte
	slot[4], slot[5] = 0xEE, 0xEE // dirt beyond the element width
	if hr := codec.MarshalOut(boxed, unsafe.Pointer(&slot[0])); hr != 0 {
		t.Fatalf("MarshalOut HRESULT = %#x", hr)
	}
	elem, hr := codec.UnmarshalSlot(unsafe.Pointer(&slot[0]))
	if hr != 0 || elem.(uint64) != 0xFFFFFFFB {
		t.Fatalf("UnmarshalSlot = %#x, %#x; want 0xFFFFFFFB", elem, hr)
	}
	if slot[4] != 0xEE || slot[5] != 0xEE {
		t.Error("MarshalOut(4) wrote beyond the element width")
	}
	word, _ := codec.UnmarshalWord(raw)
	if word.(uint64) != 0xFFFFFFFB {
		t.Fatalf("UnmarshalWord = %#x", word)
	}
	for _, size := range []uintptr{1, 2, 8} {
		c := CodecScalar(size)
		if c.ABISize() != size {
			t.Errorf("CodecScalar(%d).ABISize() = %d", size, c.ABISize())
		}
	}
	defer func() {
		if recover() == nil {
			t.Error("CodecScalar(3) did not panic")
		}
	}()
	CodecScalar(3)
}

func TestCodecGuidWordIsPointer(t *testing.T) {
	value := MustGUID("a3b1c2d3-e4f5-0617-2839-4a5b6c7d8e9f")
	var slot win32.GUID
	if hr := CodecGuid.MarshalOut(value, unsafe.Pointer(&slot)); hr != 0 {
		t.Fatalf("MarshalOut HRESULT = %#x", hr)
	}
	if slot != value {
		t.Fatalf("MarshalOut wrote %s", slot)
	}
	if CodecGuid.ABISize() != 16 {
		t.Fatalf("ABISize = %d, want 16", CodecGuid.ABISize())
	}
	// The register word for a GUID input is a POINTER to the value.
	elem, hr := CodecGuid.UnmarshalWord(uintptr(unsafe.Pointer(&value)))
	if hr != 0 || elem.(win32.GUID) != value {
		t.Fatalf("UnmarshalWord = %v, %#x", elem, hr)
	}
	if _, hr := CodecGuid.UnmarshalWord(0); hr != ePointer {
		t.Fatalf("UnmarshalWord(0) HRESULT = %#x, want E_POINTER", hr)
	}
	if !CodecGuid.EqualWord(value, uintptr(unsafe.Pointer(&value))) {
		t.Error("EqualWord(same GUID) = false")
	}
	other := MustGUID("00000000-0000-0000-0000-000000000001")
	if CodecGuid.EqualWord(other, uintptr(unsafe.Pointer(&value))) {
		t.Error("EqualWord(different GUID) = true")
	}
	if CodecGuid.EqualWord(value, 0) {
		t.Error("EqualWord(nil word) = true")
	}
	slotElem, hr := CodecGuid.UnmarshalSlot(unsafe.Pointer(&slot))
	if hr != 0 || slotElem.(win32.GUID) != value {
		t.Fatalf("UnmarshalSlot = %v, %#x", slotElem, hr)
	}
}

// testDelegate builds a Go-implemented delegate purely as a refcount-
// observable COM element: our own delegates expose their exact reference
// count, so retention arithmetic can be asserted, and every AddRef/Release
// the codec issues against them REENTERS the Go trampolines — from the
// worker thread when issued by a collection body, which exercises the
// inline-dispatch path in dispatchInspectable (this test suite deadlocks
// without it).
func testDelegate(t *testing.T) *Delegate {
	t.Helper()
	d, err := NewDelegate(MustGUID("55555555-5555-5555-5555-555555555555"), 1,
		func([]uintptr) uintptr { return 0 })
	if err != nil {
		t.Fatalf("NewDelegate: %v", err)
	}
	return d
}

func TestCodecInterfaceRefcounts(t *testing.T) {
	d := testDelegate(t)
	defer d.Release()
	word := d.Ptr()

	if refs := d.refs.Load(); refs != 1 {
		t.Fatalf("fresh delegate refs = %d, want 1", refs)
	}
	elem, hr := CodecInterface.UnmarshalWord(word)
	if hr != 0 || elem.(uintptr) != word {
		t.Fatalf("UnmarshalWord = %#x, %#x", elem, hr)
	}
	if refs := d.refs.Load(); refs != 2 {
		t.Fatalf("refs after UnmarshalWord = %d, want 2 (retained)", refs)
	}
	var slot uintptr
	if hr := CodecInterface.MarshalOut(elem, unsafe.Pointer(&slot)); hr != 0 {
		t.Fatalf("MarshalOut HRESULT = %#x", hr)
	}
	if slot != word {
		t.Fatalf("MarshalOut wrote %#x, want %#x", slot, word)
	}
	if refs := d.refs.Load(); refs != 3 {
		t.Fatalf("refs after MarshalOut = %d, want 3", refs)
	}
	fromSlot, hr := CodecInterface.UnmarshalSlot(unsafe.Pointer(&slot))
	if hr != 0 || fromSlot.(uintptr) != word {
		t.Fatalf("UnmarshalSlot = %#x, %#x", fromSlot, hr)
	}
	if refs := d.refs.Load(); refs != 4 {
		t.Fatalf("refs after UnmarshalSlot = %d, want 4", refs)
	}
	CodecInterface.FreeOut(unsafe.Pointer(&slot))
	if slot != 0 {
		t.Error("FreeOut left the slot non-zero")
	}
	CodecInterface.Release(elem)
	CodecInterface.Release(fromSlot)
	if refs := d.refs.Load(); refs != 1 {
		t.Fatalf("refs after releases = %d, want 1", refs)
	}
	// Identity-word equality: the exact word matches, everything else —
	// including null — does not.
	if !CodecInterface.EqualWord(word, word) {
		t.Error("EqualWord(identity) = false")
	}
	if CodecInterface.EqualWord(word, 0) || CodecInterface.EqualWord(uintptr(0), word) {
		t.Error("EqualWord mismatched null")
	}
	// Null elements cross without ref traffic.
	nullElem, hr := CodecInterface.UnmarshalWord(0)
	if hr != 0 || nullElem.(uintptr) != 0 {
		t.Fatalf("UnmarshalWord(0) = %#x, %#x", nullElem, hr)
	}
	CodecInterface.Release(nullElem) // must not crash
}

// --- Vector: all 12 slots through the native vtable -------------------------

func newTestStringVector(t *testing.T, items ...string) *Vector {
	t.Helper()
	boxed := make([]any, len(items))
	for i, s := range items {
		boxed[i] = s
	}
	return NewVectorObject("Windows.Foundation.Collections.IVector`1<String>",
		testCollectionIIDs, CodecString, boxed)
}

func vectorGetAtString(t *testing.T, hdr *inspectable, index uintptr) (string, uintptr) {
	t.Helper()
	var h syswinrt.HSTRING
	hr := callSlot(t, hdr, 6, index, uintptr(unsafe.Pointer(&h)))
	if hr != 0 {
		return "", hr
	}
	return TakeHString(h), 0
}

func vectorSizeOf(t *testing.T, hdr *inspectable) uint32 {
	t.Helper()
	var size uint32
	if hr := callSlot(t, hdr, 7, uintptr(unsafe.Pointer(&size))); hr != 0 {
		t.Fatalf("get_Size HRESULT = %#x", hr)
	}
	return size
}

func TestVectorWritableSlots(t *testing.T) {
	vector := newTestStringVector(t, "a", "b", "c")
	hdr := &vector.hdr

	// GetAt (6) + get_Size (7) + bounds.
	if got, hr := vectorGetAtString(t, hdr, 1); hr != 0 || got != "b" {
		t.Fatalf("GetAt(1) = %q, %#x", got, hr)
	}
	if _, hr := vectorGetAtString(t, hdr, 3); hr != eBounds {
		t.Fatalf("GetAt(3) HRESULT = %#x, want E_BOUNDS", hr)
	}
	if size := vectorSizeOf(t, hdr); size != 3 {
		t.Fatalf("Size = %d, want 3", size)
	}

	// IndexOf (9): hit and miss (input borrowed).
	needle, err := NewHString("c")
	if err != nil {
		t.Fatal(err)
	}
	defer needle.Close()
	var index uint32
	var found byte
	if hr := callSlot(t, hdr, 9, uintptr(needle.Raw()), uintptr(unsafe.Pointer(&index)), uintptr(unsafe.Pointer(&found))); hr != 0 {
		t.Fatalf("IndexOf HRESULT = %#x", hr)
	}
	if found == 0 || index != 2 {
		t.Fatalf("IndexOf(c) = %d at %d, want found at 2", found, index)
	}

	// SetAt (10): replaces in place; out of bounds is E_BOUNDS.
	setAt := func(i int, s string) uintptr {
		h, err := NewHString(s)
		if err != nil {
			t.Fatal(err)
		}
		defer h.Close()
		return callSlot(t, hdr, 10, uintptr(i), uintptr(h.Raw()))
	}
	if hr := setAt(1, "B"); hr != 0 {
		t.Fatalf("SetAt(1) HRESULT = %#x", hr)
	}
	if got, _ := vectorGetAtString(t, hdr, 1); got != "B" {
		t.Fatalf("after SetAt, GetAt(1) = %q", got)
	}
	if hr := setAt(3, "x"); hr != eBounds {
		t.Fatalf("SetAt(3) HRESULT = %#x, want E_BOUNDS", hr)
	}

	// InsertAt (11): middle, end (== Size), and beyond (E_BOUNDS).
	insertAt := func(i int, s string) uintptr {
		h, err := NewHString(s)
		if err != nil {
			t.Fatal(err)
		}
		defer h.Close()
		return callSlot(t, hdr, 11, uintptr(i), uintptr(h.Raw()))
	}
	if hr := insertAt(1, "mid"); hr != 0 {
		t.Fatalf("InsertAt(1) HRESULT = %#x", hr)
	}
	if hr := insertAt(4, "end"); hr != 0 {
		t.Fatalf("InsertAt(len) HRESULT = %#x", hr)
	}
	if hr := insertAt(6, "beyond"); hr != eBounds {
		t.Fatalf("InsertAt(len+1) HRESULT = %#x, want E_BOUNDS", hr)
	}
	// Now: a, mid, B, c, end.
	if got, _ := vectorGetAtString(t, hdr, 1); got != "mid" {
		t.Fatalf("GetAt(1) = %q, want mid", got)
	}
	if got, _ := vectorGetAtString(t, hdr, 4); got != "end" {
		t.Fatalf("GetAt(4) = %q, want end", got)
	}

	// RemoveAt (12) + bounds.
	if hr := callSlot(t, hdr, 12, 1); hr != 0 {
		t.Fatalf("RemoveAt(1) HRESULT = %#x", hr)
	}
	if hr := callSlot(t, hdr, 12, 4); hr != eBounds {
		t.Fatalf("RemoveAt(past end) HRESULT = %#x, want E_BOUNDS", hr)
	}

	// Append (13).
	appended, err := NewHString("tail")
	if err != nil {
		t.Fatal(err)
	}
	defer appended.Close()
	if hr := callSlot(t, hdr, 13, uintptr(appended.Raw())); hr != 0 {
		t.Fatalf("Append HRESULT = %#x", hr)
	}
	// Now: a, B, c, end, tail.
	if size := vectorSizeOf(t, hdr); size != 5 {
		t.Fatalf("Size = %d, want 5", size)
	}
	if got, _ := vectorGetAtString(t, hdr, 4); got != "tail" {
		t.Fatalf("GetAt(4) = %q, want tail", got)
	}

	// GetMany (16): a middle window; partial read past the end.
	buffer := make([]syswinrt.HSTRING, 4)
	var actual uint32
	if hr := callSlot(t, hdr, 16, 3, 4, uintptr(unsafe.Pointer(&buffer[0])), uintptr(unsafe.Pointer(&actual))); hr != 0 {
		t.Fatalf("GetMany(3, 4) HRESULT = %#x", hr)
	}
	if actual != 2 {
		t.Fatalf("GetMany(3, 4) actual = %d, want 2", actual)
	}
	if got := TakeHString(buffer[0]); got != "end" {
		t.Errorf("GetMany[0] = %q, want end", got)
	}
	if got := TakeHString(buffer[1]); got != "tail" {
		t.Errorf("GetMany[1] = %q, want tail", got)
	}

	// ReplaceAll (17): wholesale swap from a native-shaped input array.
	replacement := make([]syswinrt.HSTRING, 2)
	for i, s := range []string{"x", "y"} {
		h, err := NewHString(s)
		if err != nil {
			t.Fatal(err)
		}
		replacement[i] = h.Raw() // vector borrows; we still own these
	}
	if hr := callSlot(t, hdr, 17, 2, uintptr(unsafe.Pointer(&replacement[0]))); hr != 0 {
		t.Fatalf("ReplaceAll HRESULT = %#x", hr)
	}
	for _, h := range replacement {
		_ = syswinrt.WindowsDeleteString(h)
	}
	if size := vectorSizeOf(t, hdr); size != 2 {
		t.Fatalf("Size after ReplaceAll = %d, want 2", size)
	}
	if got, _ := vectorGetAtString(t, hdr, 0); got != "x" {
		t.Fatalf("GetAt(0) = %q, want x", got)
	}

	// RemoveAtEnd (14) down to empty, then E_BOUNDS.
	if hr := callSlot(t, hdr, 14); hr != 0 {
		t.Fatalf("RemoveAtEnd HRESULT = %#x", hr)
	}
	if hr := callSlot(t, hdr, 14); hr != 0 {
		t.Fatalf("RemoveAtEnd HRESULT = %#x", hr)
	}
	if hr := callSlot(t, hdr, 14); hr != eBounds {
		t.Fatalf("RemoveAtEnd(empty) HRESULT = %#x, want E_BOUNDS", hr)
	}

	// Clear (15) is idempotent and legal on empty.
	if hr := callSlot(t, hdr, 15); hr != 0 {
		t.Fatalf("Clear HRESULT = %#x", hr)
	}

	if refs := vector.Release(); refs != 0 {
		t.Fatalf("final Release = %d, want 0", refs)
	}
}

// TestVectorGetViewSnapshot pins the documented snapshot semantics: the view
// captures the contents at GetView time and later vector mutation does not
// leak in; the view also answers the vector-view and iterable IIDs.
func TestVectorGetViewSnapshot(t *testing.T) {
	vector := newTestStringVector(t, "one", "two")
	defer vector.Release()

	var viewObj uintptr
	if hr := callSlot(t, &vector.hdr, 8, uintptr(unsafe.Pointer(&viewObj))); hr != 0 {
		t.Fatalf("GetView HRESULT = %#x", hr)
	}
	view := facetAt(t, viewObj)

	// Mutate the vector AFTER taking the view.
	h, err := NewHString("three")
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	if hr := callSlot(t, &vector.hdr, 13, uintptr(h.Raw())); hr != 0 {
		t.Fatalf("Append HRESULT = %#x", hr)
	}
	if size := vectorSizeOf(t, &vector.hdr); size != 3 {
		t.Fatalf("vector Size = %d, want 3", size)
	}

	// The snapshot is unchanged: size 2 (IVectorView get_Size is slot 7).
	var viewSize uint32
	if hr := callSlot(t, view, 7, uintptr(unsafe.Pointer(&viewSize))); hr != 0 {
		t.Fatalf("view get_Size HRESULT = %#x", hr)
	}
	if viewSize != 2 {
		t.Fatalf("view Size = %d, want the snapshot's 2", viewSize)
	}
	var got syswinrt.HSTRING
	if hr := callSlot(t, view, 6, 1, uintptr(unsafe.Pointer(&got))); hr != 0 {
		t.Fatalf("view GetAt HRESULT = %#x", hr)
	}
	if s := TakeHString(got); s != "two" {
		t.Fatalf("view GetAt(1) = %q, want two", s)
	}

	// The view answers its own IIDs with COM identity intact.
	if hr, out := qiSlot(t, view, testCollectionIIDs.VectorView); hr != 0 || out != viewObj {
		t.Fatalf("view QI(VectorView) = %#x, %#x", hr, out)
	}
	callSlot(t, view, 2)
	hr, iterableFacet := qiSlot(t, view, testCollectionIIDs.Iterable)
	if hr != 0 || iterableFacet == 0 || iterableFacet == viewObj {
		t.Fatalf("view QI(Iterable) = %#x, %#x; want a tear-off facet", hr, iterableFacet)
	}
	callSlot(t, facetAt(t, iterableFacet), 2)

	if refs := callSlot(t, view, 2); refs != 0 {
		t.Fatalf("view Release = %d, want 0", refs)
	}
}

// TestVectorTearOffIterates drives the vector's IIterable tear-off: QI for
// the iterable IID lands on the tear-off facet, First yields an iterator
// over a snapshot, and mutating the vector mid-iteration does not disturb
// the iterator (it owns its own retained copies).
func TestVectorTearOffIterates(t *testing.T) {
	vector := newTestStringVector(t, "p", "q")
	defer vector.Release()

	hr, tearOff := qiSlot(t, &vector.hdr, testCollectionIIDs.Iterable)
	if hr != 0 {
		t.Fatalf("QI(Iterable) HRESULT = %#x", hr)
	}
	tearOffHdr := facetAt(t, tearOff)
	// Identity: QI(IUnknown) on the tear-off answers the vector pointer.
	if hr, back := qiSlot(t, tearOffHdr, iidUnknown); hr != 0 || back != vector.Ptr() {
		t.Fatalf("tear-off QI(IUnknown) = %#x, %#x; want the identity", hr, back)
	}
	callSlot(t, tearOffHdr, 2)

	var iterObj uintptr
	if hr := callSlot(t, tearOffHdr, 6, uintptr(unsafe.Pointer(&iterObj))); hr != 0 {
		t.Fatalf("tear-off First HRESULT = %#x", hr)
	}
	iter := facetAt(t, iterObj)

	// Clear the vector mid-iteration; the iterator's snapshot survives.
	if hr := callSlot(t, &vector.hdr, 15); hr != 0 {
		t.Fatalf("Clear HRESULT = %#x", hr)
	}
	var got []string
	for {
		var has byte
		if hr := callSlot(t, iter, 7, uintptr(unsafe.Pointer(&has))); hr != 0 {
			t.Fatalf("HasCurrent HRESULT = %#x", hr)
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
	}
	if len(got) != 2 || got[0] != "p" || got[1] != "q" {
		t.Fatalf("iterated %v, want [p q]", got)
	}
	callSlot(t, tearOffHdr, 2) // the QI reference from First's tear-off QI
	if refs := callSlot(t, iter, 2); refs != 0 {
		t.Fatalf("iterator Release = %d, want 0", refs)
	}
}

// TestVectorInterfaceElementLifecycle pins the interface-element retention
// arithmetic with an observable element: a Go-implemented delegate. Every
// AddRef/Release the codec issues reenters OUR trampolines — from the
// worker thread when a collection body triggers it, so this test hangs
// without dispatchInspectable's inline reentry path.
func TestVectorInterfaceElementLifecycle(t *testing.T) {
	d1, d2, d3 := testDelegate(t), testDelegate(t), testDelegate(t)
	defer d1.Release()
	defer d2.Release()
	defer d3.Release()

	vector := NewVectorObject("Windows.Foundation.Collections.IVector`1<Object>",
		testCollectionIIDs, CodecInterface, []any{d1.Ptr(), d2.Ptr()})
	// Construction retains: caller ref + vector ref.
	if refs := d1.refs.Load(); refs != 2 {
		t.Fatalf("d1 refs after construction = %d, want 2", refs)
	}
	if refs := d3.refs.Load(); refs != 1 {
		t.Fatalf("d3 refs before insertion = %d, want 1", refs)
	}

	// GetView snapshots re-retain every element (worker-side AddRef →
	// inline reentry).
	var viewObj uintptr
	if hr := callSlot(t, &vector.hdr, 8, uintptr(unsafe.Pointer(&viewObj))); hr != 0 {
		t.Fatalf("GetView HRESULT = %#x", hr)
	}
	view := facetAt(t, viewObj)
	if refs := d1.refs.Load(); refs != 3 {
		t.Fatalf("d1 refs after GetView = %d, want 3", refs)
	}

	// GetAt hands out a caller-owned reference (MarshalOut = AddRef).
	var out uintptr
	if hr := callSlot(t, &vector.hdr, 6, 0, uintptr(unsafe.Pointer(&out))); hr != 0 {
		t.Fatalf("GetAt HRESULT = %#x", hr)
	}
	if out != d1.Ptr() {
		t.Fatalf("GetAt(0) = %#x, want d1", out)
	}
	if refs := d1.refs.Load(); refs != 4 {
		t.Fatalf("d1 refs after GetAt = %d, want 4", refs)
	}
	comRelease(out) // we own it; give it back

	// IndexOf is identity-word equality and issues NO ref traffic.
	var index uint32
	var found byte
	if hr := callSlot(t, &vector.hdr, 9, d2.Ptr(), uintptr(unsafe.Pointer(&index)), uintptr(unsafe.Pointer(&found))); hr != 0 {
		t.Fatalf("IndexOf HRESULT = %#x", hr)
	}
	if found == 0 || index != 1 {
		t.Fatalf("IndexOf(d2) = %d at %d, want found at 1", found, index)
	}
	if refs := d2.refs.Load(); refs != 3 {
		t.Fatalf("d2 refs after IndexOf = %d, want 3 (no ref traffic)", refs)
	}

	// SetAt(0, d3) retains d3 and releases the displaced d1.
	if hr := callSlot(t, &vector.hdr, 10, 0, d3.Ptr()); hr != 0 {
		t.Fatalf("SetAt HRESULT = %#x", hr)
	}
	if refs := d1.refs.Load(); refs != 2 {
		t.Fatalf("d1 refs after displacement = %d, want 2 (caller + view snapshot)", refs)
	}
	if refs := d3.refs.Load(); refs != 2 {
		t.Fatalf("d3 refs after SetAt = %d, want 2", refs)
	}

	// RemoveAtEnd releases d2 from the vector (the view still holds one).
	if hr := callSlot(t, &vector.hdr, 14); hr != 0 {
		t.Fatalf("RemoveAtEnd HRESULT = %#x", hr)
	}
	if refs := d2.refs.Load(); refs != 2 {
		t.Fatalf("d2 refs after RemoveAtEnd = %d, want 2", refs)
	}

	// Releasing the view releases its snapshot references (destroy hook).
	if refs := callSlot(t, view, 2); refs != 0 {
		t.Fatalf("view Release = %d, want 0", refs)
	}
	if refs := d1.refs.Load(); refs != 1 {
		t.Fatalf("d1 refs after view release = %d, want 1", refs)
	}
	if refs := d2.refs.Load(); refs != 1 {
		t.Fatalf("d2 refs after view release = %d, want 1", refs)
	}

	// Final vector release runs the destroy hook: d3 (its only element left)
	// drops back to the caller-owned reference.
	if refs := vector.Release(); refs != 0 {
		t.Fatalf("vector Release = %d, want 0", refs)
	}
	if refs := d3.refs.Load(); refs != 1 {
		t.Fatalf("d3 refs after vector release = %d, want 1", refs)
	}
}

// --- Unwind paths -----------------------------------------------------------

// unwindCodec is a controllable ElementCodec: it fails MarshalOut or
// UnmarshalSlot at a chosen call ordinal and counts FreeOut/Release calls,
// making the GetMany and ReplaceAll unwind guarantees directly assertable.
type unwindCodec struct {
	failMarshalAt   int // MarshalOut ordinal (0-based) to fail at; -1 never
	failUnmarshalAt int // UnmarshalSlot ordinal (0-based) to fail at; -1 never
	marshals        int
	unmarshals      int
	frees           int
	releases        int
}

func (c *unwindCodec) ABISize() uintptr { return unsafe.Sizeof(uintptr(0)) }

func (c *unwindCodec) MarshalOut(elem any, out unsafe.Pointer) uintptr {
	ordinal := c.marshals
	c.marshals++
	if ordinal == c.failMarshalAt {
		return eFail
	}
	*(*uintptr)(out) = 0xC0DEC
	return 0
}

func (c *unwindCodec) FreeOut(out unsafe.Pointer) {
	c.frees++
	*(*uintptr)(out) = 0
}

func (c *unwindCodec) UnmarshalWord(raw uintptr) (any, uintptr) { return raw, 0 }

func (c *unwindCodec) UnmarshalSlot(slot unsafe.Pointer) (any, uintptr) {
	ordinal := c.unmarshals
	c.unmarshals++
	if ordinal == c.failUnmarshalAt {
		return nil, eFail
	}
	return *(*uintptr)(slot), 0
}

func (c *unwindCodec) EqualWord(elem any, raw uintptr) bool { return elem.(uintptr) == raw }

func (c *unwindCodec) Release(any) { c.releases++ }

func TestGetManyUnwind(t *testing.T) {
	codec := &unwindCodec{failMarshalAt: 2, failUnmarshalAt: -1}
	vector := NewVectorObject("Windows.Foundation.Collections.IVector`1<T>",
		testCollectionIIDs, codec, []any{uintptr(1), uintptr(2), uintptr(3), uintptr(4)})
	defer vector.Release()

	buffer := make([]uintptr, 4)
	var actual uint32 = 99
	hr := callSlot(t, &vector.hdr, 16, 0, 4, uintptr(unsafe.Pointer(&buffer[0])), uintptr(unsafe.Pointer(&actual)))
	if hr != eFail {
		t.Fatalf("GetMany HRESULT = %#x, want the codec failure", hr)
	}
	if actual != 0 {
		t.Errorf("actual = %d, want 0 after unwind", actual)
	}
	if codec.frees != 2 {
		t.Errorf("FreeOut calls = %d, want 2 (both successfully written slots)", codec.frees)
	}
	if buffer[0] != 0 || buffer[1] != 0 {
		t.Errorf("unwound slots = %#x, %#x; want zeroed", buffer[0], buffer[1])
	}

	// After the failure the vector still works: a clean partial read.
	codec.failMarshalAt = -1
	if hr := callSlot(t, &vector.hdr, 16, 2, 4, uintptr(unsafe.Pointer(&buffer[0])), uintptr(unsafe.Pointer(&actual))); hr != 0 {
		t.Fatalf("GetMany retry HRESULT = %#x", hr)
	}
	if actual != 2 {
		t.Errorf("retry actual = %d, want 2", actual)
	}
}

func TestReplaceAllUnwind(t *testing.T) {
	codec := &unwindCodec{failMarshalAt: -1, failUnmarshalAt: 2}
	vector := NewVectorObject("Windows.Foundation.Collections.IVector`1<T>",
		testCollectionIIDs, codec, []any{uintptr(10), uintptr(20)})
	defer vector.Release()

	input := []uintptr{101, 102, 103, 104}
	hr := callSlot(t, &vector.hdr, 17, 4, uintptr(unsafe.Pointer(&input[0])))
	if hr != eFail {
		t.Fatalf("ReplaceAll HRESULT = %#x, want the codec failure", hr)
	}
	// All-or-nothing: the two elements retained before the failure were
	// released again, and the ORIGINAL payload is untouched.
	if codec.releases != 2 {
		t.Errorf("Release calls = %d, want 2 (the partial fresh slice)", codec.releases)
	}
	if size := vectorSizeOf(t, &vector.hdr); size != 2 {
		t.Fatalf("Size after failed ReplaceAll = %d, want the original 2", size)
	}
	var out uintptr
	if hr := callSlot(t, &vector.hdr, 6, 0, uintptr(unsafe.Pointer(&out))); hr != 0 || out != 0xC0DEC {
		t.Fatalf("GetAt(0) = %#x, %#x; want the marshaled original", out, hr)
	}

	// A clean ReplaceAll then swaps: old elements released, new visible.
	codec.failUnmarshalAt = -1
	releasesBefore := codec.releases
	if hr := callSlot(t, &vector.hdr, 17, 3, uintptr(unsafe.Pointer(&input[0]))); hr != 0 {
		t.Fatalf("ReplaceAll retry HRESULT = %#x", hr)
	}
	if codec.releases != releasesBefore+2 {
		t.Errorf("Release calls after swap = %d, want +2 for the old payload", codec.releases-releasesBefore)
	}
	if size := vectorSizeOf(t, &vector.hdr); size != 3 {
		t.Fatalf("Size after ReplaceAll = %d, want 3", size)
	}
}

// TestReplaceAllNullItems pins the pointer contract: a non-zero count with a
// null items array is E_POINTER; count zero clears.
func TestReplaceAllNullItems(t *testing.T) {
	vector := newTestStringVector(t, "seed")
	defer vector.Release()
	if hr := callSlot(t, &vector.hdr, 17, 3, 0); hr != ePointer {
		t.Fatalf("ReplaceAll(3, nil) HRESULT = %#x, want E_POINTER", hr)
	}
	if hr := callSlot(t, &vector.hdr, 17, 0, 0); hr != 0 {
		t.Fatalf("ReplaceAll(0, nil) HRESULT = %#x", hr)
	}
	if size := vectorSizeOf(t, &vector.hdr); size != 0 {
		t.Fatalf("Size after ReplaceAll(0) = %d, want 0", size)
	}
}

// TestGenericIterableAndViewObjects smoke-tests the exported generic
// constructors directly (the String wrappers exercise them constantly, but
// the scalar path has no wrapper): an IIterable<Int32> and an
// IVectorView<Int32> built from boxed uint64 payloads.
func TestGenericIterableAndViewObjects(t *testing.T) {
	codec := CodecScalar(4)
	iterable := NewIterableObject("Windows.Foundation.Collections.IIterable`1<Int32>",
		testCollectionIIDs, codec, []any{uint64(7), uint64(0xFFFFFFFF_FFFFFFFB)}) // 7, -5
	defer iterable.Release()

	var iterObj uintptr
	if hr := callSlot(t, &iterable.hdr, 6, uintptr(unsafe.Pointer(&iterObj))); hr != 0 {
		t.Fatalf("First HRESULT = %#x", hr)
	}
	iter := facetAt(t, iterObj)
	// The spawned iterator's class name derives from the iterable's.
	var className syswinrt.HSTRING
	if hr := callSlot(t, iter, 4, uintptr(unsafe.Pointer(&className))); hr != 0 {
		t.Fatalf("GetRuntimeClassName HRESULT = %#x", hr)
	}
	if got := TakeHString(className); got != "Windows.Foundation.Collections.IIterator`1<Int32>" {
		t.Errorf("iterator class = %q", got)
	}
	var got []int32
	for {
		var has byte
		if hr := callSlot(t, iter, 7, uintptr(unsafe.Pointer(&has))); hr != 0 {
			t.Fatalf("HasCurrent HRESULT = %#x", hr)
		}
		if has == 0 {
			break
		}
		var current int32
		if hr := callSlot(t, iter, 6, uintptr(unsafe.Pointer(&current))); hr != 0 {
			t.Fatalf("get_Current HRESULT = %#x", hr)
		}
		got = append(got, current)
		var more byte
		if hr := callSlot(t, iter, 8, uintptr(unsafe.Pointer(&more))); hr != 0 {
			t.Fatalf("MoveNext HRESULT = %#x", hr)
		}
	}
	if len(got) != 2 || got[0] != 7 || got[1] != -5 {
		t.Fatalf("iterated %v, want [7 -5]", got)
	}
	if refs := callSlot(t, iter, 2); refs != 0 {
		t.Fatalf("iterator Release = %d, want 0", refs)
	}

	view := NewVectorViewObject("Windows.Foundation.Collections.IVectorView`1<Int32>",
		testCollectionIIDs, codec, []any{uint64(41), uint64(42)})
	var index uint32
	var found byte
	if hr := callSlot(t, &view.hdr, 8, 42, uintptr(unsafe.Pointer(&index)), uintptr(unsafe.Pointer(&found))); hr != 0 {
		t.Fatalf("IndexOf HRESULT = %#x", hr)
	}
	if found == 0 || index != 1 {
		t.Fatalf("IndexOf(42) = %d at %d, want found at 1", found, index)
	}
	// GetMany over 4-byte slots honors the element stride.
	buffer := make([]int32, 2)
	var actual uint32
	if hr := callSlot(t, &view.hdr, 9, 0, 2, uintptr(unsafe.Pointer(&buffer[0])), uintptr(unsafe.Pointer(&actual))); hr != 0 {
		t.Fatalf("GetMany HRESULT = %#x", hr)
	}
	if actual != 2 || buffer[0] != 41 || buffer[1] != 42 {
		t.Fatalf("GetMany = %v (%d), want [41 42]", buffer, actual)
	}
	if refs := view.Release(); refs != 0 {
		t.Fatalf("view Release = %d, want 0", refs)
	}
}
