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
go run ./cmd/generate bindings --namespace Windows.Globalization \
  --diagnostics-baseline metadata/diagnostics-baseline.json      # regenerate the committed tree
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
    interface pointer; delegates/generics/arrays degrade the member.
    External map (never re-emitted): Windows.Foundation
    EventRegistrationToken → syswinrt, HResult → int32.
  - `emit/winrt/` — gather → view → render with the render firewall:
    gather resolves everything through the typemap into pure-data view
    models; `render/` executes `//go:embed` templates that only branch on
    precomputed kinds, never decide. Emits per package (non-empty only):
    doc.go, `<pkg>_enums.go`, `<pkg>_structs.go`, `<pkg>_interfaces.go`,
    `<pkg>_classes.go`.
  - `shared/fileasm/` — DO-NOT-EDIT header + build tag + go/format; the
    emitter is self-cleaning and only ever prunes files bearing the header.
- **Emit rules**: slot = 6 + MethodDef index; skipped members NEVER
  renumber — they leave `// slot N: name skipped: reason` comments.
  Classes (non-composable) embed their default interface by value;
  direct-activatable classes get `NewFoo()`; other instance interfaces get
  `As<Name>()` query methods. Events, delegates, statics, factory ctors,
  generics, arrays, and by-value structs wider than one integer word are
  skipped with diagnostics this wave.
- **Diagnostics ratchet**: `metadata/diagnostics-baseline.json` is the
  committed degradation set; `bindings --diagnostics-baseline` fails on any
  NEW diagnostic, and CI's regen job enforces byte-identical regeneration
  of the committed tree (the Windows.Globalization closure).
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
- See `docs/ROADMAP.md` for the wave plan and out-of-scope list
  (composition, statics, generic instantiations/pinterface IIDs, async).
