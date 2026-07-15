//go:build windows && (amd64 || arm64)

package winrt

import (
	"fmt"
	"slices"
	"strings"
	"syscall"
	"unsafe"

	win32 "github.com/deploymenttheory/go-bindings-win32/bindings/runtime/win32"
	syswinrt "github.com/deploymenttheory/go-bindings-win32/bindings/win32/system/winrt"
)

// Element-generic Go-implemented WinRT collections: IIterable<T>,
// IIterator<T>, IVectorView<T>, and the writable IVector<T>, backed by Go
// slices and driven by native code through the inspectable core
// (inspectable.go). The element type is NOT a Go type parameter: IIDs are
// derivation-time knowledge and syscall.NewCallback allocations are
// process-permanent, so generic trampolines would multiply per element type.
// Instead the core types carry a `[]any` payload plus an ElementCodec that
// owns every element-shaped decision (ABI slot size, marshal/unmarshal,
// equality, ownership), and ONE shared trampoline per (interface shape,
// slot) dispatches through it — a fixed set of callbacks, never per element
// type.
//
// Method slots follow the open interfaces' MethodDef order in
// Windows.Foundation.Collections (IIterable`1 slot 6 = First; IIterator`1
// 6-9; IVectorView`1 6-9; IVector`1 6-17).
//
// Thread-safety: every ABI entry point serializes through the inspectable
// worker (one body at a time, see inspectable.go), and the Go side exposes
// no mutation API — the payload is only ever touched by worker bodies (or
// their inline nested reentries on the worker thread), so IVector's
// mutations need no further locking.
//
// Constructed objects start with one caller-owned reference; Release it
// once no native code can still hold the object. Generated per-instantiation
// constructors (New<IIterableOfX> etc. in <pkg>_pinterfaces.go) box typed
// slices into the payload, select the codec, and wire the derived IIDs.

// ElementCodec is one element type's ABI strategy. Payload elements are the
// codec's OWN retained representation (Go string for HSTRING elements, a
// raw retained interface-pointer word for interface elements, uint64 for
// scalars/enums, win32.GUID for GUIDs).
//
// Ownership contract:
//   - MarshalOut writes one retained ABI representation the CALLER owns
//     (a new HSTRING, an AddRef'd pointer) into the out slot.
//   - FreeOut releases a representation MarshalOut wrote (unwind paths).
//   - UnmarshalWord/UnmarshalSlot take a BORROWED raw word / ABI slot and
//     return a RETAINED payload element (copying or AddRef'ing as needed).
//   - Release drops a retained payload element.
type ElementCodec interface {
	// ABISize is the element's size in an ABI array slot (GetMany buffers,
	// ReplaceAll input arrays).
	ABISize() uintptr
	// MarshalOut writes elem's ABI representation to out (an ABISize slot).
	// Returns an HRESULT; on 0 the ownership of the written representation
	// transfers to the caller.
	MarshalOut(elem any, out unsafe.Pointer) uintptr
	// FreeOut releases the representation a successful MarshalOut wrote at
	// out and zeroes the slot.
	FreeOut(out unsafe.Pointer)
	// UnmarshalWord converts one borrowed raw ABI word (a register-passed
	// input value: SetAt/InsertAt/Append/IndexOf) into a retained payload
	// element. For 16-byte elements (GUID) the word is a pointer to the
	// value, per the amd64 ABI.
	UnmarshalWord(raw uintptr) (any, uintptr)
	// UnmarshalSlot converts one borrowed ABISize array slot (ReplaceAll)
	// into a retained payload element.
	UnmarshalSlot(slot unsafe.Pointer) (any, uintptr)
	// EqualWord reports whether a payload element equals a borrowed raw
	// input word (IndexOf).
	EqualWord(elem any, raw uintptr) bool
	// Release drops one retained payload element.
	Release(elem any)
}

// CollectionIIDs carries the pinterface-derived IIDs a collection object
// answers QueryInterface with. Constructors use the subset their shape
// needs: Iterable + Iterator for NewIterableObject; those plus VectorView
// for NewVectorViewObject; all four for NewVectorObject (GetView spawns
// snapshot views, First spawns iterators).
type CollectionIIDs struct {
	Iterable, Iterator, VectorView, Vector win32.GUID
}

// ---------------------------------------------------------------------------
// Codecs

// CodecString is the ElementCodec for HSTRING elements. Payload elements are
// Go strings; MarshalOut creates a NEW HSTRING the caller owns, unmarshal
// reads borrowed handles without consuming them, equality is Go string
// equality (preserving the original string-collection semantics).
var CodecString ElementCodec = codecString{}

type codecString struct{}

func (codecString) ABISize() uintptr { return unsafe.Sizeof(syswinrt.HSTRING(0)) }

func (codecString) MarshalOut(elem any, out unsafe.Pointer) uintptr {
	h, err := NewHString(elem.(string))
	if err != nil {
		return eFail
	}
	*(*syswinrt.HSTRING)(out) = h.Raw() // ownership passes to the caller; do not Close
	return 0
}

func (codecString) FreeOut(out unsafe.Pointer) {
	slot := (*syswinrt.HSTRING)(out)
	if *slot != 0 {
		_ = syswinrt.WindowsDeleteString(*slot)
		*slot = 0
	}
}

func (codecString) UnmarshalWord(raw uintptr) (any, uintptr) {
	return HStringToString(syswinrt.HSTRING(raw)), 0
}

func (codecString) UnmarshalSlot(slot unsafe.Pointer) (any, uintptr) {
	return HStringToString(*(*syswinrt.HSTRING)(slot)), 0
}

func (codecString) EqualWord(elem any, raw uintptr) bool {
	return elem.(string) == HStringToString(syswinrt.HSTRING(raw))
}

func (codecString) Release(any) {}

// CodecInterface is the ElementCodec for interface-pointer elements
// (interfaces, runtime classes projected as their default interface, and
// Object). Payload elements are raw retained pointer words (uintptr);
// retention is AddRef through the element's own vtable (slot 1), release is
// slot 2. Null elements are legal and cross as 0 without ref traffic.
//
// LIMITATION (documented, by design): EqualWord — and therefore IndexOf on
// Go-implemented collections — compares COM identity WORDS only. No
// QueryInterface(IUnknown) is issued from collection bodies, so an element
// compares equal only to the exact pointer it was constructed from; a
// different interface pointer onto the same COM object does NOT match.
var CodecInterface ElementCodec = codecInterface{}

type codecInterface struct{}

func (codecInterface) ABISize() uintptr { return unsafe.Sizeof(uintptr(0)) }

func (codecInterface) MarshalOut(elem any, out unsafe.Pointer) uintptr {
	word := elem.(uintptr)
	if word != 0 {
		comAddRef(word)
	}
	*(*uintptr)(out) = word
	return 0
}

func (codecInterface) FreeOut(out unsafe.Pointer) {
	slot := (*uintptr)(out)
	if *slot != 0 {
		comRelease(*slot)
		*slot = 0
	}
}

func (codecInterface) UnmarshalWord(raw uintptr) (any, uintptr) {
	if raw != 0 {
		comAddRef(raw)
	}
	return raw, 0
}

func (codecInterface) UnmarshalSlot(slot unsafe.Pointer) (any, uintptr) {
	return codecInterface{}.UnmarshalWord(*(*uintptr)(slot))
}

func (codecInterface) EqualWord(elem any, raw uintptr) bool {
	return elem.(uintptr) == raw
}

func (codecInterface) Release(elem any) {
	if word := elem.(uintptr); word != 0 {
		comRelease(word)
	}
}

// wordPointer reinterprets a raw ABI word as a pointer. The word is always
// a NATIVE address (a COM object the caller passed, a by-reference GUID) —
// never a Go pointer — so the reinterpretation cannot hide a GC-movable
// referent; reading the bits back out of a uintptr local also keeps vet's
// unsafeptr heuristic satisfied where a direct uintptr → unsafe.Pointer
// conversion would (rightly, in general) be flagged.
func wordPointer(word uintptr) unsafe.Pointer {
	return *(*unsafe.Pointer)(unsafe.Pointer(&word))
}

// comAddRef / comRelease drive IUnknown slots 1/2 on a raw object word.
// Called from worker bodies (and constructors): when the element is itself
// Go-implemented, the call reenters a trampoline on the worker thread and
// dispatchInspectable runs it inline (see inspectable.go). No pointers into
// any Go stack cross these calls.
func comAddRef(obj uintptr) {
	vtbl := *(**[3]uintptr)(wordPointer(obj))
	_, _, _ = syscall.SyscallN(vtbl[1], obj)
}

func comRelease(obj uintptr) {
	vtbl := *(**[3]uintptr)(wordPointer(obj))
	_, _, _ = syscall.SyscallN(vtbl[2], obj)
}

// CodecScalar returns the ElementCodec for integer-scalar and enum elements
// of the given ABI size (1, 2, 4, or 8 bytes). Payload elements are uint64;
// comparisons and slot traffic mask to the element width, so sign-extended
// boxing (uint64(int32(-5))) compares correctly against zero-extended raw
// words. Panics on an unsupported size — constructors pass literal sizes,
// so this is a generation-time bug, never a data-dependent one.
func CodecScalar(size uintptr) ElementCodec {
	switch size {
	case 1, 2, 4, 8:
		return codecScalar{size: size}
	}
	panic(fmt.Sprintf("winrt: CodecScalar(%d): unsupported element size", size))
}

type codecScalar struct{ size uintptr }

func (c codecScalar) mask(v uint64) uint64 {
	if c.size == 8 {
		return v
	}
	return v & (1<<(8*c.size) - 1)
}

func (c codecScalar) ABISize() uintptr { return c.size }

func (c codecScalar) MarshalOut(elem any, out unsafe.Pointer) uintptr {
	value := elem.(uint64)
	switch c.size {
	case 1:
		*(*uint8)(out) = uint8(value)
	case 2:
		*(*uint16)(out) = uint16(value)
	case 4:
		*(*uint32)(out) = uint32(value)
	case 8:
		*(*uint64)(out) = value
	}
	return 0
}

func (codecScalar) FreeOut(unsafe.Pointer) {}

func (c codecScalar) UnmarshalWord(raw uintptr) (any, uintptr) {
	return c.mask(uint64(raw)), 0
}

func (c codecScalar) UnmarshalSlot(slot unsafe.Pointer) (any, uintptr) {
	switch c.size {
	case 1:
		return uint64(*(*uint8)(slot)), 0
	case 2:
		return uint64(*(*uint16)(slot)), 0
	case 4:
		return uint64(*(*uint32)(slot)), 0
	}
	return *(*uint64)(slot), 0
}

func (c codecScalar) EqualWord(elem any, raw uintptr) bool {
	return c.mask(elem.(uint64)) == c.mask(uint64(raw))
}

func (codecScalar) Release(any) {}

// CodecGuid is the ElementCodec for GUID elements. Payload elements are
// win32.GUID values; array slots are 16 bytes wide, and register-passed
// input words (IndexOf/SetAt/InsertAt/Append values) are POINTERS to the
// GUID, per the amd64 by-value-composite ABI.
//
// arm64 caveat: the arm64 calling convention passes a 16-byte composite in
// two registers instead, which the shared word-shaped trampolines cannot
// see — GUID collections' value-taking slots are amd64-only, mirroring the
// generated consumer side (which skips by-value GUID parameters outright).
// Slot-shaped traffic (GetAt, get_Current, GetMany, ReplaceAll) is
// ABI-portable.
var CodecGuid ElementCodec = codecGuid{}

type codecGuid struct{}

func (codecGuid) ABISize() uintptr { return unsafe.Sizeof(win32.GUID{}) }

func (codecGuid) MarshalOut(elem any, out unsafe.Pointer) uintptr {
	*(*win32.GUID)(out) = elem.(win32.GUID)
	return 0
}

func (codecGuid) FreeOut(unsafe.Pointer) {}

func (codecGuid) UnmarshalWord(raw uintptr) (any, uintptr) {
	if raw == 0 {
		return nil, ePointer
	}
	return *(*win32.GUID)(wordPointer(raw)), 0
}

func (codecGuid) UnmarshalSlot(slot unsafe.Pointer) (any, uintptr) {
	return *(*win32.GUID)(slot), 0
}

func (codecGuid) EqualWord(elem any, raw uintptr) bool {
	return raw != 0 && elem.(win32.GUID) == *(*win32.GUID)(wordPointer(raw))
}

func (codecGuid) Release(any) {}

// retainElement takes a BORROWED payload element and returns a RETAINED one.
// Value payloads (string, uint64, win32.GUID) are self-contained; a raw
// pointer-word payload (uintptr — CodecInterface) gains a reference through
// the codec's UnmarshalWord, whose borrowed-word → retained-element contract
// is exactly retention (and is infallible for word payloads).
func retainElement(codec ElementCodec, elem any) any {
	if word, ok := elem.(uintptr); ok {
		retained, _ := codec.UnmarshalWord(word)
		return retained
	}
	return elem
}

// retainAll copies items into a fresh retained payload slice. Constructors
// run it on the caller's goroutine (an element AddRef from there dispatches
// to the worker normally); First/GetView bodies run it on the worker, where
// a Go-implemented element's AddRef reenters inline.
func retainAll(codec ElementCodec, items []any) []any {
	retained := make([]any, len(items))
	for i, item := range items {
		retained[i] = retainElement(codec, item)
	}
	return retained
}

// ---------------------------------------------------------------------------
// Shared collection state

// collection is the payload every core collection type carries: the codec,
// the retained elements, the IID set (for spawning iterators and snapshot
// views), and the runtime class name (its element display substring names
// the spawned objects).
type collection struct {
	codec ElementCodec
	iids  CollectionIIDs
	class string
	items []any
}

// releaseAll releases every retained element (the inspectable destroy hook).
func (c *collection) releaseAll() {
	items := c.items
	c.items = nil
	for _, elem := range items {
		c.codec.Release(elem)
	}
}

// first spawns a NEW iterator over a retained snapshot of the current items;
// its constructor reference transfers to the native caller. A snapshot keeps
// the iterator valid however the source is later mutated or released — the
// iterator owns its own element references.
func (c *collection) first() *Iterator {
	return newIteratorObject(collectionSpawnClass(c.class, "IIterator`1"), c.iids, c.codec, c.items)
}

// getAt marshals items[index] into out, E_BOUNDS past the end.
func (c *collection) getAt(index uintptr, out unsafe.Pointer) uintptr {
	i := int(uint32(index))
	if i >= len(c.items) {
		return eBounds
	}
	return c.codec.MarshalOut(c.items[i], out)
}

// indexOf finds the first element equal to the borrowed raw input word.
func (c *collection) indexOf(raw uintptr, index *uint32, found *byte) uintptr {
	*index = 0
	*found = 0
	for i, elem := range c.items {
		if c.codec.EqualWord(elem, raw) {
			*index = uint32(i)
			*found = 1
			return 0
		}
	}
	return 0
}

// marshalRange writes up to capacity retained ABI representations (caller
// owns each) into the caller's buffer starting at start, unwinding
// everything already written on a mid-loop failure, and reports the count
// written (0 at or past the end). The GetMany core.
func (c *collection) marshalRange(start, capacity uintptr, items unsafe.Pointer, actual *uint32) uintptr {
	*actual = 0
	from := int(uint32(start))
	n := min(int(uint32(capacity)), len(c.items)-from)
	if n <= 0 {
		return 0
	}
	if items == nil {
		return ePointer
	}
	size := c.codec.ABISize()
	for i := range n {
		slot := unsafe.Add(items, uintptr(i)*size)
		if hr := c.codec.MarshalOut(c.items[from+i], slot); hr != 0 {
			for j := range i {
				c.codec.FreeOut(unsafe.Add(items, uintptr(j)*size))
			}
			return hr
		}
	}
	*actual = uint32(n)
	return 0
}

// replaceAll unmarshals ALL count input slots into a fresh retained slice
// first (releasing the partial slice and failing all-or-nothing on a
// mid-loop error), then releases the old items and swaps.
func (c *collection) replaceAll(count uintptr, items unsafe.Pointer) uintptr {
	n := int(uint32(count))
	if n > 0 && items == nil {
		return ePointer
	}
	fresh := make([]any, 0, n)
	size := c.codec.ABISize()
	for i := range n {
		elem, hr := c.codec.UnmarshalSlot(unsafe.Add(items, uintptr(i)*size))
		if hr != 0 {
			for _, retained := range fresh {
				c.codec.Release(retained)
			}
			return hr
		}
		fresh = append(fresh, elem)
	}
	old := c.items
	c.items = fresh
	for _, elem := range old {
		c.codec.Release(elem)
	}
	return 0
}

// collectionSpawnClass derives a spawned object's runtime class name from
// its parent's: the element display inside the parent's <...> is re-wrapped
// under the given open name ("...IIterable`1<String>" + "IIterator`1" →
// "Windows.Foundation.Collections.IIterator`1<String>"). A class that does
// not carry the <element> shape is passed through unchanged.
func collectionSpawnClass(class, open string) string {
	i := strings.IndexByte(class, '<')
	if i < 0 || !strings.HasSuffix(class, ">") {
		return class
	}
	return "Windows.Foundation.Collections." + open + class[i:]
}

// ---------------------------------------------------------------------------
// Vtables: one immutable array per interface shape (per facet for the
// tear-offs), shared across instances AND across element types — the codec
// field carries every element decision, so the callback count is fixed
// (+13 over the retired per-string-type set) no matter how many element
// types instantiate.

var (
	iterableVtbl = [7]uintptr{
		inspectableCallbackQI, inspectableCallbackAddRef, inspectableCallbackRelease,
		inspectableCallbackGetIids, inspectableCallbackGetRuntimeClassName, inspectableCallbackGetTrustLevel,
		syscall.NewCallback(iterableFirst), // slot 6: First
	}
	iteratorVtbl = [10]uintptr{
		inspectableCallbackQI, inspectableCallbackAddRef, inspectableCallbackRelease,
		inspectableCallbackGetIids, inspectableCallbackGetRuntimeClassName, inspectableCallbackGetTrustLevel,
		syscall.NewCallback(iteratorCurrent),    // slot 6: get_Current
		syscall.NewCallback(iteratorHasCurrent), // slot 7: get_HasCurrent
		syscall.NewCallback(iteratorMoveNext),   // slot 8: MoveNext
		syscall.NewCallback(iteratorGetMany),    // slot 9: GetMany
	}
	vectorViewVtbl = [10]uintptr{
		inspectableCallbackQI, inspectableCallbackAddRef, inspectableCallbackRelease,
		inspectableCallbackGetIids, inspectableCallbackGetRuntimeClassName, inspectableCallbackGetTrustLevel,
		syscall.NewCallback(vectorViewGetAt),   // slot 6: GetAt
		syscall.NewCallback(vectorViewSize),    // slot 7: get_Size
		syscall.NewCallback(vectorViewIndexOf), // slot 8: IndexOf
		syscall.NewCallback(vectorViewGetMany), // slot 9: GetMany
	}
	vectorViewIterableVtbl = [7]uintptr{
		inspectableCallbackQI, inspectableCallbackAddRef, inspectableCallbackRelease,
		inspectableCallbackGetIids, inspectableCallbackGetRuntimeClassName, inspectableCallbackGetTrustLevel,
		syscall.NewCallback(vectorViewFirst), // slot 6: First (tear-off facet)
	}
	vectorVtbl = [18]uintptr{
		inspectableCallbackQI, inspectableCallbackAddRef, inspectableCallbackRelease,
		inspectableCallbackGetIids, inspectableCallbackGetRuntimeClassName, inspectableCallbackGetTrustLevel,
		syscall.NewCallback(vectorGetAt),       // slot 6: GetAt
		syscall.NewCallback(vectorSize),        // slot 7: get_Size
		syscall.NewCallback(vectorGetView),     // slot 8: GetView
		syscall.NewCallback(vectorIndexOf),     // slot 9: IndexOf
		syscall.NewCallback(vectorSetAt),       // slot 10: SetAt
		syscall.NewCallback(vectorInsertAt),    // slot 11: InsertAt
		syscall.NewCallback(vectorRemoveAt),    // slot 12: RemoveAt
		syscall.NewCallback(vectorAppend),      // slot 13: Append
		syscall.NewCallback(vectorRemoveAtEnd), // slot 14: RemoveAtEnd
		syscall.NewCallback(vectorClear),       // slot 15: Clear
		syscall.NewCallback(vectorGetMany),     // slot 16: GetMany
		syscall.NewCallback(vectorReplaceAll),  // slot 17: ReplaceAll
	}
	vectorIterableVtbl = [7]uintptr{
		inspectableCallbackQI, inspectableCallbackAddRef, inspectableCallbackRelease,
		inspectableCallbackGetIids, inspectableCallbackGetRuntimeClassName, inspectableCallbackGetTrustLevel,
		syscall.NewCallback(vectorFirst), // slot 6: First (tear-off facet)
	}
)

// ---------------------------------------------------------------------------
// Iterable

// Iterable is a Go-implemented IIterable<T> over retained copies of the
// constructor's items. Instances are pinned in the package registry from
// construction until their reference count reaches zero, so they stay
// reachable while native code holds them.
type Iterable struct {
	hdr  inspectable // must be the first field: `this` == the object pointer
	coll collection
}

// NewIterableObject creates an IIterable<element> named class (the runtime
// class display name, e.g.
// "Windows.Foundation.Collections.IIterable`1<String>") answering
// QueryInterface for iids.Iterable; First spawns iterators under
// iids.Iterator. items are BORROWED: the object retains its own element
// references through the codec (interface elements are AddRef'd) and
// releases them when its reference count reaches zero. The object starts
// with one caller-owned reference.
func NewIterableObject(class string, iids CollectionIIDs, codec ElementCodec, items []any) *Iterable {
	obj := &Iterable{}
	obj.initIterable(class, iids, codec, items)
	return obj
}

// initIterable wires an embedded Iterable in place (the address registered
// with the inspectable core must be the final object's, so wrappers embed
// and init rather than copy).
func (o *Iterable) initIterable(class string, iids CollectionIIDs, codec ElementCodec, items []any) {
	o.coll = collection{codec: codec, iids: iids, class: class, items: retainAll(codec, items)}
	o.hdr.lpVtbl = &iterableVtbl[0]
	o.hdr.iids = []win32.GUID{iids.Iterable}
	o.hdr.destroy = o.coll.releaseAll
	initInspectable(class, &o.hdr)
}

// Ptr is the COM object pointer to pass where the IIterable<T> is expected.
func (o *Iterable) Ptr() uintptr { return uintptr(unsafe.Pointer(o)) }

// Release drops the caller's reference (the native side holds its own).
func (o *Iterable) Release() uint32 { return uint32(inspectableRelease(&o.hdr)) }

// iterableFirst is IIterable<T>.First (slot 6): a NEW iterator over the
// iterable's items, its constructor reference transferred to the native
// caller.
func iterableFirst(this *inspectable, out *uintptr) uintptr {
	return dispatchInspectable(inspectableWork{fn: iterableFirstBody, this: this, p0: unsafe.Pointer(out)})
}

func iterableFirstBody(w *inspectableWork) uintptr {
	this, out := w.this, (*uintptr)(w.p0)
	if out == nil {
		return ePointer
	}
	*out = 0
	if !registeredInspectable(this) {
		return eFail
	}
	iterable := (*Iterable)(unsafe.Pointer(this.identity))
	*out = iterable.coll.first().Ptr()
	return 0
}

// ---------------------------------------------------------------------------
// Iterator

// Iterator is a Go-implemented IIterator<T> over retained copies of its
// items, positioned at the first. Position tracking is a plain field: WinRT
// iterators are single-consumer by contract, and every body is serialized
// by the worker anyway.
type Iterator struct {
	hdr  inspectable // must be the first field: `this` == the object pointer
	coll collection
	pos  int
}

// newIteratorObject creates an IIterator<element> answering iids.Iterator.
// items are borrowed and retained exactly as in NewIterableObject. The
// object starts with one reference (First transfers it to the native
// consumer). Unexported: native code obtains iterators through First;
// StringIterator keeps its exported wrapper for compatibility.
func newIteratorObject(class string, iids CollectionIIDs, codec ElementCodec, items []any) *Iterator {
	obj := &Iterator{}
	obj.initIterator(class, iids, codec, items)
	return obj
}

func (o *Iterator) initIterator(class string, iids CollectionIIDs, codec ElementCodec, items []any) {
	o.coll = collection{codec: codec, iids: iids, class: class, items: retainAll(codec, items)}
	o.hdr.lpVtbl = &iteratorVtbl[0]
	o.hdr.iids = []win32.GUID{iids.Iterator}
	o.hdr.destroy = o.coll.releaseAll
	initInspectable(class, &o.hdr)
}

// Ptr is the COM object pointer to pass where the IIterator<T> is expected.
func (o *Iterator) Ptr() uintptr { return uintptr(unsafe.Pointer(o)) }

// Release drops the caller's reference (the native side holds its own).
func (o *Iterator) Release() uint32 { return uint32(inspectableRelease(&o.hdr)) }

// iteratorCurrent is IIterator<T>.get_Current (slot 6): a NEW retained
// element representation the caller owns, or E_BOUNDS when exhausted.
func iteratorCurrent(this *inspectable, out unsafe.Pointer) uintptr {
	return dispatchInspectable(inspectableWork{fn: iteratorCurrentBody, this: this, p0: out})
}

func iteratorCurrentBody(w *inspectableWork) uintptr {
	this, out := w.this, w.p0
	if out == nil {
		return ePointer
	}
	if !registeredInspectable(this) {
		return eFail
	}
	it := (*Iterator)(unsafe.Pointer(this.identity))
	return it.coll.getAt(uintptr(it.pos), out)
}

// iteratorHasCurrent is IIterator<T>.get_HasCurrent (slot 7).
func iteratorHasCurrent(this *inspectable, out *byte) uintptr {
	return dispatchInspectable(inspectableWork{fn: iteratorHasCurrentBody, this: this, p0: unsafe.Pointer(out)})
}

func iteratorHasCurrentBody(w *inspectableWork) uintptr {
	this, out := w.this, (*byte)(w.p0)
	if out == nil {
		return ePointer
	}
	*out = 0
	if !registeredInspectable(this) {
		return eFail
	}
	it := (*Iterator)(unsafe.Pointer(this.identity))
	*out = boolByte(it.pos < len(it.coll.items))
	return 0
}

// iteratorMoveNext is IIterator<T>.MoveNext (slot 8): advance, then report
// HasCurrent. Moving an already-exhausted iterator fails with E_BOUNDS, per
// the documented IIterator contract (well-behaved consumers stop at the
// first false).
func iteratorMoveNext(this *inspectable, out *byte) uintptr {
	return dispatchInspectable(inspectableWork{fn: iteratorMoveNextBody, this: this, p0: unsafe.Pointer(out)})
}

func iteratorMoveNextBody(w *inspectableWork) uintptr {
	this, out := w.this, (*byte)(w.p0)
	if out == nil {
		return ePointer
	}
	*out = 0
	if !registeredInspectable(this) {
		return eFail
	}
	it := (*Iterator)(unsafe.Pointer(this.identity))
	if it.pos >= len(it.coll.items) {
		return eBounds
	}
	it.pos++
	*out = boolByte(it.pos < len(it.coll.items))
	return 0
}

// iteratorGetMany is IIterator<T>.GetMany (slot 9): write up to capacity NEW
// retained representations (caller owns each) into the caller's buffer
// starting at the current position, advance past them, and report the count
// written.
func iteratorGetMany(this *inspectable, capacity uintptr, items unsafe.Pointer, actual *uint32) uintptr {
	return dispatchInspectable(inspectableWork{
		fn: iteratorGetManyBody, this: this,
		u0: capacity, p0: items, p1: unsafe.Pointer(actual),
	})
}

func iteratorGetManyBody(w *inspectableWork) uintptr {
	this, capacity, items, actual := w.this, w.u0, w.p0, (*uint32)(w.p1)
	if actual == nil {
		return ePointer
	}
	*actual = 0
	if !registeredInspectable(this) {
		return eFail
	}
	it := (*Iterator)(unsafe.Pointer(this.identity))
	if hr := it.coll.marshalRange(uintptr(it.pos), capacity, items, actual); hr != 0 {
		return hr
	}
	it.pos += int(*actual)
	return 0
}

// ---------------------------------------------------------------------------
// VectorView

// VectorView is a Go-implemented IVectorView<T> over retained copies of its
// items. It answers QueryInterface for the vector-view IID (the identity
// facet) AND the iterable IID (a tear-off facet inside the same allocation
// — the two interfaces have different method slots, so each needs its own
// vtable word); both share one reference count and one COM identity.
type VectorView struct {
	hdr      inspectable // must be the first field: identity facet (IVectorView<T>)
	iterable inspectable // tear-off facet (IIterable<T>)
	coll     collection
}

// NewVectorViewObject creates an IVectorView<element> (also iterable) named
// class, answering QueryInterface for iids.VectorView and iids.Iterable;
// First spawns iterators under iids.Iterator. items are borrowed and
// retained exactly as in NewIterableObject. The object starts with one
// caller-owned reference.
func NewVectorViewObject(class string, iids CollectionIIDs, codec ElementCodec, items []any) *VectorView {
	obj := &VectorView{}
	obj.initVectorView(class, iids, codec, items)
	return obj
}

func (o *VectorView) initVectorView(class string, iids CollectionIIDs, codec ElementCodec, items []any) {
	o.coll = collection{codec: codec, iids: iids, class: class, items: retainAll(codec, items)}
	o.hdr.lpVtbl = &vectorViewVtbl[0]
	o.hdr.iids = []win32.GUID{iids.VectorView}
	o.hdr.destroy = o.coll.releaseAll
	o.iterable.lpVtbl = &vectorViewIterableVtbl[0]
	o.iterable.iids = []win32.GUID{iids.Iterable}
	initInspectable(class, &o.hdr, &o.iterable)
}

// Ptr is the COM object pointer to pass where the IVectorView<T> is
// expected. Where an IIterable<T> is expected, QueryInterface for
// iids.Iterable instead (the iterable facet has its own vtable).
func (o *VectorView) Ptr() uintptr { return uintptr(unsafe.Pointer(o)) }

// Release drops the caller's reference (the native side holds its own).
func (o *VectorView) Release() uint32 { return uint32(inspectableRelease(&o.hdr)) }

// vectorViewGetAt is IVectorView<T>.GetAt (slot 6): a NEW retained element
// representation the caller owns, or E_BOUNDS past the end.
func vectorViewGetAt(this *inspectable, index uintptr, out unsafe.Pointer) uintptr {
	return dispatchInspectable(inspectableWork{fn: vectorViewGetAtBody, this: this, u0: index, p0: out})
}

func vectorViewGetAtBody(w *inspectableWork) uintptr {
	this, index, out := w.this, w.u0, w.p0
	if out == nil {
		return ePointer
	}
	if !registeredInspectable(this) {
		return eFail
	}
	view := (*VectorView)(unsafe.Pointer(this.identity))
	return view.coll.getAt(index, out)
}

// vectorViewSize is IVectorView<T>.get_Size (slot 7).
func vectorViewSize(this *inspectable, out *uint32) uintptr {
	return dispatchInspectable(inspectableWork{fn: vectorViewSizeBody, this: this, p0: unsafe.Pointer(out)})
}

func vectorViewSizeBody(w *inspectableWork) uintptr {
	this, out := w.this, (*uint32)(w.p0)
	if out == nil {
		return ePointer
	}
	*out = 0
	if !registeredInspectable(this) {
		return eFail
	}
	view := (*VectorView)(unsafe.Pointer(this.identity))
	*out = uint32(len(view.coll.items))
	return 0
}

// vectorViewIndexOf is IVectorView<T>.IndexOf (slot 8): the first index
// whose element equals the BORROWED input word under the codec's equality
// (string value equality; interface identity-word equality — see
// CodecInterface).
func vectorViewIndexOf(this *inspectable, value uintptr, index *uint32, found *byte) uintptr {
	return dispatchInspectable(inspectableWork{
		fn: vectorViewIndexOfBody, this: this,
		u0: value, p0: unsafe.Pointer(index), p1: unsafe.Pointer(found),
	})
}

func vectorViewIndexOfBody(w *inspectableWork) uintptr {
	this, value, index, found := w.this, w.u0, (*uint32)(w.p0), (*byte)(w.p1)
	if index == nil || found == nil {
		return ePointer
	}
	*index = 0
	*found = 0
	if !registeredInspectable(this) {
		return eFail
	}
	view := (*VectorView)(unsafe.Pointer(this.identity))
	return view.coll.indexOf(value, index, found)
}

// vectorViewGetMany is IVectorView<T>.GetMany (slot 9): write up to capacity
// NEW retained representations (caller owns each) starting at startIndex and
// report the count written (0 at or past the end).
func vectorViewGetMany(this *inspectable, startIndex, capacity uintptr, items unsafe.Pointer, actual *uint32) uintptr {
	return dispatchInspectable(inspectableWork{
		fn: vectorViewGetManyBody, this: this,
		u0: startIndex, u1: capacity, p0: items, p1: unsafe.Pointer(actual),
	})
}

func vectorViewGetManyBody(w *inspectableWork) uintptr {
	this, startIndex, capacity := w.this, w.u0, w.u1
	items, actual := w.p0, (*uint32)(w.p1)
	if actual == nil {
		return ePointer
	}
	*actual = 0
	if !registeredInspectable(this) {
		return eFail
	}
	view := (*VectorView)(unsafe.Pointer(this.identity))
	return view.coll.marshalRange(startIndex, capacity, items, actual)
}

// vectorViewFirst is the tear-off facet's IIterable<T>.First (slot 6): a NEW
// iterator over the view's items, its constructor reference transferred to
// the native caller.
func vectorViewFirst(this *inspectable, out *uintptr) uintptr {
	return dispatchInspectable(inspectableWork{fn: vectorViewFirstBody, this: this, p0: unsafe.Pointer(out)})
}

func vectorViewFirstBody(w *inspectableWork) uintptr {
	this, out := w.this, (*uintptr)(w.p0)
	if out == nil {
		return ePointer
	}
	*out = 0
	if !registeredInspectable(this) {
		return eFail
	}
	view := (*VectorView)(unsafe.Pointer(this.identity))
	*out = view.coll.first().Ptr()
	return 0
}

// ---------------------------------------------------------------------------
// Vector

// Vector is a Go-implemented WRITABLE IVector<T> over retained copies of its
// items, plus an IIterable<T> tear-off facet sharing one reference count and
// COM identity. All mutation happens through the WinRT ABI (serialized by
// the inspectable worker); the Go side exposes no mutation API this wave.
//
// Displaced elements are released as they leave: SetAt releases the element
// it overwrites, RemoveAt/RemoveAtEnd/Clear release what they remove, and
// ReplaceAll releases the old payload after the new one fully retained.
type Vector struct {
	hdr      inspectable // must be the first field: identity facet (IVector<T>)
	iterable inspectable // tear-off facet (IIterable<T>)
	coll     collection
}

// NewVectorObject creates a writable IVector<element> (also iterable) named
// class, answering QueryInterface for iids.Vector and iids.Iterable; First
// spawns iterators under iids.Iterator and GetView spawns snapshot views
// under iids.VectorView — all four IIDs must be wired. items are borrowed
// and retained exactly as in NewIterableObject. The object starts with one
// caller-owned reference.
func NewVectorObject(class string, iids CollectionIIDs, codec ElementCodec, items []any) *Vector {
	obj := &Vector{}
	obj.coll = collection{codec: codec, iids: iids, class: class, items: retainAll(codec, items)}
	obj.hdr.lpVtbl = &vectorVtbl[0]
	obj.hdr.iids = []win32.GUID{iids.Vector}
	obj.hdr.destroy = obj.coll.releaseAll
	obj.iterable.lpVtbl = &vectorIterableVtbl[0]
	obj.iterable.iids = []win32.GUID{iids.Iterable}
	initInspectable(class, &obj.hdr, &obj.iterable)
	return obj
}

// Ptr is the COM object pointer to pass where the IVector<T> is expected.
// Where an IIterable<T> is expected, QueryInterface for iids.Iterable
// instead (the iterable facet has its own vtable).
func (o *Vector) Ptr() uintptr { return uintptr(unsafe.Pointer(o)) }

// Release drops the caller's reference (the native side holds its own).
func (o *Vector) Release() uint32 { return uint32(inspectableRelease(&o.hdr)) }

// vectorGetAt is IVector<T>.GetAt (slot 6): a NEW retained element
// representation the caller owns, or E_BOUNDS past the end.
func vectorGetAt(this *inspectable, index uintptr, out unsafe.Pointer) uintptr {
	return dispatchInspectable(inspectableWork{fn: vectorGetAtBody, this: this, u0: index, p0: out})
}

func vectorGetAtBody(w *inspectableWork) uintptr {
	this, index, out := w.this, w.u0, w.p0
	if out == nil {
		return ePointer
	}
	if !registeredInspectable(this) {
		return eFail
	}
	vector := (*Vector)(unsafe.Pointer(this.identity))
	return vector.coll.getAt(index, out)
}

// vectorSize is IVector<T>.get_Size (slot 7).
func vectorSize(this *inspectable, out *uint32) uintptr {
	return dispatchInspectable(inspectableWork{fn: vectorSizeBody, this: this, p0: unsafe.Pointer(out)})
}

func vectorSizeBody(w *inspectableWork) uintptr {
	this, out := w.this, (*uint32)(w.p0)
	if out == nil {
		return ePointer
	}
	*out = 0
	if !registeredInspectable(this) {
		return eFail
	}
	vector := (*Vector)(unsafe.Pointer(this.identity))
	*out = uint32(len(vector.coll.items))
	return 0
}

// vectorGetView is IVector<T>.GetView (slot 8): a NEW immutable SNAPSHOT of
// the current contents (a fresh VectorView with its own re-retained element
// references), its constructor reference transferred to the native caller.
// The WinRT contract permits a view that does not track later mutation; a
// snapshot needs zero invalidation machinery and can never dangle.
func vectorGetView(this *inspectable, out *uintptr) uintptr {
	return dispatchInspectable(inspectableWork{fn: vectorGetViewBody, this: this, p0: unsafe.Pointer(out)})
}

func vectorGetViewBody(w *inspectableWork) uintptr {
	this, out := w.this, (*uintptr)(w.p0)
	if out == nil {
		return ePointer
	}
	*out = 0
	if !registeredInspectable(this) {
		return eFail
	}
	vector := (*Vector)(unsafe.Pointer(this.identity))
	coll := &vector.coll
	view := NewVectorViewObject(collectionSpawnClass(coll.class, "IVectorView`1"), coll.iids, coll.codec, coll.items)
	*out = view.Ptr()
	return 0
}

// vectorIndexOf is IVector<T>.IndexOf (slot 9): the first index whose
// element equals the BORROWED input word under the codec's equality.
func vectorIndexOf(this *inspectable, value uintptr, index *uint32, found *byte) uintptr {
	return dispatchInspectable(inspectableWork{
		fn: vectorIndexOfBody, this: this,
		u0: value, p0: unsafe.Pointer(index), p1: unsafe.Pointer(found),
	})
}

func vectorIndexOfBody(w *inspectableWork) uintptr {
	this, value, index, found := w.this, w.u0, (*uint32)(w.p0), (*byte)(w.p1)
	if index == nil || found == nil {
		return ePointer
	}
	*index = 0
	*found = 0
	if !registeredInspectable(this) {
		return eFail
	}
	vector := (*Vector)(unsafe.Pointer(this.identity))
	return vector.coll.indexOf(value, index, found)
}

// vectorSetAt is IVector<T>.SetAt (slot 10): retain the BORROWED input word,
// store it at index, and release the displaced element. E_BOUNDS past the
// end.
func vectorSetAt(this *inspectable, index uintptr, value uintptr) uintptr {
	return dispatchInspectable(inspectableWork{fn: vectorSetAtBody, this: this, u0: index, u1: value})
}

func vectorSetAtBody(w *inspectableWork) uintptr {
	this, index, value := w.this, w.u0, w.u1
	if !registeredInspectable(this) {
		return eFail
	}
	coll := &(*Vector)(unsafe.Pointer(this.identity)).coll
	i := int(uint32(index))
	if i >= len(coll.items) {
		return eBounds
	}
	elem, hr := coll.codec.UnmarshalWord(value)
	if hr != 0 {
		return hr
	}
	displaced := coll.items[i]
	coll.items[i] = elem
	coll.codec.Release(displaced)
	return 0
}

// vectorInsertAt is IVector<T>.InsertAt (slot 11): retain the BORROWED input
// word and insert it at index (index == Size appends). E_BOUNDS beyond that.
func vectorInsertAt(this *inspectable, index uintptr, value uintptr) uintptr {
	return dispatchInspectable(inspectableWork{fn: vectorInsertAtBody, this: this, u0: index, u1: value})
}

func vectorInsertAtBody(w *inspectableWork) uintptr {
	this, index, value := w.this, w.u0, w.u1
	if !registeredInspectable(this) {
		return eFail
	}
	coll := &(*Vector)(unsafe.Pointer(this.identity)).coll
	i := int(uint32(index))
	if i > len(coll.items) {
		return eBounds
	}
	elem, hr := coll.codec.UnmarshalWord(value)
	if hr != 0 {
		return hr
	}
	coll.items = slices.Insert(coll.items, i, elem)
	return 0
}

// vectorRemoveAt is IVector<T>.RemoveAt (slot 12): release and remove the
// element at index. E_BOUNDS past the end.
func vectorRemoveAt(this *inspectable, index uintptr) uintptr {
	return dispatchInspectable(inspectableWork{fn: vectorRemoveAtBody, this: this, u0: index})
}

func vectorRemoveAtBody(w *inspectableWork) uintptr {
	this, index := w.this, w.u0
	if !registeredInspectable(this) {
		return eFail
	}
	coll := &(*Vector)(unsafe.Pointer(this.identity)).coll
	i := int(uint32(index))
	if i >= len(coll.items) {
		return eBounds
	}
	removed := coll.items[i]
	coll.items = slices.Delete(coll.items, i, i+1)
	coll.codec.Release(removed)
	return 0
}

// vectorAppend is IVector<T>.Append (slot 13): retain the BORROWED input
// word and append it.
func vectorAppend(this *inspectable, value uintptr) uintptr {
	return dispatchInspectable(inspectableWork{fn: vectorAppendBody, this: this, u0: value})
}

func vectorAppendBody(w *inspectableWork) uintptr {
	this, value := w.this, w.u0
	if !registeredInspectable(this) {
		return eFail
	}
	coll := &(*Vector)(unsafe.Pointer(this.identity)).coll
	elem, hr := coll.codec.UnmarshalWord(value)
	if hr != 0 {
		return hr
	}
	coll.items = append(coll.items, elem)
	return 0
}

// vectorRemoveAtEnd is IVector<T>.RemoveAtEnd (slot 14): release and remove
// the last element; E_BOUNDS on an empty vector.
func vectorRemoveAtEnd(this *inspectable) uintptr {
	return dispatchInspectable(inspectableWork{fn: vectorRemoveAtEndBody, this: this})
}

func vectorRemoveAtEndBody(w *inspectableWork) uintptr {
	this := w.this
	if !registeredInspectable(this) {
		return eFail
	}
	coll := &(*Vector)(unsafe.Pointer(this.identity)).coll
	if len(coll.items) == 0 {
		return eBounds
	}
	removed := coll.items[len(coll.items)-1]
	coll.items = coll.items[:len(coll.items)-1]
	coll.codec.Release(removed)
	return 0
}

// vectorClear is IVector<T>.Clear (slot 15): release and remove everything.
func vectorClear(this *inspectable) uintptr {
	return dispatchInspectable(inspectableWork{fn: vectorClearBody, this: this})
}

func vectorClearBody(w *inspectableWork) uintptr {
	this := w.this
	if !registeredInspectable(this) {
		return eFail
	}
	coll := &(*Vector)(unsafe.Pointer(this.identity)).coll
	coll.releaseAll()
	return 0
}

// vectorGetMany is IVector<T>.GetMany (slot 16): write up to capacity NEW
// retained representations (caller owns each) starting at startIndex and
// report the count written (0 at or past the end); mid-loop failures unwind
// everything already written.
func vectorGetMany(this *inspectable, startIndex, capacity uintptr, items unsafe.Pointer, actual *uint32) uintptr {
	return dispatchInspectable(inspectableWork{
		fn: vectorGetManyBody, this: this,
		u0: startIndex, u1: capacity, p0: items, p1: unsafe.Pointer(actual),
	})
}

func vectorGetManyBody(w *inspectableWork) uintptr {
	this, startIndex, capacity := w.this, w.u0, w.u1
	items, actual := w.p0, (*uint32)(w.p1)
	if actual == nil {
		return ePointer
	}
	*actual = 0
	if !registeredInspectable(this) {
		return eFail
	}
	vector := (*Vector)(unsafe.Pointer(this.identity))
	return vector.coll.marshalRange(startIndex, capacity, items, actual)
}

// vectorReplaceAll is IVector<T>.ReplaceAll (slot 17): all-or-nothing —
// every input slot is unmarshaled into a fresh retained slice first (a
// mid-loop failure releases the partial slice and leaves the vector
// untouched), then the old payload is released and swapped out.
func vectorReplaceAll(this *inspectable, count uintptr, items unsafe.Pointer) uintptr {
	return dispatchInspectable(inspectableWork{fn: vectorReplaceAllBody, this: this, u0: count, p0: items})
}

func vectorReplaceAllBody(w *inspectableWork) uintptr {
	this, count, items := w.this, w.u0, w.p0
	if !registeredInspectable(this) {
		return eFail
	}
	vector := (*Vector)(unsafe.Pointer(this.identity))
	return vector.coll.replaceAll(count, items)
}

// vectorFirst is the tear-off facet's IIterable<T>.First (slot 6): a NEW
// iterator over a SNAPSHOT of the vector's current items (the iterator owns
// its own element references, so later mutation of the vector never
// invalidates it), its constructor reference transferred to the native
// caller.
func vectorFirst(this *inspectable, out *uintptr) uintptr {
	return dispatchInspectable(inspectableWork{fn: vectorFirstBody, this: this, p0: unsafe.Pointer(out)})
}

func vectorFirstBody(w *inspectableWork) uintptr {
	this, out := w.this, (*uintptr)(w.p0)
	if out == nil {
		return ePointer
	}
	*out = 0
	if !registeredInspectable(this) {
		return eFail
	}
	vector := (*Vector)(unsafe.Pointer(this.identity))
	*out = vector.coll.first().Ptr()
	return 0
}
