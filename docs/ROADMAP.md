# go-bindings-winrt — roadmap and prerequisites

Research snapshot (2026-07). Sources: the WinMD file format reference
(learn.microsoft.com/uwp/winrt-cref/winmd-files), windows-rs's metadata
handling, CsWinRT.

## Metadata source

- **Consume per-contract winmds from `Microsoft.Windows.SDK.Contracts`**
  (pinned in `metadata/winmd/PROVENANCE.json`, fetched by
  `go run ./cmd/generate fetch-metadata`):
  `ref/netstandard2.0/Windows.Foundation.FoundationContract.winmd` +
  `ref/netstandard2.0/Windows.Foundation.UniversalApiContract.winmd`.
  *(Correction to the original plan: there is **no** pre-merged
  `Windows.winmd` on NuGet — the Contracts package ships ~94 per-contract
  files and its `Windows.WinMD` entry is a type-forwarder facade;
  windows-rs's merged file is GitHub-only with its own filtering policy.
  UniversalApiContract carries the entire roadmap target surface. If ingest
  ever hits a TypeRef into a third contract, pin that file as an additional
  PROVENANCE record.)*
- Same ECMA-335 physical format as win32metadata, with WinRT-specific rules:
  version string `WindowsRuntime 1.4` (in the 26100 contracts),
  `tdWindowsRuntime` flag on every public type, and **TypeRef indirection
  everywhere** (system winmds never reference TypeDefs directly, even
  same-file — required for projection substitutions like `IVector<T>` →
  `IList<T>`).
- **Overloads**: overloaded methods share their MethodDef *name* (two
  `MonthAsString` rows); the `[Overload]` attribute carries the unique name
  (`MonthAsFullString`). Projected Go names use the unique overload name.
- Scale: thousands of types across ~50+ `Windows.*` namespaces.

## Reader prerequisites (land in go-winmd, versioned + additive)

The Win32/WDK metadata contains none of these (tripwire tests in go-winmd's
consumers prove it), so they are pure additions:

1. **Generics engine** — decode `ELEMENT_TYPE_GENERICINST` / `VAR` / `MVAR`
   in signature blobs (§II.23.2.12), materialize the `GenericParam`
   (§II.22.20) and `TypeSpec` (§II.23.2.14) tables, handle arity-backtick
   names (`IVector`1`).
2. **Event/property tables** — materialize `Event`/`EventMap`,
   `Property`/`PropertyMap`, `MethodSemantics` (`add_`/`remove_`,
   `get_`/`put_` pairing).
3. **PropertySig** decoding (0x08 marker).
4. `InterfaceImpl` targets that are TypeSpecs (generic instantiations).

## Generator/runtime prerequisites (this repo, repo-specific)

- **Calling convention**: WinRT methods return HRESULT implicitly — the
  metadata signature's return is the `[out, retval]` param; the emitter must
  synthesize HRESULT + trailing out-param at the ABI layer.
- **HSTRING** — `ELEMENT_TYPE_STRING` means HSTRING; runtime needs
  create/delete/read helpers (`WindowsCreateString` et al., available via
  go-bindings-win32's `system/winrt` surface).
- **IInspectable** root (GetIids/GetRuntimeClassName/GetTrustLevel) atop
  IUnknown; `RoInitialize`/`RoGetActivationFactory` bootstrap.
- **Runtime class model** driven by attributes: `[Activatable]` (direct or
  factory), `[Static]` (static interfaces surfaced on the class),
  `[Composable]` (inheritance; hidden controlling-IInspectable factory
  params), `[Default]` interface per class, `[ExclusiveTo]`,
  `[Overload]`/`[DefaultOverload]`.
  *Status: statics and factory constructors are landed. `[Static]`
  interfaces project as package-level accessors
  (`CalendarIdentifiersStatics()` — the activation factory queried to the
  statics IID; statics-only classes emit just their accessors, no class
  type), and each emitted method of a non-generic `[Activatable]` factory
  interface returning the class default interface projects as a
  package-level constructor (`CreateGeographicRegion("US")`, live-tested;
  the factory is fetched per call — a cache is a future optimization).
  Calendar's factory ctors emit too; their `IIterable<String>` argument
  (which the OS rejects as null with E_POINTER, verified live) is now
  constructible — Go-implemented WinRT collections landed in
  `bindings/runtime/winrt` (`inspectable.go` + `collections.go`:
  `NewStringIterable`/`NewStringIterator`/`NewStringVectorView` over the
  shared IInspectable trampoline core, IIDs pinned to the pinterface
  derivation), and `CreateCalendarDefaultCalendarAndClock`/
  `CreateCalendarWithTimeZone` are live-tested consuming a Go-backed
  iterable in `acceptance/collections_test.go` — finding resolved.
  `[Composable]` classes stay skipped.*
- **Delegates** (TypeDef extending `System.MulticastDelegate`, `Invoke`
  method, `[Guid]`), **events** returning `EventRegistrationToken`.
  *Status: the Go-implemented delegate runtime
  (`bindings/runtime/winrt/delegate.go` — shared NewCallback vtables, pin
  registry, IAgileObject-answering QI) is landed and live-tested against
  `MediaProtectionManager.RebootNeeded`. The pinterface IID engine
  (`internal/codegen/pinterface`) and generic INTERFACE instantiation
  emission are landed: closed instantiations referenced by emittable
  members monomorphize into the consuming package's `<pkg>_pinterfaces.go`
  with derived IIDs (`Calendar.Languages()` → `IVectorViewOfString`,
  live-tested). Generator **event emission** is landed: add_/remove_
  accessors project as `Add<Event>`/`Remove<Event>`, and each event's
  delegate — a generic instantiation like
  `TypedEventHandler`2<IMemoryBufferReference, Object>` (IID
  pinterface-derived) or a non-generic delegate (declared IID) — grounds
  into a package-local typed handler in `<pkg>_delegates.go` wrapping the
  delegate runtime (live-tested: `IMemoryBufferReference.Closed` fires
  through generated code). Delegate-typed method PARAMETERS are landed:
  the same grounding (and adaptability rules) serves method params via the
  typemap's `RequestDelegate` seam, so `put_Completed` emits as
  `SetCompleted(handler *<Handler>)` (nil passes NULL) — which unlocked
  **async awaiting**: every monomorphized `IAsyncOperationOf<X>` whose
  `SetCompleted` and `GetResults` both emitted, plus the plain
  `IAsyncAction`, gains a synthesized blocking `Await()` returning
  `GetResults()` on completion or a `winrt.AsyncError` (AsyncStatus + the
  IAsyncInfo error code as a `win32.HRESULT`) otherwise. Live-tested in
  `acceptance/async_test.go`: `StorageFile.GetFileFromPathAsync` awaited
  end to end, including the already-completed (immediate handler invoke)
  and failure (0x80070002 file-not-found surfaced through `errors.As`)
  paths. Methods RETURNING delegates (`get_Completed`), delegate TypeDefs
  in their home namespaces, and Await on `IAsyncOperationWithProgress`
  (its Completed/Progress handler setters emit; the Await synthesis
  targets only `IAsyncOperation`1`/`IAsyncAction`) stay deferred, as does
  a context-aware Await variant; the ~142 non-generic-delegate events
  across the wider surface light up when their namespaces are emitted.*
- **mscorlib marker types** (`System.Object`, `System.Guid`, `System.Enum`,
  `System.ValueType`, `System.MulticastDelegate`, `System.Attribute`) are
  type-system signals only — never resolve them as real types.
- Enums are Int32/UInt32 only (`[Flags]` on UInt32).

## Suggested sequencing

1. go-winmd generics + event/property tables (with brute-force tests over a
   pinned `Windows.winmd`).
2. Minimal runtime: HSTRING, IInspectable, activation, one hand-written
   vertical (e.g. `Windows.Globalization.Calendar` — the canonical sample).
3. Generator for interfaces + runtime classes without composition; events;
   statics + factory constructors (landed); then composition.
4. First namespace targets, chosen for product value:
   `Windows.Management.*` (MDM enrollment/provisioning),
   `Windows.UI.Notifications` (toasts) **(landed — the first namespace
   target shipped: the committed tree is the closure of
   {`Windows.Globalization`, `Windows.UI.Notifications`}, pulling in
   `Windows.Data.Xml.Dom`, `Windows.System`, `Windows.Storage`, and
   `Windows.ApplicationModel`; the toast pipeline — template XML →
   `XmlDocument` → `ToastNotification` → `ToastNotifier.Show`/`Hide` — is
   live-tested end to end in `acceptance/toast_test.go`)**,
   `Windows.Devices.Bluetooth`.

## CI note

The Go lint (`go-lint.yml`) and build/test (`ci.yml`) workflows are active.
The regeneration-determinism CI job is added when the generator's emit
stage lands.
