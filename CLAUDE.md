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
go vet ./...
go test ./bindings/runtime/...   # live WinRT calls; needs Windows
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
- Naming: `get_X` → `X()`, `put_X` → `SetX()`; plain methods keep IDL names.
- Conventional commits, release-please, SHA-pinned actions, LF-normalized
  text (`.gitattributes`), `*.winmd` binary.
- See `docs/ROADMAP.md` for the wave plan and out-of-scope list
  (composition, statics, generic instantiations/pinterface IIDs, async).
