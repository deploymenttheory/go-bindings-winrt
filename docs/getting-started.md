# Getting started

Call the Windows Runtime (`Windows.*`) from Go with generated bindings — Go
strings, Go errors, typed interfaces and events, and a blocking `Await()` for
async operations, across every namespace in the Windows SDK contract winmds
(282 packages, `Windows.ApplicationModel` through `Windows.Web.UI.Interop`).

## Install

```sh
go get github.com/deploymenttheory/go-bindings-winrt@latest
```

The bindings target **Windows on amd64 or arm64** (the same 64-bit LLP64
layout). Every Windows-facing file carries `//go:build windows && (amd64 ||
arm64)`, so a non-Windows or 32-bit build simply skips them. The only runtime
dependency is [go-bindings-win32](https://github.com/deploymenttheory/go-bindings-win32),
which supplies the ABI layer; nothing links beyond the standard library.

## The three layers

| Layer | Import path | What it is |
|---|---|---|
| **Generated bindings** | `bindings/winrt/<namespace>` | One Go package per WinRT namespace (`Windows.UI.Notifications` → `ui/notifications`): interfaces, runtime classes, enums, structs, events, async operations. This is what you program against. |
| **WinRT runtime** | `bindings/runtime/winrt` | The hand-written support layer the bindings use: `Initialize`, activation, `QueryInterface`, HSTRING helpers, Go-implemented delegates and collections, `AsyncError`. You touch it for `QueryInterface` and `NewStringIterable`; the rest is called for you. |
| **ABI foundation** | go-bindings-win32's `bindings/win32/system/winrt` (alias `syswinrt`) and `bindings/runtime/win32` (alias `win32`) | HSTRING, IInspectable, `EventRegistrationToken`, the `Ro*` activation functions, HRESULT/GUID/IUnknown. This repo never redeclares the ABI — types cross module boundaries as-is. |

Rule of thumb: import `bindings/winrt/<namespace>` for the API,
`bindings/runtime/winrt` when you need `QueryInterface` or a Go-implemented
collection, and go-bindings-win32's `runtime/win32` when you match HRESULTs
with `errors.As`/`errors.Is`.

## Your first call

A toast notification, end to end:

```go
//go:build windows && (amd64 || arm64)

package main

import (
	"log"

	"github.com/deploymenttheory/go-bindings-winrt/bindings/winrt/ui/notifications"
)

func main() {
	statics, err := notifications.ToastNotificationManagerStatics()
	if err != nil {
		log.Fatal(err)
	}
	defer statics.Release()
	doc, _ := statics.GetTemplateContent(notifications.ToastTemplateTypeToastText01)
	defer doc.Release()
	toast, _ := notifications.CreateToastNotification(doc)
	defer toast.Release()
	notifier, _ := statics.CreateToastNotifierWithId("my.app.aumid")
	defer notifier.Release()
	if err := notifier.Show(&toast.IToastNotification); err != nil {
		log.Fatal(err)
	}
}
```

Run it: `go run .` — see [`examples/`](../examples) for complete programs
(this one, with the DOM mutation and the AUMID caveat, is
[`examples/toast`](../examples/toast)).

There is no explicit initialization step: every activation path calls
`winrt.Initialize()` (process-wide, multithreaded apartment) implicitly. Call
it directly only to surface initialization errors early.

## Getting objects

WinRT has three ways to produce an instance, and the bindings project each:

**Direct activation** — a class with a default constructor gets `NewFoo()`:

```go
calendar, err := globalization.NewCalendar()
defer calendar.Release()
```

**Factory constructors** — a class constructed through an `[Activatable]`
factory interface gets a package-level function named after the factory
method:

```go
region, err := globalization.CreateGeographicRegion("US")
toast, err := notifications.CreateToastNotification(doc)
```

**Statics accessors** — each `[Static]` interface on a class gets a
package-level accessor returning the statics interface (the activation
factory queried to the statics IID; you own the reference):

```go
statics, err := storage.StorageFileStatics()
defer statics.Release()
operation, err := statics.GetFileFromPathAsync(path)
```

Statics-only classes (`ApplicationLanguages`, `MdmAllowPolicy`) have no class
type at all — just their accessors.

## Classes, interfaces, and QueryInterface

A runtime class is a struct embedding its **default interface** by value:

```go
type Calendar struct {
	ICalendar
}
```

so every default-interface method is directly callable on the class. A
class's OTHER interfaces get `As<Name>()` query methods:

```go
timeZone, err := calendar.AsTimeZoneOnCalendar()
defer timeZone.Release()
```

When you hold a bare interface pointer (an `Await` result, an event
argument), query manually with the runtime — every generated interface struct
is a single vtable-pointer word, which is what makes the generic cast sound:

```go
item, err := winrt.QueryInterface[storage.IStorageItem](
	unsafe.Pointer(file), &storage.IID_IStorageItem)
defer item.Release()
```

Every interface's IID is generated as `IID_IFoo` in the same package.

## Release discipline

WinRT is reference-counted COM. The rules the bindings expect:

- **Release what you own.** Every constructor, statics accessor, `As<Name>()`
  query, `QueryInterface`, and interface-pointer-returning method hands you a
  reference — `defer x.Release()` is idiomatic. `Release` is promoted from
  the embedded IInspectable → IUnknown chain, so it is always there.
- **Callback arguments are borrowed.** Pointers passed INTO your event
  handler are owned by the event source for the duration of the callback —
  do not Release or retain them ([events guide](events-and-delegates.md)).
- Value returns (strings, numbers, enums, structs) carry no reference;
  nothing to release. Generated methods that return HSTRINGs consume them for
  you — you always see plain Go `string`.

## Errors

Every method returns `error`; failures are `win32.HRESULT` values from
go-bindings-win32's runtime, so `errors.As`/`errors.Is` work:

```go
var hr win32.HRESULT
if errors.As(err, &hr) {
	fmt.Printf("HRESULT 0x%08X\n", uint32(hr))
}
```

Async failures wrap the HRESULT in a `winrt.AsyncError` naming the terminal
status — the chain still reaches the HRESULT ([async guide](async.md)).

## Next

- [Strings and memory](strings-and-memory.md) — the HSTRING lifecycle,
  ownership, and the out-param heap rule.
- [Async operations](async.md) — `Await`, failure surfacing, IAsyncInfo.
- [Events and delegates](events-and-delegates.md) — typed handlers, tokens,
  the execution model.
- [Collections](collections.md) — monomorphized generics, iteration,
  Go-implemented collections.
- [Examples](../examples) — nine runnable programs.
