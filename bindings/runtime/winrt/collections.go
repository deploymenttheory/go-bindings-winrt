//go:build windows && (amd64 || arm64)

package winrt

import (
	"slices"
	"syscall"
	"unsafe"

	win32 "github.com/deploymenttheory/go-bindings-win32/bindings/runtime/win32"
	syswinrt "github.com/deploymenttheory/go-bindings-win32/bindings/win32/system/winrt"
)

// Go-implemented WinRT string collections: IIterable<String>,
// IIterator<String>, and IVectorView<String> objects backed by Go slices,
// for passing INTO WinRT methods that consume collections (e.g. the
// Calendar factory constructors' languages parameter). Native code drives
// them through the inspectable core (inspectable.go) plus the per-type
// method trampolines below; method slots follow the open interfaces'
// MethodDef order in Windows.Foundation.Collections.
//
// To pass an object to a generated binding, cast its pointer to the
// package-local monomorphized consumer type — sound because both layouts
// are a single vtable-pointer word:
//
//	iterable := winrt.NewStringIterable(languages)
//	calendar, err := globalization.CreateCalendarDefaultCalendarAndClock(
//		(*globalization.IIterableOfString)(unsafe.Pointer(iterable)))
//	iterable.Release()

// Pinterface-derived IIDs for the String instantiations, hard-coded here
// and pinned against a fresh derivation by internal/verify (a drifted
// constant fails there, not in a corrupted live call).
var (
	// IIDIterableOfString is Windows.Foundation.Collections.IIterable`1<String>.
	IIDIterableOfString = win32.GUID{Data1: 0xe2fcc7c1, Data2: 0x3bfc, Data3: 0x5a0b, Data4: [8]byte{0xb2, 0xb0, 0x72, 0xe7, 0x69, 0xd1, 0xcb, 0x7e}}
	// IIDIteratorOfString is Windows.Foundation.Collections.IIterator`1<String>.
	IIDIteratorOfString = win32.GUID{Data1: 0x8c304ebb, Data2: 0x6615, Data3: 0x50a4, Data4: [8]byte{0x88, 0x29, 0x87, 0x9e, 0xcd, 0x44, 0x32, 0x36}}
	// IIDVectorViewOfString is Windows.Foundation.Collections.IVectorView`1<String>.
	IIDVectorViewOfString = win32.GUID{Data1: 0x2f13c006, Data2: 0xa03a, Data3: 0x5f69, Data4: [8]byte{0xb0, 0x90, 0x75, 0xa4, 0x3e, 0x33, 0x42, 0x3e}}
)

// One immutable vtable per type (per facet for the vector view), shared
// across instances: the six IInspectable slots then the interface's methods
// in MethodDef order.
var (
	stringIterableVtbl = [7]uintptr{
		inspectableCallbackQI, inspectableCallbackAddRef, inspectableCallbackRelease,
		inspectableCallbackGetIids, inspectableCallbackGetRuntimeClassName, inspectableCallbackGetTrustLevel,
		syscall.NewCallback(stringIterableFirst), // slot 6: First
	}
	stringIteratorVtbl = [10]uintptr{
		inspectableCallbackQI, inspectableCallbackAddRef, inspectableCallbackRelease,
		inspectableCallbackGetIids, inspectableCallbackGetRuntimeClassName, inspectableCallbackGetTrustLevel,
		syscall.NewCallback(stringIteratorCurrent),    // slot 6: get_Current
		syscall.NewCallback(stringIteratorHasCurrent), // slot 7: get_HasCurrent
		syscall.NewCallback(stringIteratorMoveNext),   // slot 8: MoveNext
		syscall.NewCallback(stringIteratorGetMany),    // slot 9: GetMany
	}
	stringVectorViewVtbl = [10]uintptr{
		inspectableCallbackQI, inspectableCallbackAddRef, inspectableCallbackRelease,
		inspectableCallbackGetIids, inspectableCallbackGetRuntimeClassName, inspectableCallbackGetTrustLevel,
		syscall.NewCallback(stringVectorViewGetAt),   // slot 6: GetAt
		syscall.NewCallback(stringVectorViewSize),    // slot 7: get_Size
		syscall.NewCallback(stringVectorViewIndexOf), // slot 8: IndexOf
		syscall.NewCallback(stringVectorViewGetMany), // slot 9: GetMany
	}
	stringVectorViewIterableVtbl = [7]uintptr{
		inspectableCallbackQI, inspectableCallbackAddRef, inspectableCallbackRelease,
		inspectableCallbackGetIids, inspectableCallbackGetRuntimeClassName, inspectableCallbackGetTrustLevel,
		syscall.NewCallback(stringVectorViewFirst), // slot 6: First (tear-off facet)
	}
)

// StringIterable is a Go-implemented IIterable<String> over a copy of a Go
// slice. Instances are pinned in the package registry from NewStringIterable
// until their reference count reaches zero, so they stay reachable while
// native code holds them.
type StringIterable struct {
	hdr   inspectable // must be the first field: `this` == the object pointer
	items []string
}

// NewStringIterable creates an IIterable<String> over a copy of items. The
// object starts with one reference owned by the caller; Release it when no
// native code can still hold it.
func NewStringIterable(items []string) *StringIterable {
	obj := &StringIterable{items: slices.Clone(items)}
	obj.hdr.lpVtbl = &stringIterableVtbl[0]
	obj.hdr.iids = []win32.GUID{IIDIterableOfString}
	initInspectable("Windows.Foundation.Collections.IIterable`1<String>", &obj.hdr)
	return obj
}

// Ptr is the COM object pointer to pass where an IIterable<String> is
// expected.
func (o *StringIterable) Ptr() uintptr { return uintptr(unsafe.Pointer(o)) }

// Release drops the caller's reference (the native side holds its own).
func (o *StringIterable) Release() uint32 { return uint32(inspectableRelease(&o.hdr)) }

// stringIterableFirst is IIterable<String>.First (slot 6): a NEW iterator
// over the iterable's items, its constructor reference transferred to the
// native caller.
func stringIterableFirst(this *inspectable, out *uintptr) uintptr {
	return dispatchInspectable(inspectableWork{fn: stringIterableFirstBody, this: this, p0: unsafe.Pointer(out)})
}

func stringIterableFirstBody(w *inspectableWork) uintptr {
	this, out := w.this, (*uintptr)(w.p0)
	if out == nil {
		return ePointer
	}
	*out = 0
	if !registeredInspectable(this) {
		return eFail
	}
	iterable := (*StringIterable)(unsafe.Pointer(this.identity))
	*out = NewStringIterator(iterable.items).Ptr()
	return 0
}

// StringIterator is a Go-implemented IIterator<String> over a copy of a Go
// slice. Position tracking is a plain field: WinRT iterators are
// single-consumer by contract, so no cross-thread mutation occurs.
type StringIterator struct {
	hdr   inspectable // must be the first field: `this` == the object pointer
	items []string
	pos   int
}

// NewStringIterator creates an IIterator<String> over a copy of items,
// positioned at the first item. The object starts with one reference owned
// by the caller (First transfers it to the native consumer).
func NewStringIterator(items []string) *StringIterator {
	obj := &StringIterator{items: slices.Clone(items)}
	obj.hdr.lpVtbl = &stringIteratorVtbl[0]
	obj.hdr.iids = []win32.GUID{IIDIteratorOfString}
	initInspectable("Windows.Foundation.Collections.IIterator`1<String>", &obj.hdr)
	return obj
}

// Ptr is the COM object pointer to pass where an IIterator<String> is
// expected.
func (o *StringIterator) Ptr() uintptr { return uintptr(unsafe.Pointer(o)) }

// Release drops the caller's reference (the native side holds its own).
func (o *StringIterator) Release() uint32 { return uint32(inspectableRelease(&o.hdr)) }

// stringIteratorCurrent is IIterator<String>.get_Current (slot 6): a NEW
// HSTRING the caller owns, or E_BOUNDS when the iterator is exhausted.
func stringIteratorCurrent(this *inspectable, out *syswinrt.HSTRING) uintptr {
	return dispatchInspectable(inspectableWork{fn: stringIteratorCurrentBody, this: this, p0: unsafe.Pointer(out)})
}

func stringIteratorCurrentBody(w *inspectableWork) uintptr {
	this, out := w.this, (*syswinrt.HSTRING)(w.p0)
	if out == nil {
		return ePointer
	}
	*out = 0
	if !registeredInspectable(this) {
		return eFail
	}
	it := (*StringIterator)(unsafe.Pointer(this.identity))
	if it.pos >= len(it.items) {
		return eBounds
	}
	h, err := NewHString(it.items[it.pos])
	if err != nil {
		return eFail
	}
	*out = h.Raw() // ownership passes to the caller; do not Close
	return 0
}

// stringIteratorHasCurrent is IIterator<String>.get_HasCurrent (slot 7).
func stringIteratorHasCurrent(this *inspectable, out *byte) uintptr {
	return dispatchInspectable(inspectableWork{fn: stringIteratorHasCurrentBody, this: this, p0: unsafe.Pointer(out)})
}

func stringIteratorHasCurrentBody(w *inspectableWork) uintptr {
	this, out := w.this, (*byte)(w.p0)
	if out == nil {
		return ePointer
	}
	*out = 0
	if !registeredInspectable(this) {
		return eFail
	}
	it := (*StringIterator)(unsafe.Pointer(this.identity))
	*out = boolByte(it.pos < len(it.items))
	return 0
}

// stringIteratorMoveNext is IIterator<String>.MoveNext (slot 8): advance,
// then report HasCurrent. Moving an already-exhausted iterator fails with
// E_BOUNDS, per the documented IIterator contract (well-behaved consumers
// stop at the first false).
func stringIteratorMoveNext(this *inspectable, out *byte) uintptr {
	return dispatchInspectable(inspectableWork{fn: stringIteratorMoveNextBody, this: this, p0: unsafe.Pointer(out)})
}

func stringIteratorMoveNextBody(w *inspectableWork) uintptr {
	this, out := w.this, (*byte)(w.p0)
	if out == nil {
		return ePointer
	}
	*out = 0
	if !registeredInspectable(this) {
		return eFail
	}
	it := (*StringIterator)(unsafe.Pointer(this.identity))
	if it.pos >= len(it.items) {
		return eBounds
	}
	it.pos++
	*out = boolByte(it.pos < len(it.items))
	return 0
}

// stringIteratorGetMany is IIterator<String>.GetMany (slot 9): write up to
// capacity NEW HSTRINGs (caller owns each) into the caller's buffer starting
// at the current position, advance past them, and report the count written.
func stringIteratorGetMany(this *inspectable, capacity uintptr, items *syswinrt.HSTRING, actual *uint32) uintptr {
	return dispatchInspectable(inspectableWork{
		fn: stringIteratorGetManyBody, this: this,
		u0: capacity, p0: unsafe.Pointer(items), p1: unsafe.Pointer(actual),
	})
}

func stringIteratorGetManyBody(w *inspectableWork) uintptr {
	this, capacity, items, actual := w.this, w.u0, (*syswinrt.HSTRING)(w.p0), (*uint32)(w.p1)
	if actual == nil {
		return ePointer
	}
	*actual = 0
	if !registeredInspectable(this) {
		return eFail
	}
	it := (*StringIterator)(unsafe.Pointer(this.identity))
	n := min(int(uint32(capacity)), len(it.items)-it.pos)
	if n <= 0 {
		return 0
	}
	if items == nil {
		return ePointer
	}
	buffer := unsafe.Slice(items, n)
	if hr := fillHStrings(buffer, it.items[it.pos:it.pos+n]); hr != 0 {
		return hr
	}
	it.pos += n
	*actual = uint32(n)
	return 0
}

// StringVectorView is a Go-implemented IVectorView<String> over a copy of a
// Go slice. It answers QueryInterface for IVectorView<String> (the identity
// facet) AND IIterable<String> (a tear-off facet inside the same allocation
// — the two interfaces have different method slots, so each needs its own
// vtable word); both share one reference count and one COM identity.
type StringVectorView struct {
	hdr      inspectable // must be the first field: identity facet (IVectorView<String>)
	iterable inspectable // tear-off facet (IIterable<String>)
	items    []string
}

// NewStringVectorView creates an IVectorView<String> (also iterable) over a
// copy of items. The object starts with one reference owned by the caller;
// Release it when no native code can still hold it.
func NewStringVectorView(items []string) *StringVectorView {
	obj := &StringVectorView{items: slices.Clone(items)}
	obj.hdr.lpVtbl = &stringVectorViewVtbl[0]
	obj.hdr.iids = []win32.GUID{IIDVectorViewOfString}
	obj.iterable.lpVtbl = &stringVectorViewIterableVtbl[0]
	obj.iterable.iids = []win32.GUID{IIDIterableOfString}
	initInspectable("Windows.Foundation.Collections.IVectorView`1<String>", &obj.hdr, &obj.iterable)
	return obj
}

// Ptr is the COM object pointer to pass where an IVectorView<String> is
// expected. Where an IIterable<String> is expected, QueryInterface for
// IIDIterableOfString instead (the iterable facet has its own vtable).
func (o *StringVectorView) Ptr() uintptr { return uintptr(unsafe.Pointer(o)) }

// Release drops the caller's reference (the native side holds its own).
func (o *StringVectorView) Release() uint32 { return uint32(inspectableRelease(&o.hdr)) }

// stringVectorViewGetAt is IVectorView<String>.GetAt (slot 6): a NEW HSTRING
// the caller owns, or E_BOUNDS past the end.
func stringVectorViewGetAt(this *inspectable, index uintptr, out *syswinrt.HSTRING) uintptr {
	return dispatchInspectable(inspectableWork{
		fn: stringVectorViewGetAtBody, this: this, u0: index, p0: unsafe.Pointer(out),
	})
}

func stringVectorViewGetAtBody(w *inspectableWork) uintptr {
	this, index, out := w.this, w.u0, (*syswinrt.HSTRING)(w.p0)
	if out == nil {
		return ePointer
	}
	*out = 0
	if !registeredInspectable(this) {
		return eFail
	}
	view := (*StringVectorView)(unsafe.Pointer(this.identity))
	i := int(uint32(index))
	if i >= len(view.items) {
		return eBounds
	}
	h, err := NewHString(view.items[i])
	if err != nil {
		return eFail
	}
	*out = h.Raw() // ownership passes to the caller; do not Close
	return 0
}

// stringVectorViewSize is IVectorView<String>.get_Size (slot 7).
func stringVectorViewSize(this *inspectable, out *uint32) uintptr {
	return dispatchInspectable(inspectableWork{fn: stringVectorViewSizeBody, this: this, p0: unsafe.Pointer(out)})
}

func stringVectorViewSizeBody(w *inspectableWork) uintptr {
	this, out := w.this, (*uint32)(w.p0)
	if out == nil {
		return ePointer
	}
	*out = 0
	if !registeredInspectable(this) {
		return eFail
	}
	view := (*StringVectorView)(unsafe.Pointer(this.identity))
	*out = uint32(len(view.items))
	return 0
}

// stringVectorViewIndexOf is IVectorView<String>.IndexOf (slot 8): the first
// index whose value equals the BORROWED input HSTRING (read, not consumed).
func stringVectorViewIndexOf(this *inspectable, value syswinrt.HSTRING, index *uint32, found *byte) uintptr {
	return dispatchInspectable(inspectableWork{
		fn: stringVectorViewIndexOfBody, this: this,
		u0: uintptr(value), p0: unsafe.Pointer(index), p1: unsafe.Pointer(found),
	})
}

func stringVectorViewIndexOfBody(w *inspectableWork) uintptr {
	this, value, index, found := w.this, syswinrt.HSTRING(w.u0), (*uint32)(w.p0), (*byte)(w.p1)
	if index == nil || found == nil {
		return ePointer
	}
	*index = 0
	*found = 0
	if !registeredInspectable(this) {
		return eFail
	}
	view := (*StringVectorView)(unsafe.Pointer(this.identity))
	needle := HStringToString(value)
	for i, item := range view.items {
		if item == needle {
			*index = uint32(i)
			*found = 1
			return 0
		}
	}
	return 0
}

// stringVectorViewGetMany is IVectorView<String>.GetMany (slot 9): write up
// to capacity NEW HSTRINGs (caller owns each) starting at startIndex and
// report the count written (0 at or past the end).
func stringVectorViewGetMany(this *inspectable, startIndex uintptr, capacity uintptr, items *syswinrt.HSTRING, actual *uint32) uintptr {
	return dispatchInspectable(inspectableWork{
		fn: stringVectorViewGetManyBody, this: this,
		u0: startIndex, u1: capacity, p0: unsafe.Pointer(items), p1: unsafe.Pointer(actual),
	})
}

func stringVectorViewGetManyBody(w *inspectableWork) uintptr {
	this, startIndex, capacity := w.this, w.u0, w.u1
	items, actual := (*syswinrt.HSTRING)(w.p0), (*uint32)(w.p1)
	if actual == nil {
		return ePointer
	}
	*actual = 0
	if !registeredInspectable(this) {
		return eFail
	}
	view := (*StringVectorView)(unsafe.Pointer(this.identity))
	start := int(uint32(startIndex))
	n := min(int(uint32(capacity)), len(view.items)-start)
	if n <= 0 {
		return 0
	}
	if items == nil {
		return ePointer
	}
	buffer := unsafe.Slice(items, n)
	if hr := fillHStrings(buffer, view.items[start:start+n]); hr != 0 {
		return hr
	}
	*actual = uint32(n)
	return 0
}

// stringVectorViewFirst is the tear-off facet's IIterable<String>.First
// (slot 6): a NEW iterator over the view's items, its constructor reference
// transferred to the native caller.
func stringVectorViewFirst(this *inspectable, out *uintptr) uintptr {
	return dispatchInspectable(inspectableWork{fn: stringVectorViewFirstBody, this: this, p0: unsafe.Pointer(out)})
}

func stringVectorViewFirstBody(w *inspectableWork) uintptr {
	this, out := w.this, (*uintptr)(w.p0)
	if out == nil {
		return ePointer
	}
	*out = 0
	if !registeredInspectable(this) {
		return eFail
	}
	view := (*StringVectorView)(unsafe.Pointer(this.identity))
	*out = NewStringIterator(view.items).Ptr()
	return 0
}

// fillHStrings creates one NEW HSTRING per source string into the caller's
// buffer, unwinding everything already written on failure.
func fillHStrings(buffer []syswinrt.HSTRING, source []string) uintptr {
	for i, s := range source {
		h, err := NewHString(s)
		if err != nil {
			for j := range i {
				_ = syswinrt.WindowsDeleteString(buffer[j])
				buffer[j] = 0
			}
			return eFail
		}
		buffer[i] = h.Raw() // ownership passes to the caller; do not Close
	}
	return 0
}

func boolByte(b bool) byte {
	if b {
		return 1
	}
	return 0
}
