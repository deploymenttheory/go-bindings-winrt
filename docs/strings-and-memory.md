# Strings and memory

WinRT is reference-counted COM with its own string type. The bindings hide
almost all of it — you see Go strings and `Release()` — but the rules below
explain what happens at the boundary and what you must do when you step
outside the generated surface.

## HSTRING: you usually never see one

WinRT strings are HSTRINGs — immutable, length-prefixed UTF-16 handles. The
generated bindings convert at the boundary in both directions:

```go
// string in: the binding creates an HSTRING for the call and deletes it after.
err := calendar.ChangeCalendarSystem("HebrewCalendar")

// string out: the binding reads the returned HSTRING and deletes it.
month, err := calendar.MonthAsFullString() // plain Go string
```

You only handle HSTRINGs yourself when calling raw ABI surfaces. The runtime
helpers, and who owns what:

| Helper | Direction | Ownership |
|---|---|---|
| `winrt.NewHString(s)` | Go → HSTRING | You own it: `defer h.Close()`. Pass `h.Raw()` to the call. The empty string is the null handle (canonical empty); strings containing NUL are rejected. |
| `winrt.HStringToString(h)` | HSTRING → Go | **Borrows** — reads without releasing. Use for handles you do not own (callback arguments). Honors the length: embedded NULs are legal; the null handle reads as `""`. |
| `winrt.TakeHString(h)` | HSTRING → Go | **Consumes** — reads then deletes. Use for `[out, retval]` HSTRINGs a call handed you (this is what generated code does). |

Choosing between the last two is the whole game: a string an API *returned*
is yours to delete (`TakeHString`); a string passed *into* your callback is
the caller's (`HStringToString`).

## Reference counting

Interface pointers are refcounted; the rules are mechanical:

- **Returns are owned by you.** Constructors, statics accessors, `As<Name>()`
  queries, `winrt.QueryInterface`, `Await` results, and every method
  returning an interface pointer transfer one reference: `defer x.Release()`.
- **Arguments you pass are borrowed by the callee.** Passing an interface
  pointer into a method does not transfer your reference — the callee AddRefs
  if it keeps the object. Release your reference when *you* are done, even if
  the OS still holds its own (Go-implemented collections prove this live: the
  caller's `Release()` after a factory call returns a remaining count of 0
  once the OS drops its temporary references).
- **Callback arguments are borrowed by you.** Handler parameters are valid
  only for the callback's duration — never Release, never retain
  ([events guide](events-and-delegates.md)).
- Iterators hand out a **new reference per element** for pointer-typed
  elements (`iterator.Current()` on `IIterable<Package>` returns a reference
  you must Release each iteration).

`Release()` returns the remaining count, which is occasionally useful for
leak-checking; ignore it otherwise.

## Out-parameters and the heap (why you see allocations)

A WinRT call can reenter Go before it returns — the callee may QueryInterface
a Go-implemented argument (a delegate, a collection) on the same thread.
While that nested callback is in flight, the calling goroutine is an ordinary
blocked goroutine, and the Go garbage collector is free to **move its stack**.
Any raw pointer the native side still holds into that stack — like the
address of a local the binding passed as an out-parameter — would go stale,
and the callee would write its result into freed stack memory.

Go's heap does not move, so the invariant (generated and hand-written code
alike) is: *every pointer handed to native code for writing is
heap-allocated*. Generated bindings route out-params through
`winrt.OutParam`, which forces the pointee onto the heap.

What this means for you:

- **Nothing to do.** The hardening is invisible and automatic; locals you
  pass as out-pointers to generated methods (`languages.IndexOf(tag, &index)`)
  are quietly heap-moved by the same mechanism.
- **It explains allocation counts.** A generated call typically allocates a
  few words for its out-params even when the results are scalars. That is the
  price of the invariant, not an accident to optimize away.
- If you hand-write a raw `SyscallN` against a vtable slot, you inherit the
  obligation: pass `uintptr(winrt.OutParam(unsafe.Pointer(&result)))`, never
  a bare stack address.

## Structs and enums

WinRT value structs generate as plain Go structs with identical layout
(`foundation.DateTime{UniversalTime: ticks}`,
`applicationmodel.PackageVersion{Major, Minor, Build, Revision}`); pass and
receive them by value. Enums are typed Go constants over `int32`/`uint32`
(`notifications.ToastTemplateTypeToastText01`). Neither carries references —
nothing to release.

## `unsafe` is expected at the boundary

Casting between same-layout interface types (`QueryInterface`'s input, the
Go-collection-to-consumer-type cast) uses `unsafe.Pointer`. Every generated
interface struct is a single vtable-pointer word, layout-compatible with
`win32.IUnknown` — that compatibility is a design guarantee, not a
coincidence. Keep the `unsafe` at the call boundary and expose ordinary Go
types to the rest of your program, as the [examples](../examples) do.
