//go:build windows && (amd64 || arm64)

package winrt

import (
	win32 "github.com/deploymenttheory/go-bindings-win32/bindings/runtime/win32"
)

// Go-implemented WinRT string collections: IIterable<String>,
// IIterator<String>, and IVectorView<String> objects backed by Go slices,
// for passing INTO WinRT methods that consume collections (e.g. the
// Calendar factory constructors' languages parameter). Since the
// element-generic wave these are thin wrappers over the collections_core.go
// types wired to CodecString — the exported API, the hard-coded IIDs, and
// the semantics (copied items, Go string equality for IndexOf, E_BOUNDS
// contracts) are unchanged. Native code drives them through the inspectable
// core (inspectable.go) plus the shared per-shape method trampolines in
// collections_core.go; method slots follow the open interfaces' MethodDef
// order in Windows.Foundation.Collections.
//
// To pass an object to a generated binding, cast its pointer to the
// package-local monomorphized consumer type — sound because both layouts
// are a single vtable-pointer word:
//
//	iterable := winrt.NewStringIterable(languages)
//	calendar, err := globalization.CreateCalendarDefaultCalendarAndClock(
//		(*globalization.IIterableOfString)(unsafe.Pointer(iterable)))
//	iterable.Release()
//
// (Generated packages whose pinterfaces ground a string collection also
// carry a typed constructor — globalization.NewIIterableOfString — built on
// the same core.)

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

// stringCollectionIIDs wires the String instantiations' IID set (no
// IVector<String> constant is exported here; the generated
// New<IVectorOfString> constructors carry their own derived IID).
var stringCollectionIIDs = CollectionIIDs{
	Iterable:   IIDIterableOfString,
	Iterator:   IIDIteratorOfString,
	VectorView: IIDVectorViewOfString,
}

// boxStrings boxes a string slice into the core payload representation
// (which also snapshots the source: mutating it afterwards cannot leak in).
func boxStrings(items []string) []any {
	boxed := make([]any, len(items))
	for i, item := range items {
		boxed[i] = item
	}
	return boxed
}

// StringIterable is a Go-implemented IIterable<String> over a copy of a Go
// slice. Instances are pinned in the package registry from NewStringIterable
// until their reference count reaches zero, so they stay reachable while
// native code holds them.
type StringIterable struct {
	Iterable // must be the first field: `this` == the object pointer
}

// NewStringIterable creates an IIterable<String> over a copy of items. The
// object starts with one reference owned by the caller; Release it when no
// native code can still hold it.
func NewStringIterable(items []string) *StringIterable {
	obj := &StringIterable{}
	obj.initIterable("Windows.Foundation.Collections.IIterable`1<String>",
		stringCollectionIIDs, CodecString, boxStrings(items))
	return obj
}

// StringIterator is a Go-implemented IIterator<String> over a copy of a Go
// slice. Position tracking is a plain field: WinRT iterators are
// single-consumer by contract, so no cross-thread mutation occurs.
type StringIterator struct {
	Iterator // must be the first field: `this` == the object pointer
}

// NewStringIterator creates an IIterator<String> over a copy of items,
// positioned at the first item. The object starts with one reference owned
// by the caller (First transfers it to the native consumer).
func NewStringIterator(items []string) *StringIterator {
	obj := &StringIterator{}
	obj.initIterator("Windows.Foundation.Collections.IIterator`1<String>",
		stringCollectionIIDs, CodecString, boxStrings(items))
	return obj
}

// StringVectorView is a Go-implemented IVectorView<String> over a copy of a
// Go slice. It answers QueryInterface for IVectorView<String> (the identity
// facet) AND IIterable<String> (a tear-off facet inside the same allocation
// — the two interfaces have different method slots, so each needs its own
// vtable word); both share one reference count and one COM identity.
type StringVectorView struct {
	VectorView // must be the first field: `this` == the object pointer
}

// NewStringVectorView creates an IVectorView<String> (also iterable) over a
// copy of items. The object starts with one reference owned by the caller;
// Release it when no native code can still hold it.
func NewStringVectorView(items []string) *StringVectorView {
	obj := &StringVectorView{}
	obj.initVectorView("Windows.Foundation.Collections.IVectorView`1<String>",
		stringCollectionIIDs, CodecString, boxStrings(items))
	return obj
}

func boolByte(b bool) byte {
	if b {
		return 1
	}
	return 0
}
