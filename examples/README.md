# Examples

Runnable, self-contained programs, one per directory — `go run` any of them
from the repository root. All build with `//go:build windows && (amd64 ||
arm64)` and degrade gracefully where hardware or identity is missing (no
Bluetooth radio, no package identity for toasts): they print the situation
instead of crashing.

| Example | What it shows | Run |
|---|---|---|
| [calendar](calendar) | Direct activation vs a factory constructor consuming a Go-implemented `IIterable<String>`; dates through four calendar systems | `go run ./examples/calendar` |
| [toast](toast) | The toast pipeline: template XML → DOM mutation → `ToastNotification` → notifier `Show`/`Hide` | `go run ./examples/toast` |
| [async](async) | `GetFileFromPathAsync(...).Await()`, plus the failure path surfacing the HRESULT via `errors.As` | `go run ./examples/async` |
| [events](events) | Subscribe a typed Go handler to `IMemoryBufferReference.Closed`, watch it fire, unregister, `Close` | `go run ./examples/events` |
| [collections](collections) | Both directions: read an OS vector view (`ApplicationLanguages`), pass a Go-backed iterable into a factory | `go run ./examples/collections` |
| [bluetooth](bluetooth) | Adapter capabilities; with a powered-on BLE radio, a ~3 s advertisement scan via the typed `Received` handler | `go run ./examples/bluetooth` |
| [packages](packages) | `PackageManager` current-user package query, names and versions through `IIterable<Package>` | `go run ./examples/packages` |
| [speech](speech) | Enumerate installed voices; synthesize a phrase to an in-memory stream and report its size | `go run ./examples/speech` |
| [mdmpolicy](mdmpolicy) | `MdmAllowPolicy` statics reads (browser/camera/account/store allow policies) | `go run ./examples/mdmpolicy` |

Guides live in [docs/](../docs): [getting started](../docs/getting-started.md),
[strings and memory](../docs/strings-and-memory.md),
[async](../docs/async.md), [events](../docs/events-and-delegates.md),
[collections](../docs/collections.md).
