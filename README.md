# go-bindings-winrt

Idiomatic Go bindings for the **Windows Runtime** — the `Windows.*` API
surface: toasts/notifications, Bluetooth LE, storage, speech,
`Windows.Management.*` MDM/provisioning, and everything else in the Windows
SDK contract winmds. **The full surface ships**: every namespace in the
ingested contracts is generated, compiled, and committed — 282 packages from
`Windows.ApplicationModel` to `Windows.Web.UI.Interop`, pinned in
[`metadata/emit-roots.txt`](metadata/emit-roots.txt).

Go strings in and out, `error` returns carrying real HRESULTs, typed event
handlers for Go functions, a blocking `Await()` on async operations, and
Go-implemented collection objects the OS can consume.

## Install

```sh
go get github.com/deploymenttheory/go-bindings-winrt@latest
```

Windows on amd64/arm64; every Windows-facing file carries
`//go:build windows && (amd64 || arm64)`.

## Quick start

A toast notification, end to end:

```go
import "github.com/deploymenttheory/go-bindings-winrt/bindings/winrt/ui/notifications"

statics, err := notifications.ToastNotificationManagerStatics()
defer statics.Release()
doc, err := statics.GetTemplateContent(notifications.ToastTemplateTypeToastText01)
defer doc.Release()
toast, err := notifications.CreateToastNotification(doc)
defer toast.Release()
notifier, err := statics.CreateToastNotifierWithId("my.app.aumid")
defer notifier.Release()
err = notifier.Show(&toast.IToastNotification)
```

Start with [docs/getting-started.md](docs/getting-started.md) for the mental
model (three layers, activation, Release discipline).

## Examples

Runnable programs under [`examples/`](examples), all verified live; each
degrades gracefully when hardware or identity is missing:

| Example | What it shows |
|---|---|
| [calendar](examples/calendar) | Direct activation vs a factory constructor consuming a Go-implemented `IIterable<String>` |
| [toast](examples/toast) | Template XML → DOM mutation → `ToastNotification` → notifier `Show`/`Hide` |
| [async](examples/async) | `GetFileFromPathAsync(...).Await()`, plus the failure path via `errors.As` |
| [events](examples/events) | A typed Go handler on `IMemoryBufferReference.Closed`: add, fire, remove, `Close` |
| [collections](examples/collections) | Consume an OS vector view; pass a Go-backed iterable into a factory |
| [bluetooth](examples/bluetooth) | Adapter capabilities; a ~3 s BLE advertisement scan via the typed `Received` handler |
| [packages](examples/packages) | `PackageManager` current-user package query |
| [speech](examples/speech) | Installed voices; synthesize a phrase to a stream and report its size |
| [mdmpolicy](examples/mdmpolicy) | `MdmAllowPolicy` statics reads |

## Documentation

- [Getting started](docs/getting-started.md) — install, the three-layer
  model, activation, statics, factory constructors, Release discipline.
- [Strings and memory](docs/strings-and-memory.md) — HSTRING lifecycle,
  refcount rules, the out-param heap invariant.
- [Async operations](docs/async.md) — `Await`, `AsyncError`/`errors.As`,
  IAsyncInfo, bounding waits.
- [Events and delegates](docs/events-and-delegates.md) — typed handlers,
  tokens, borrowed arguments, the execution model.
- [Collections](docs/collections.md) — monomorphized generics, iteration,
  Go-implemented collections.
- [The generator](docs/generator.md) — for contributors: pipeline,
  emit-roots, the diagnostics ratchet, the determinism gate.
- [Roadmap / state](docs/ROADMAP.md) — what is landed vs deferred.

## Capabilities

| Area | State |
|---|---|
| Interfaces, runtime classes, enums, value structs | **Emitted** — absolute vtable-slot dispatch, constructors, `As<Name>()` queries |
| Statics + factory constructors | **Emitted** — package-level accessors and `Create*` functions |
| Events | **Emitted** — `Add`/`Remove` accessors + typed Go handler constructors |
| Async | **Emitted** — synthesized blocking `Await()` on `IAsyncOperation<T>`/`IAsyncAction` and the `WithProgress` variants |
| Generic instantiations | **Emitted** — monomorphized per consuming package, pinterface-derived IIDs |
| Go-implemented collections | **Emitted + runtime** — element-generic `IIterable`/`IVectorView`/writable `IVector` with generated typed constructors (`New<IIterableOfX>`); `winrt.NewStringIterable` et al. remain |
| Composable classes (`Windows.UI.*` hierarchy) | **Emitted, instantiate-only** — `New<Class>` null-outer composable constructors, direct activation, statics, `As<Name>()` queries; Go-side derivation out of scope (inherited interfaces via `winrt.QueryInterface`) |
| Delegate-returning methods (`get_Completed`), arrays, float ABI, wide by-value structs, Go-implemented maps | Deferred — per-member skips tracked by the diagnostics ratchet |

Every degradation is per member, never per namespace: skipped members leave
`// slot N: name skipped: reason` comments and an entry in the committed
diagnostics baseline, and vtable slots never renumber.

## How it is built

The tree is generated from pinned `Microsoft.Windows.SDK.Contracts` winmds
through a committed IR (`go run ./cmd/generate fetch-metadata | ingest |
bindings`), with CI enforcing byte-identical regeneration and a diagnostics
ratchet that only ever shrinks. Details in
[docs/generator.md](docs/generator.md).

## Related projects

Part of the deploymenttheory Windows bindings family:

- [go-winmd](https://github.com/deploymenttheory/go-winmd) — the shared ECMA-335 `.winmd` metadata reader
- [go-bindings-win32](https://github.com/deploymenttheory/go-bindings-win32) — the Win32 API surface — functions, structs, enums, COM
- [go-bindings-wdk](https://github.com/deploymenttheory/go-bindings-wdk) — the Windows Driver Kit / user-mode Native API surface
- [go-bindings-wmi](https://github.com/deploymenttheory/go-bindings-wmi) — typed WMI/CIM classes
- **go-bindings-winrt** — the WinRT API surface (shipping) *(this repo)*

## License

[MIT](LICENSE).
