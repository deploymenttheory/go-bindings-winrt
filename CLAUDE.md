# CLAUDE.md

Guidance for Claude Code (claude.ai/code) working in this repository.

## What this is

`go-bindings-winrt` provides idiomatic Go bindings for the **Windows
Runtime** (the `Windows.*` namespaces), the fourth member of the
deploymenttheory Windows bindings family:

- [go-winmd](https://github.com/deploymenttheory/go-winmd) — the shared
  ECMA-335 `.winmd` reader (generics and the event/property tables were added
  for this repo)
- [go-bindings-win32](https://github.com/deploymenttheory/go-bindings-win32)
  — the Win32 surface, and this repo's ABI foundation: `HSTRING`,
  `IInspectable` and the `Ro*` activation functions from its generated
  `system/winrt` package, plus `HRESULT`, `GUID`, `IUnknown` and the DLL
  loader from `bindings/runtime/win32`
- [go-bindings-wdk](https://github.com/deploymenttheory/go-bindings-wdk) —
  the WDK surface

## Commands

```sh
go build ./...
go vet ./cmd/... ./internal/... ./acceptance/ ./bindings/runtime/...  # generated wrappers trip vet by design
go test ./bindings/runtime/...            # makes real WinRT calls; needs Windows
go test ./internal/... ./acceptance/...   # slot/IID checks + tests against live WinRT
go run ./cmd/generate fetch-metadata      # download the pinned contract winmds
go run ./cmd/generate ingest              # winmds → metadata/winrt/*.winrtmeta.json (committed)
go run ./cmd/generate validate            # check the metadata JSON for structural problems
go run ./cmd/generate bindings \
  --diagnostics-baseline metadata/diagnostics-baseline.json      # regenerate the committed tree
  # namespaces come from metadata/emit-roots.txt; --namespace A,B overrides for experiments
go run ./cmd/generate diff --old <dir> --new <dir>               # compare two metadata trees
go run ./examples/calendar                # a complete working example
```

## The runtime layer

`bindings/runtime/winrt/` is hand-written. Generated code depends on it; it
depends on nothing generated.

**`init.go`** — `Initialize()` puts the calling thread in the multithreaded
apartment, and tolerates `RPC_E_CHANGED_MODE`. Apartment initialization is
per *thread*, which is why this is not guarded by a process-wide `sync.Once`:
a `Once` consumed by whichever thread activated first would leave every later
thread uninitialized, and that thread's first activation would fail with
`CO_E_NOTINITIALIZED`. `CoGetApartmentType` is used to test whether the
thread already has an apartment, because it has no side effect —
`RoInitialize` reports "already initialized" as the *success* code `S_FALSE`,
which a binding that maps every success to a nil error cannot distinguish
from `S_OK`, and it takes a reference that would then need releasing. A
thread that already has an apartment keeps it, which is what allows a caller
to enter a single-threaded apartment first (XAML requires one).

**`hstring.go`** — `HString` wraps an input string with `NewHString` / `Raw` /
`Close`. `HStringToString` reads a handle without consuming it (honours the
length, so embedded NUL characters are fine; a null handle reads as `""`).
`TakeHString` reads and then deletes, which is what `[out, retval]` strings
need.

**`activation.go`** — `ActivateInstance(className)` and
`GetActivationFactory(className, iid)`.

**`query.go`** — `QueryInterface[T](unsafe.Pointer, *win32.GUID) (*T, error)`.
This is sound because every generated interface struct is a single
vtable-pointer word, laid out identically to `win32.IUnknown`.

**`guid.go`** — `ParseGUID` / `MustGUID`, for hand-written IID variables.
Generated code uses struct literals instead.

**`delegate.go`** — Go functions callable from WinRT. `NewDelegate` builds a
four-slot COM object over shared `syscall.NewCallback` trampolines (one set
per Invoke parameter count, 0–3 ABI words), keeps live delegates in a
registry keyed by the native `this` pointer, and answers `QueryInterface` for
the delegate's own IID plus `IUnknown` and `IAgileObject`. Event registration
in `acceptance/` exercises this against live WinRT.

Two details are load-bearing:

- *Zero parameters is a real case, not a degenerate one.*
  `DispatcherQueueHandler` has a parameterless `Invoke`, and it is the
  delegate every `TryEnqueue` takes — the only way to move work onto a UI
  thread.
- *`SetInlineThread(tid)`* names an OS thread whose `Invoke` bodies run
  directly on the calling thread instead of being handed to a new goroutine.
  Frameworks with thread affinity need this: XAML keeps its state per UI
  thread, and generated bindings call straight through vtable slots with no
  COM proxy in between, so a handler moved to another goroutine could not
  legally touch the objects it was passed. It is safe for the same reason
  the worker-reentry path in `inspectable.go` is, and because the
  `OutParam` rule below already removed the stack-growth hazard the goroutine
  hop was protecting against. Off by default; the caller is responsible for
  `runtime.LockOSThread`.

**`async.go`** — `AsyncError(status, hresult int32) error`, the error
returned by a generated `Await` when an async operation fails. It names the
`AsyncStatus` and wraps the `IAsyncInfo` error code as a `win32.HRESULT`, so
`errors.As` reaches it.

**`inspectable.go`** — the shared machinery for Go types that implement
`IInspectable`. Each object gets one header per interface it exposes (vtable
word first), sharing `syscall.NewCallback` trampolines for the six
`IInspectable` slots: `QueryInterface` answers the interface IIDs plus
`IUnknown` / `IInspectable` / `IAgileObject`, preserving COM identity across
tear-off interfaces; `AddRef` / `Release` are reference counted; `GetIids`
allocates with `CoTaskMemAlloc`; `GetRuntimeClassName` and `GetTrustLevel`
(`BaseTrust`) are answered directly.

Method *bodies* run on a dedicated worker goroutine. The reason is specific:
a callback that grew the calling goroutine's stack would move that stack, and
strand the raw `&result` pointer that the in-flight generated `SyscallN`
call had already handed to native code. This was observed live, not
theorised. So the callback side only stages its arguments and waits, without
allocating.

The worker locks its OS thread and publishes the thread ID. A callback
arriving *on that thread* can only be a nested re-entry — a collection body
calling `AddRef` or `Release` on a Go-implemented element — so it runs
directly instead of waiting, which is what stops the worker deadlocking
against itself.

An optional per-object `destroy` hook runs once when the reference count
reaches zero; collections use it to release the elements they hold.

**`collections_core.go`** — Go implementations of the WinRT collection
interfaces, written once and reused for every element type. The core types
`Iterable`, `Iterator`, `VectorView` and the writable `Vector` (all twelve
`IVector` slots, including `E_BOUNDS` range checks, all-or-nothing
`ReplaceAll`, `GetView` and `First` returning re-retained snapshots, and
`GetMany` unwinding on failure) hold a `[]any` and an `ElementCodec` that
knows how to convert one element: `CodecString`, `CodecInterface`
(`AddRef`/`Release` retention, `IndexOf` comparing identity pointers — it
does not call `QueryInterface`, which is a documented limitation),
`CodecScalar(n)`, `CodecGuid`. One trampoline per interface-and-slot handles
every element type by going through the codec, so the number of
`syscall.NewCallback` allocations is fixed rather than growing per type.
Constructed with `NewIterableObject` / `NewVectorViewObject` /
`NewVectorObject(class, CollectionIIDs, codec, items)`.

**`collections.go`** — string-specific wrappers over that core:
`NewStringIterable`, `NewStringIterator`, `NewStringVectorView`. Their IIDs
are written out literally and checked against the calculated parameterized
interface IDs by `internal/verify`. Either cast `Ptr()` to a generated
consumer type, or use the generated typed constructors: every
`IIterable<X>` / `IVectorView<X>` / `IVector<X>` with a codec-able element
type gets `New<Mangled>(items []<GoElem>) *<Mangled>` in
`<pkg>_pinterfaces.go`. Element types with no codec are skipped and recorded
as `collection-ctor-skipped`. The Calendar factories,
`IppAttributeValueStatics.Create{Uri,Integer}Array`,
`DataPackage.SetStorageItems` → `GetStorageItemsAsync().Await()`, and the
whole writable `IVector<String>` surface are covered by tests against live
WinRT in `acceptance/`.

## Metadata and the generator

**`internal/winrtmeta`** and **`internal/winrtmeta/ingest`** turn the pinned
contract winmds into one JSON file per namespace under `metadata/winrt/`.
These are committed, and CI keeps them in step with the winmds. Methods are
recorded with their *logical* signature; converting that to the ABI shape
(HRESULT return, value written through a trailing `[out, retval]` pointer) is
the emitter's job.

**`internal/codegen/`** follows the same layout as go-bindings-win32:

- **`pipeline/`** loads the JSON into a registry. `ComputeBlockedImports`
  breaks import cycles between namespaces; a reference across a broken edge
  is skipped and recorded as `import-cycle-skipped`.
- **`typemap/`** is the only place that decides what a metadata type becomes
  in Go. `Bool` is one byte at the ABI (Go `bool`); `HString` is a Go
  `string` (`syswinrt.HSTRING` at the ABI) — except as a STRUCT FIELD, where
  `StructFieldGoType` emits the handle itself, because a struct crosses as a
  block of bytes and has no boundary at which a conversion could run; `Guid`
  is `win32.GUID`; `Object`
  is `*syswinrt.IInspectable`; a class reference becomes a pointer to that
  class's default interface. Closed generic *interface* instantiations become
  a package-local concrete type through the `Context.RequestInstantiation`
  callback, and delegate references in method *parameters* become
  package-local handler types through `Context.RequestDelegate` — which is
  wired for parameters only, so delegates in return position, open generics
  and arrays still cause the member to be skipped. Two Windows.Foundation
  types are mapped rather than re-emitted: `EventRegistrationToken` to
  `syswinrt`, and `HResult` to `int32`.
- **`emit/winrt/`** asks `typemap` about everything and builds plain data
  structures; `render/` runs `//go:embed` templates over them. The templates
  contain no decisions — they read fields that were already worked out and
  pick between fixed alternatives. Files written per package (only when
  non-empty): `doc.go`, `<pkg>_enums.go`, `<pkg>_structs.go`,
  `<pkg>_interfaces.go`, `<pkg>_classes.go`, `<pkg>_pinterfaces.go` (generic
  instantiations), `<pkg>_delegates.go` (handler types, created on demand by
  events and by delegate-typed parameters).
- **`shared/fileasm/`** writes the `DO-NOT-EDIT` header, build tag and
  `go/format` output. The emitter deletes generated files that no longer
  correspond to anything in the metadata, but only ever files carrying that
  header.

## What gets generated

### Vtable slots and naming

`IInspectable` occupies slots 0–5, so an interface's first method is slot 6,
numbered in MethodDef order. **A skipped member never renumbers the ones
after it** — it leaves a `// slot N: name skipped: reason` comment in place,
so every other method on the interface still dispatches correctly.

`get_X` becomes `X()`, `put_X` becomes `SetX()`, and other methods keep their
metadata names. Overloaded methods share one MethodDef name in metadata; the
`[Overload]` attribute carries the distinct name, and that is what the Go
method is called (`MonthAsFullString`, whose metadata name is
`MonthAsString`).

### Classes and interfaces

A class embeds its default interface by value, so those methods are available
without a cast. Every other instance interface gets an `As<Name>()` method.
Inherited interfaces from a base class are reached with
`winrt.QueryInterface`.

### Constructors, statics and factories

- Directly activatable classes get `NewFoo()`.
- Each `[Static]` interface on a class gets a package-level accessor named
  after the interface without its leading `I` — `CalendarIdentifiersStatics()`
  — which fetches the activation factory, queries it for that interface's
  IID, and returns it. The caller owns the reference. A class with only
  statics emits just the accessors and no class type.
- Each method of a non-generic `[Activatable]` factory interface that returns
  the class's default interface becomes a package-level function named after
  the method — `CreateGeographicRegion(code string) (*GeographicRegion, error)`
  — which fetches the factory, calls the generated factory method, and
  re-types the result as the class. Names that collide across classes get a
  factory-ordinal suffix. The factory interface itself is still emitted
  normally. The factory is fetched per call; caching it is a possible future
  optimisation.

### Composable classes: create, but do not derive

A `[Composable]` factory method qualifies as a constructor when its trailing
parameters are the COM aggregation pair (the controlling outer as an `Object`
in, the inner as an `Object` out) and it returns the class's default
interface. It becomes a package-level `New<Class>` or
`New<Class>With<Suffix>` taking only the leading parameters. It passes a null
controlling outer, releases the non-null inner it gets back (under null-outer
aggregation that is a second reference to the same object), and re-types the
result as the class.

Methods that do not match that shape record `composable-factory-skipped`. A
composable class with no factory at all — `SpriteVisual` — is a normal
platform shape, not a failure, and records nothing.

**Writing a Go type that derives from a WinRT class is out of scope.** A
class-typed parameter or return always resolves to the class's default
interface pointer, composable or not.

### Generic interfaces

A closed generic interface instantiation referenced by a generated member is
emitted as a concrete type in the *consuming* package's
`<pkg>_pinterfaces.go`. The name is mangled — ``IVectorView`1<String>``
becomes `IVectorViewOfString` — the IID is calculated by
`internal/codegen/pinterface`,
and slot numbers come from the open interface's MethodDef order. Instantiations
are followed transitively (`First` pulls in `IIterator`, `GetView` pulls in
`IVectorView`) and deduplicated per package.

Two packages using the same instantiation each get their own copy: different
Go types, identical ABI.

### Events

`add_` / `remove_` accessors become
`Add<Event>(handler *<Handler>) (syswinrt.EventRegistrationToken, error)` and
`Remove<Event>(token) error`.

The event's delegate type — either a generic instantiation
(``TypedEventHandler`2<A,B>`` → `TypedEventHandlerOfAAndB`, IID calculated) or
a non-generic delegate (IID declared in metadata) — becomes a typed handler
in `<pkg>_delegates.go` wrapping `winrt.Delegate`. The handler's constructor
converts raw ABI words into typed callback arguments: pointers are
**borrowed**, valid only until the callback returns, and HSTRINGs are read
without being consumed.

An event whose delegate cannot be converted is skipped as
`event-delegate-unloweable`. That means a delegate with float, struct or
array parameters, one whose `Invoke` returns a value, or one with a parameter
count outside 0–3.

Delegate-typed method *parameters* go through the same machinery and the same
rules: a convertible one becomes `handler *<Handler>` and passes
`handler.Ptr()` (nil passes NULL). One that is not keeps its
`delegate-param-skipped` or `generic-member-skipped` entry, and methods
*returning* delegates (`get_Completed`) stay skipped for the same reason.

### Async

`put_Completed` becomes `SetCompleted`. Every generated `IAsyncOperationOf<X>`
whose `SetCompleted` and `GetResults` both emitted — plus plain
`foundation.IAsyncAction` — gains a blocking `Await()` returning
`(<X>, error)` or `error`.

`Await` registers a Completed handler that sends the terminal `AsyncStatus`
over a buffered channel, blocks, then calls `GetResults()` on success or
returns a `winrt.AsyncError` (status plus `IAsyncInfo` error code) otherwise.
It cannot race — WinRT guarantees a handler assigned after completion is
invoked immediately — and it cannot deadlock, because the send happens on the
delegate runtime's own Invoke goroutine. It blocks indefinitely by design; a
context-aware variant is future work. `acceptance/async_test.go` covers it
against live WinRT, including the already-completed and failure paths.

### Still not generated

Open generic types themselves, methods returning delegates
(`get_Completed`), delegate type definitions in their home namespace, and
arrays. Each records a diagnostic.

## The amd64 ABI

`internal/codegen/typemap/layout.go` is what allows floats and by-value
structs to be generated rather than skipped.

- **A float return was never an ABI question.** A WinRT return is an
  `[out, retval]` *pointer* and the HRESULT is the real return value, so the
  float comes back through memory on every architecture.
- **A float parameter** travels as its bit pattern in an ordinary argument
  word (`math.Float64bits`). Go's `asm_windows_amd64.s` copies the first four
  argument words into XMM0–XMM3 before the call, and arguments five onward
  occupy the same stack slot whatever their type.
- **A by-value struct is classified by size alone**, which is the Windows x64
  rule. One, two, four or eight bytes travel in a general purpose register as
  an integer of that width — read at exactly that width, since reading eight
  bytes out of a four-byte struct would read past it. Any other size travels
  as a pointer to a caller-owned temporary, passed through `winrt.OutParam`
  because the stale-stack-pointer problem is the same in this direction. MSVC
  never puts a struct in an XMM register whatever its fields, so a two-float
  `Point` is an eight-byte integer word.
- **Sizes are calculated, not measured**, because the decision is needed
  while generating. That is sound because a struct that can be generated
  holds only scalars, floats, bools, GUIDs, enums and nested structs, all of
  which Go lays out exactly as C does. `layout_test.go` checks the arithmetic
  (tail padding, GUID's four-byte alignment) and
  `acceptance/abi_float_struct_test.go` checks the result against live WinRT
  calls.
- **Delegate `Invoke` signatures still skip floats and structs.** That limit
  is real: `callbackasm1` saves only CX, DX, R8 and R9 and no XMM register,
  so values arriving that way cannot be recovered.

## Members that cannot be generated yet

`metadata/diagnostics-baseline.json` is the committed list of every member
the generator currently skips, and why. `bindings --diagnostics-baseline`
fails if anything is skipped that is not already on the list. The list may
shrink — delete the entries your change fixes, in the same pull request — but
it cannot grow without review.

CI's regen job additionally requires the committed tree to regenerate byte
for byte from the committed metadata. The tree is the namespaces named in
`metadata/emit-roots.txt` plus everything they reference. That file is read
when `--namespace` is absent; `--namespace` takes a comma-separated list as
an override for experiments.

The roots are currently the full surface — every namespace in the ingested
metadata, written out explicitly (282 packages, about 571,000 generated
lines). Regenerate the list after an ingest with the command in the file's
header. A namespace whose last segment is a Go keyword gets a trailing
underscore: `Windows.Media.Import` → package `import_` in `media/import_`.

## Never redeclare the ABI

`HSTRING`, `IInspectable`, `IActivationFactory`, `EventRegistrationToken`,
`TrustLevel` and every `Ro*` / `Windows*` function come from
`go-bindings-win32/bindings/win32/system/winrt` (import alias `syswinrt`).
`HRESULT` and `ErrIfFailed`, `GUID`, `IUnknown` and the UTF-16 helpers come
from `go-bindings-win32/bindings/runtime/win32` (alias `win32`).

## Architecture support

Every Windows-facing file carries `//go:build windows && amd64`.

arm64 is excluded rather than generated for. Go's `asm_windows_arm64.s` loads
only R0–R7 and never touches V0–V7 — it still carries a
`TODO(rsc) floating point like amd64` note — so on Windows on ARM every
`double` (passed in V0) and every all-float struct such as `Point`, `Rect` or
`Vector3` (passed in V0–V3) would be silently corrupted. Generating for arm64
would mean either producing code that miscompiles those members, or skipping
about 2,000 of them on that architecture alone. Excluding it is the honest
option until Go copies the arguments across. 386 stays excluded for the older
reason: 32-bit pointers and different struct layouts. CI checks the build tag
rather than cross-compiling, because a wildcard `GOARCH=arm64 go build`
silently succeeds by building nothing.

## Conventions

- **WinRT calling convention.** Methods return HRESULT at the ABI; the value
  the caller wants is written through the trailing `[out, retval]` parameter.
- **`internal/verify`** recalculates the Calendar slot numbers and IIDs from
  the committed winmd, so a metadata bump or a generator bug that reorders
  anything fails there rather than as a corrupted call at runtime.
- **`metadata/winmd/`** (two contract winmds and `PROVENANCE.json`) is
  committed. `go run ./cmd/generate fetch-metadata` refreshes it, with
  `--version latest` to move to a newer SDK.
- Conventional commits, release-please, SHA-pinned GitHub Actions,
  LF-normalised text (`.gitattributes`), `*.winmd` marked binary.
- `docs/ROADMAP.md` tracks what is done and what is deferred. Deriving from
  composable classes in Go and returning delegates stay deferred; events,
  statics, factory constructors, composable constructors, delegate-typed
  parameters and async `Await` (including `WithProgress`) are all generated.
  User-facing guides are in `docs/*.md`; runnable examples in `examples/`,
  indexed by `examples/README.md`.
