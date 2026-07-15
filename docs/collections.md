# Collections

WinRT collections are generic interfaces (`IVectorView<T>`, `IIterable<T>`,
`IIterator<T>`, `IMapView<K,V>`, …). Go has no COM generics, so the generator
**monomorphizes**: every closed instantiation referenced by an emitted member
becomes a concrete type in the consuming package, with an IID derived by the
same algorithm the OS uses (the "pinterface" derivation).

## Monomorphized types

Naming scheme: `IVectorView<String>` → `IVectorViewOfString`,
`IIterable<Package>` → `IIterableOfPackage`,
`TypedEventHandler<A, B>` → `TypedEventHandlerOfAAndB`. They land in the
package that uses them (`<pkg>_pinterfaces.go`), transitively closed — a
vector view brings its iterator along.

**Per-package duplication is intentional.** Two packages using
`IVectorView<String>` each get their own Go type: distinct types, identical
ABI and identical IID. When an API in package A wants the instantiation and
you hold package B's flavor, convert with a QueryInterface for A's type — the
object itself answers the same IID either way:

```go
converted, err := winrt.QueryInterface[pkga.IVectorViewOfString](
	unsafe.Pointer(viewFromPkgB), &pkga.IID_IVectorViewOfString)
defer converted.Release()
```

(An `unsafe.Pointer` cast would also be layout-sound, but the QI keeps the
reference counting explicit.)

## Reading a vector view

Indexed access via `Size`/`GetAt`, membership via `IndexOf`:

```go
languages, err := statics.Languages() // *globalization.IVectorViewOfString
defer languages.Release()
size, err := languages.Size()
for i := uint32(0); i < size; i++ {
	tag, err := languages.GetAt(i)
	// ...
}
var index uint32
found, err := languages.IndexOf("en-US", &index)
```

Pointer-element views (`IVectorViewOfVoiceInformation`) return a caller-owned
reference per `GetAt` — Release each element.

## Iterating

`IIterable<T>.First()` returns an iterator positioned at the first element;
the loop shape is `HasCurrent` / `Current` / `MoveNext`:

```go
iterator, err := packages.First() // *deployment.IIteratorOfPackage
defer iterator.Release()
for {
	has, err := iterator.HasCurrent()
	if err != nil || !has {
		break
	}
	pkg, err := iterator.Current() // pointer elements: a new reference each call
	// ... use pkg ...
	pkg.Release()
	if _, err := iterator.MoveNext(); err != nil {
		break
	}
}
```

Iterators are single-consumer, forward-only; call `First()` again for a
second pass. Every `IVectorView` is also `IIterable` — QueryInterface for the
iterable type when an API gives you only the view and you prefer the loop
(the [collections example](../examples/collections) shows both shapes over
the same list).

## GetMany

`GetMany` (bulk copy into a caller buffer) takes a conformant array, which
the generator does not lower yet — on generated consumer types the slot is
skipped with a `// slot N: GetMany skipped: conformant array` comment. Read
element-wise via `GetAt` or the iterator. (The Go-*implemented* collections
below do implement GetMany on their native-facing vtables, so OS code
consuming them can bulk-read; only the Go-consumer direction is missing.)

## Go-implemented collections (element-generic)

Some WinRT APIs *take* a collection — the Calendar factory constructors
require an `IIterable<String>` of language tags,
`DataPackage.SetStorageItems` an `IIterable<IStorageItem>`, the Ipp
attribute factories `IIterable<Int32>`/`IIterable<Uri>`. The runtime layer
implements real, OS-callable collection objects over Go slices, and the
generator emits a **typed constructor for every monomorphized
`IIterable<X>` / `IVectorView<X>` / `IVector<X>`** whose element it can
marshal:

```go
// In the package that declared the instantiation (<pkg>_pinterfaces.go):
iterable := printers.NewIIterableOfUri([]*foundation.IUriRuntimeClass{u1, u2})
value, err := statics.CreateUriArray(iterable)
iterable.Release() // the OS copied the elements; ours was the last reference

items := datatransfer.NewIIterableOfIStorageItem([]*storage.IStorageItem{a, b})
err = pkg.SetStorageItems(items, true)
items.Release()

vector := globalization.NewIVectorOfString([]string{"one", "two"}) // writable!
```

The constructor returns the package-local consumer type directly — no cast
needed — and the object starts with one caller-owned reference (`Release()`
returns the remaining count; after the OS finishes it drains to zero, which
the acceptance tests use to prove the OS leaks nothing).

Element coverage (the runtime "codec" set):

| Element kind | Payload semantics |
|---|---|
| `String` | copied Go strings; `IndexOf` is string-value equality |
| interface / class / `Object` pointers | the collection **AddRefs** each element and releases it when displaced, removed, or when the collection dies |
| enums and integer scalars | copied values |
| `Guid` | copied values |

Elements outside the set (bool, floats, structs, delegates) get no
constructor — the instantiation type still emits, and the skip is recorded
under the `collection-ctor-skipped` diagnostic key.

**Identity-equality caveat (interface elements).** `IndexOf` on a
Go-implemented collection compares COM identity *words* only — no
`QueryInterface(IUnknown)` is issued from collection bodies. An element
matches only the exact interface pointer the collection holds; a different
interface pointer onto the same COM object does not.

### Writable vectors

`New<IVectorOfX>` builds a real `IVector<T>`: all twelve methods (GetAt,
get_Size, GetView, IndexOf, SetAt, InsertAt, RemoveAt, Append, RemoveAtEnd,
Clear, GetMany, ReplaceAll) are implemented on the native-facing vtable with
the standard contracts — E_BOUNDS on bad indices, ReplaceAll all-or-nothing,
GetMany partial reads with unwind on failure. Two semantics are deliberate
and documented:

- **`GetView` returns a snapshot**: an immutable copy of the contents at
  call time (re-retained for interface elements). Later vector mutation does
  not appear in the view — permissible under the WinRT contract, and it
  needs zero invalidation machinery.
- **Iterators snapshot too**: `First` hands out an iterator over its own
  retained copy, so mutating (or releasing) the source never invalidates it.

Mutation happens only through the WinRT ABI — the Go side exposes no
mutation API — and every ABI entry is serialized by the runtime's dispatch
worker, so no extra locking exists or is needed.

### The runtime core

The generated constructors sit on three exported runtime constructors —
`winrt.NewIterableObject`, `winrt.NewVectorViewObject`,
`winrt.NewVectorObject`, each taking the runtime class name, a
`winrt.CollectionIIDs` set, an element codec (`winrt.CodecString`,
`winrt.CodecInterface`, `winrt.CodecScalar(n)`, `winrt.CodecGuid`), and the
boxed `[]any` payload. The element type is *not* a Go type parameter: IIDs
are derivation-time knowledge and `syscall.NewCallback` slots are
process-permanent, so one shared trampoline per interface shape/slot
dispatches through the codec — a fixed callback budget no matter how many
element types instantiate.

The pre-generic string wrappers remain, unchanged in API and semantics:

| Constructor | Implements |
|---|---|
| `winrt.NewStringIterable(items)` | `IIterable<String>` |
| `winrt.NewStringVectorView(items)` | `IVectorView<String>` + an `IIterable<String>` tear-off facet |
| `winrt.NewStringIterator(items)` | `IIterator<String>` (rarely needed directly — `First` creates them) |

Their hard-coded IIDs are pinned against the pinterface derivation by
`internal/verify`. `IMap`/`IMapView` implementations remain deferred (no
emittable member consumes one today).
