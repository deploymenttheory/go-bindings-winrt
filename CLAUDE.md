# CLAUDE.md

Guidance for Claude Code (claude.ai/code) working in this repository.

## What this is

`go-bindings-winrt` provides idiomatic Go bindings for the **Windows
Runtime** (the `Windows.*` namespaces), the fourth member of the
deploymenttheory Windows bindings family:

- [go-winmd](https://github.com/deploymenttheory/go-winmd) — the shared
  ECMA-335 `.winmd` reader (generics + event/property tables landed for
  this repo)
- [go-bindings-win32](https://github.com/deploymenttheory/go-bindings-win32)
  — the Win32 surface; also supplies this repo's ABI foundation (HSTRING,
  IInspectable, Ro* activation via its generated `system/winrt` package, and
  HRESULT/GUID/IUnknown/loader via its `bindings/runtime/win32`)
- [go-bindings-wdk](https://github.com/deploymenttheory/go-bindings-wdk) —
  the WDK surface

## Commands

```sh
go build ./...
go vet ./cmd/... ./internal/... ./acceptance/ ./bindings/runtime/...  # generated wrappers trip vet by design
go test ./bindings/runtime/...            # live WinRT calls; needs Windows
go test ./internal/... ./acceptance/...   # slot/IID guard + live acceptance
go run ./cmd/generate fetch-metadata      # refresh the pinned contract winmds
go run ./cmd/generate ingest              # winmds → metadata/winrt/*.winrtmeta.json (committed)
go run ./cmd/generate validate            # structural integrity checks over the IR
go run ./cmd/generate bindings \
  --diagnostics-baseline metadata/diagnostics-baseline.json      # regenerate the committed tree
  # roots come from metadata/emit-roots.txt; --namespace A,B is an ad-hoc override
go run ./cmd/generate diff --old <dir> --new <dir>               # semantic API diff between IR trees
go run ./examples/calendar                # the vertical, end to end
```

## Architecture

- **`bindings/runtime/winrt/`** — the hand-written runtime layer:
  - `init.go` — process-wide `Initialize()` (MTA once-guard;
    RPC_E_CHANGED_MODE tolerated), `Uninitialize()`.
  - `hstring.go` — `HString` RAII input wrapper (`NewHString`/`Raw`/`Close`),
    `HStringToString` (length-honoring read; embedded NULs legal; null
    handle = ""), `TakeHString` (read + delete — the `[out, retval]`
    consumption helper).
  - `activation.go` — `ActivateInstance(className)` and
    `GetActivationFactory(className, iid)`.
  - `query.go` — `QueryInterface[T](unsafe.Pointer, *win32.GUID) (*T, error)`;
    sound because every generated interface struct is a single
    vtable-pointer word, layout-compatible with `win32.IUnknown`.
  - `guid.go` — `ParseGUID`/`MustGUID` for hand-written IID vars (generated
    code uses struct literals).
  - `delegate.go` — Go-implemented WinRT delegates (`NewDelegate`): a
    4-slot COM object over shared `syscall.NewCallback` trampolines (one
    set per Invoke arity, 1–3 raw ABI words), a pin registry keyed by the
    native `this` word, and QI answering the delegate IID + IUnknown +
    IAgileObject. Live-proven by event registration in `acceptance/`.
  - `async.go` — `AsyncError(status, hresult int32) error`, the
    terminal-failure error generated `Await` methods return: names the
    AsyncStatus and wraps the IAsyncInfo error code as a `win32.HRESULT`
    (reachable via `errors.As`).
  - `inspectable.go` — the shared core for Go-implemented INSPECTABLE
    objects: per-facet `inspectable` headers (vtable word first), shared
    `syscall.NewCallback` trampolines for the six IInspectable slots (QI
    answering the facet IIDs + IUnknown/IInspectable/IAgileObject with COM
    identity preserved across tear-off facets, refcounted AddRef/Release,
    GetIids via CoTaskMemAlloc, GetRuntimeClassName, GetTrustLevel =
    BaseTrust), and its own pin registry (delegates keep theirs). Method
    BODIES run on a dedicated worker goroutine: a reentrant callback that
    grew the calling goroutine's stack would move it and strand the raw
    `&result` out-pointer the in-flight generated SyscallN already handed
    to native code (verified live), so the callback side only stages
    arguments and parks, allocation-free. The worker is thread-locked and
    publishes its OS thread id: a callback arriving ON that thread is a
    nested reentry (a collection body AddRef/Releasing a Go-implemented
    element) and runs INLINE instead of parking — the self-deadlock guard
    the element codecs rely on. An optional per-object `destroy` hook runs
    once at refcount zero (collections release retained elements there).
  - `collections_core.go` — element-generic Go-implemented WinRT
    collections: non-generic core types `Iterable`/`Iterator`/`VectorView`/
    writable `Vector` (all 12 IVector slots: E_BOUNDS bounds, ReplaceAll
    all-or-nothing, GetView/First = re-retained SNAPSHOTS, GetMany with
    unwind) carrying a `[]any` payload plus an `ElementCodec`
    (`CodecString`, `CodecInterface` — AddRef/Release retention,
    identity-word IndexOf equality, documented no-QI limitation —
    `CodecScalar(n)`, `CodecGuid`). One shared trampoline per
    shape/slot dispatches through the codec: fixed NewCallback budget, never
    per element type. Constructors `NewIterableObject`/`NewVectorViewObject`/
    `NewVectorObject(class, CollectionIIDs, codec, items)`.
  - `collections.go` — the String wrappers over that core (exported API and
    IIDs unchanged): `NewStringIterable`, `NewStringIterator`,
    `NewStringVectorView`. IIDs hard-coded and pinned against the
    pinterface derivation by `internal/verify`; cast `Ptr()`/the object
    pointer to a generated consumer type, or use the GENERATED typed
    constructors — every monomorphized `IIterable<X>`/`IVectorView<X>`/
    `IVector<X>` with a codec-able element gains `New<Mangled>(items
    []<GoElem>) *<Mangled>` in `<pkg>_pinterfaces.go` (sibling
    instantiations requested so their IID vars exist in-package;
    un-codec-able elements skip under `collection-ctor-skipped`). The
    Calendar factories, `IppAttributeValueStatics.Create{Uri,Integer}Array`,
    `DataPackage.SetStorageItems` → `GetStorageItemsAsync().Await()`, and
    the full writable IVector<String> surface are live-proven in
    `acceptance/`.
- **`internal/winrtmeta` + `internal/winrtmeta/ingest`** — the IR and its
  producer: the pinned contract winmds project into per-namespace
  `metadata/winrt/<Namespace>.winrtmeta.json` files (committed; the CI
  regen gate keeps them in lockstep with the winmds). Methods
  carry the LOGICAL signature — the HRESULT return and trailing
  `[out, retval]` lowering is emit's job, not ingest's.
- **`internal/codegen/`** — the generator (mirrors go-bindings-win32's
  layout):
  - `pipeline/` — loads the IR into a Registry; `ComputeBlockedImports`
    severs import cycles (references along severed edges degrade with an
    `import-cycle-skipped` diagnostic).
  - `typemap/` — the ONE type-decision site. Bool = 1 byte at the ABI (Go
    bool), HString = Go string (syswinrt.HSTRING at the ABI), Guid =
    win32.GUID, Object = *syswinrt.IInspectable; ApiRef class → its default
    interface pointer; closed generic INTERFACE instantiations resolve to a
    package-local monomorphized type via the gather layer's
    `Context.RequestInstantiation` callback; delegate references in METHOD
    PARAMETERS (incl. generic delegate instantiations) ground into
    package-local handler types via the `Context.RequestDelegate` callback
    (wired only for parameter resolution); delegates in RETURN
    position/open generics/arrays degrade the member.
    External map (never re-emitted): Windows.Foundation
    EventRegistrationToken → syswinrt, HResult → int32.
  - `emit/winrt/` — gather → view → render with the render firewall:
    gather resolves everything through the typemap into pure-data view
    models; `render/` executes `//go:embed` templates that only branch on
    precomputed kinds, never decide. Emits per package (non-empty only):
    doc.go, `<pkg>_enums.go`, `<pkg>_structs.go`, `<pkg>_interfaces.go`,
    `<pkg>_classes.go`, `<pkg>_pinterfaces.go` (generic instantiations),
    `<pkg>_delegates.go` (delegate handler types, grounded by events and
    delegate-typed method parameters).
  - `shared/fileasm/` — DO-NOT-EDIT header + build tag + go/format; the
    emitter is self-cleaning and only ever prunes files bearing the header.
- **Emit rules**: slot = 6 + MethodDef index; skipped members NEVER
  renumber — they leave `// slot N: name skipped: reason` comments.
  Classes (non-composable) embed their default interface by value;
  direct-activatable classes get `NewFoo()`; other instance interfaces get
  `As<Name>()` query methods. Closed generic INTERFACE instantiations
  referenced by emittable members are emitted as concrete (monomorphized)
  types into the CONSUMING package's `<pkg>_pinterfaces.go` — mangled name
  (`IVectorView`1<String>` → `IVectorViewOfString`), IID derived by
  `internal/codegen/pinterface`, slots preserved from the open interface's
  MethodDef order, transitively closed (First → IIterator, GetView →
  IVectorView) and deduped per package. Two packages using the same
  instantiation each get their own copy: distinct Go types, identical ABI.
  Events ARE emitted: add_/remove_ accessors become
  `Add<Event>(handler *<Handler>) (syswinrt.EventRegistrationToken, error)`
  / `Remove<Event>(token) error`, and the event's delegate type — a generic
  instantiation (TypedEventHandler`2<A,B> → `TypedEventHandlerOfAAndB`, IID
  pinterface-derived) or a non-generic delegate (declared IID) — grounds
  into a package-local typed handler in `<pkg>_delegates.go` wrapping the
  runtime `winrt.Delegate`; the constructor's adapter converts the raw ABI
  words to typed callback arguments (pointers are BORROWED for the
  callback's duration, HSTRINGs read without consuming). Events whose
  delegate cannot be adapted (float/struct/array params, an Invoke return,
  or a param count outside 1–3) skip with `event-delegate-unloweable`.
  Delegate-typed method PARAMS ARE emitted through the same grounding (same
  adaptability rules): an adaptable delegate param lowers to
  `handler *<Handler>` passing `handler.Ptr()` (nil passes NULL); an
  un-adaptable one keeps the `delegate-param-skipped` /
  `generic-member-skipped` diagnostics, and methods RETURNING delegates
  (`get_Completed`) stay skipped under those same keys. Async awaiting IS
  emitted: `put_Completed` lowers as `SetCompleted`, and every monomorphized
  `IAsyncOperationOf<X>` whose `SetCompleted` and `GetResults` both emitted
  — plus the plain `foundation.IAsyncAction` — gains a synthesized blocking
  `Await()` (`(<X>, error)` / `error`): register a Completed handler that
  sends the terminal AsyncStatus on a buffered channel, block, then
  `GetResults()` on Completed or a `winrt.AsyncError` (status + IAsyncInfo
  error code) otherwise. Race-free per the WinRT contract (a handler
  assigned after completion is invoked immediately) and deadlock-free (the
  send runs on the delegate runtime's fresh Invoke goroutine); Await blocks
  indefinitely by design — a context-aware variant is future work.
  Live-proven in `acceptance/async_test.go` (`GetFileFromPathAsync` →
  `Await`, including the already-completed and failure paths).
  Statics ARE emitted: each [Static] interface S on a class gets a
  package-level accessor `<S-minus-leading-I>()` (`CalendarIdentifiersStatics()`)
  that GetActivationFactory-fetches the class factory queried to S's IID and
  re-types it (caller owns the reference); statics-ONLY classes emit just
  their accessors (no class type). Factory ctors ARE emitted: each emitted
  method of a non-generic [Activatable] factory interface whose return is the
  class's default interface becomes a package-level func named after the
  method's projected name (`CreateGeographicRegion(code string) (*GeographicRegion, error)`;
  collisions take a factory-ordinal suffix) that fetches the factory,
  delegates to the already-generated factory-interface method, and re-types
  the result as the class — the factory interface itself stays emitted as a
  plain interface. The factory is fetched per call (a cache is a future
  optimization). Open generic types themselves, delegate-returning methods
  (`get_Completed`), delegate TypeDefs in their home namespace, arrays, and
  by-value structs wider than one integer word are still skipped with
  diagnostics.
- **Diagnostics ratchet**: `metadata/diagnostics-baseline.json` is the
  committed degradation set; `bindings --diagnostics-baseline` fails on any
  NEW diagnostic, and CI's regen job enforces byte-identical regeneration
  of the committed tree — the closure of the root namespaces pinned in
  `metadata/emit-roots.txt` (read when `--namespace` is absent; `--namespace`
  takes a comma-separated root list as an ad-hoc override). The roots are
  the FULL surface: every namespace in the ingested IR, listed explicitly
  (282 packages, ~571k generated lines) — regenerate the list after an
  ingest with the one-liner in the file's header. Go-keyword namespace
  leaves escape with a trailing underscore (Windows.Media.Import →
  package import_ in media/import_).
- **Never redeclare the ABI**: HSTRING, IInspectable, IActivationFactory,
  EventRegistrationToken, TrustLevel, and every Ro*/Windows* function come
  from `go-bindings-win32/bindings/win32/system/winrt` (import alias
  `syswinrt`); HRESULT/`ErrIfFailed`, GUID, IUnknown, UTF-16 helpers from
  `go-bindings-win32/bindings/runtime/win32` (alias `win32`).
- All Windows-facing files carry `//go:build windows && (amd64 || arm64)`
  (LLP64 pair; 386 excluded).

## Conventions

- WinRT calling convention: methods return HRESULT at the ABI; the logical
  return is the trailing `[out, retval]` param. IInspectable occupies vtable
  slots 0–5; an interface's first method is slot 6, in MethodDef order.
- Naming: `get_X` → `X()`, `put_X` → `SetX()`; plain methods keep their
  metadata names. Overloaded methods share a MethodDef name in metadata —
  the `[Overload]` attribute carries the unique name, which is what the Go
  method is called (e.g. `MonthAsFullString`, metadata name `MonthAsString`).
- The generated Calendar slots/IIDs are pinned against the committed winmd
  by `internal/verify` — a metadata bump or generator regression that
  reorders anything fails there, not in a corrupted live call.
- `metadata/winmd/` (two contract winmds + PROVENANCE.json) is committed;
  `go run ./cmd/generate fetch-metadata` refreshes it (`--version latest`
  for updates).
- Conventional commits, release-please, SHA-pinned actions, LF-normalized
  text (`.gitattributes`), `*.winmd` binary.
- See `docs/ROADMAP.md` for the landed/deferred state (composition, delegate
  returns, and progress handlers' Await stay deferred; events, statics,
  factory constructors, delegate-typed method params, and async awaiting are
  emitted). User-facing guides live in `docs/*.md`; runnable examples in
  `examples/` (indexed by `examples/README.md`).
