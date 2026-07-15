# go-bindings-winrt — state

What is landed and what is deliberately deferred. The original build-out plan
completed with the full-surface milestone (v0.2.0); this document replaces it
as the record. For pipeline mechanics see [generator.md](generator.md); for
the emit rulebook see [CLAUDE.md](../CLAUDE.md).

## Landed

All 2026-07, in dependency order:

| Milestone | PR |
|---|---|
| Hand-written runtime layer: `Initialize`, HSTRING lifecycle, activation, `QueryInterface` | #11 |
| Hand-written `Windows.Globalization.Calendar` vertical + pinned contract winmds (`Microsoft.Windows.SDK.Contracts`: FoundationContract + UniversalApiContract; no pre-merged `Windows.winmd` exists on NuGet — the Contracts package ships per-contract files) | #12 |
| Generator ingest stage: winmds → committed per-namespace IR, diagnostics pipeline | #13 |
| Generator emit stage: interfaces (absolute vtable-slot dispatch), non-composable runtime classes, enums, value structs | #14, #16 |
| Go-implemented delegate runtime (shared `NewCallback` vtables, pin registry, IAgileObject-answering QI) | #15 |
| Committed-IR regen tracking in CI (determinism gate + diagnostics ratchet) | #18 |
| Pinterface IID engine — derived IIDs for parameterized-type instantiations | #19 |
| Generic interface instantiations emitted as monomorphized per-package types (`IVectorViewOfString`) | #20 |
| Event emission: `Add`/`Remove` accessors + typed Go handler constructors | #21 |
| Statics accessors + factory constructors as package-level functions | #22 |
| `Windows.UI.Notifications` vertical: the full toast pipeline, live-tested | #23 |
| Go-implemented WinRT collections (`NewStringIterable`/`NewStringIterator`/`NewStringVectorView`) + stack-growth-safe callback dispatch (the inspectable worker) | #24 |
| Async awaiting: delegate-typed method params (`SetCompleted`) + synthesized blocking `Await()`, live-tested incl. already-completed and failure paths | #25 |
| Bluetooth + Management namespaces; the heap-escaped out-param invariant (`winrt.OutParam`) killing the GC stack-move flake | #26 |
| **Full surface: all 282 namespaces in the ingested IR emitted** (~571k lines), keyword-escaping packages (`media/import_`), collision-suffixed factory names, statics-only/composable name-claim fixes; `SpeechSynthesis` live-proven as a never-before-emitted namespace | #27 |
| v0.2.0 release | #17 |
| `Await()` on `IAsyncOperationWithProgress`/`IAsyncActionWithProgress` (progress via `SetProgress` + handler ctor, documented in [async.md](async.md)) | — |
| Element-generic Go-implemented collections: runtime core + codecs, writable `IVector<T>` (all 12 slots), generated `New<IIterableOfX>`-style constructors, inline nested-reentry dispatch on the inspectable worker | — |
| **Composition, instantiate-only**: composable classes emit like any other class (704 un-skipped — class types, statics, `As<Name>()` queries, direct activation), `[Composable]` factory methods become null-outer `New<Class>`/`New<Class>With<Suffix>` constructors, composable-class-typed params/returns upgrade from `IInspectable` to typed default-interface pointers | — |

Coverage is enforced structurally: the emit roots pin every IR namespace
explicitly, CI regenerates byte-identically, `internal/verify` pins slots and
IIDs against the committed winmd, and the live acceptance suite
(`acceptance/`) drives toasts, async, events, collections, Bluetooth,
`PackageManager`, MDM policy, and speech on real Windows.

## Deferred

Remaining gaps are **per-member degradations** tracked by the committed
diagnostics baseline (skipped members keep their slot comments; nothing
renumbers) — not missing namespaces:

- **Go-side derivation of composable classes** (subclassing a `Button` from
  Go — a non-null controlling outer with a Go-implemented overridable
  IInspectable): out of scope. Composition itself landed
  **instantiate-only** — every composable class is created and used through
  null-outer constructors; inherited (base-class) interfaces are reached
  with `winrt.QueryInterface`.
- **Context-aware Await** (`AwaitContext(ctx)`): Await blocks indefinitely by
  design today; bound it with the select idiom in [async.md](async.md).
  Related sharp edge, also documented there: a fully-parked quiet process can
  trip Go's deadlock detector while a native completion is in flight.
- **Go-implemented maps** (`IMap<K,V>`/`IMapView<K,V>`): no emittable member
  consumes one today. (Element-generic iterables/views and writable
  `IVector<T>` landed with the generated `New<IIterableOfX>`-style
  constructors; see [collections.md](collections.md).)
- **Delegate-returning methods** (`get_Completed`) and delegate TypeDefs in
  their home namespaces.
- **Arrays** (conformant arrays — includes `GetMany` on generated consumer
  types), **float parameters at the delegate ABI**, and **by-value structs
  wider than one integer word** in delegate signatures: the affected members
  skip; ~142 non-generic-delegate events across the surface light up as
  delegate adaptability grows.
- **Activation-factory caching**: factory constructors and statics accessors
  fetch the factory per call.
- **Informational success codes / event-ordering guarantees** beyond what
  WinRT contracts give: nothing curated yet.

## CI

`ci.yml` (build/test + the regeneration determinism gate with the
diagnostics ratchet), `go-lint.yml`, `linter.yml` (markdown among others),
`winmd-update.yml` (automated metadata refresh PRs), release-please with
conventional commits.
