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

## Go-implemented collections (strings only today)

Some WinRT APIs *take* a collection — the Calendar factory constructors
require an `IIterable<String>` of language tags and reject null with
E_POINTER. The runtime layer implements real, OS-callable collection objects
over Go slices:

| Constructor | Implements |
|---|---|
| `winrt.NewStringIterable(items)` | `IIterable<String>` |
| `winrt.NewStringVectorView(items)` | `IVectorView<String>` + an `IIterable<String>` tear-off facet |
| `winrt.NewStringIterator(items)` | `IIterator<String>` (rarely needed directly — `First` creates them) |

Each copies the slice, starts with one caller-owned reference, and is driven
by native code through Go vtables (QueryInterface, First, MoveNext, …). To
pass one into a generated binding, cast to the package-local consumer type —
sound because both layouts are a single vtable-pointer word:

```go
languages := winrt.NewStringIterable([]string{"de-DE", "en-GB"})
calendar, err := globalization.CreateCalendarDefaultCalendarAndClock(
	(*globalization.IIterableOfString)(unsafe.Pointer(languages)))
languages.Release() // the factory has consumed it; ours was the last reference
```

`Release()` returns the remaining count — after the OS finishes with the
object it drains to zero, which the acceptance tests use to prove the OS
leaks nothing.

Only the `String` instantiations exist today. Writable collections
(`IVector<T>`) and non-string element types are deferred; their IIDs are
pinned against the pinterface derivation by `internal/verify`, so extending
the set is mechanical rather than risky.
