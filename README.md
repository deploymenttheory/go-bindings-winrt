# go-bindings-winrt

**Status: full surface shipping — every namespace in the ingested contract
winmds (282 packages, from `Windows.ApplicationModel` to
`Windows.Web.UI.Interop`) is generated, compiled, and committed;
`metadata/emit-roots.txt` pins the complete list.** Idiomatic Go bindings
for the
**Windows Runtime** (`Windows.*` namespaces: toasts/notifications,
Bluetooth LE, Windows Hello, geolocation, camera, `Windows.Management.*`
MDM/provisioning, …), the fourth member of the deploymenttheory Windows
bindings family:

- [go-winmd](https://github.com/deploymenttheory/go-winmd) — the shared
  ECMA-335 metadata reader (generics + event/property tables landed)
- [go-bindings-win32](https://github.com/deploymenttheory/go-bindings-win32) —
  the flat Win32 + COM surface (shipping); supplies this repo's ABI
  foundation (HSTRING, IInspectable, Ro* activation)
- [go-bindings-wdk](https://github.com/deploymenttheory/go-bindings-wdk) —
  the WDK / user-mode Native API surface (shipping)

What works today:

- `bindings/runtime/winrt` — Windows Runtime initialization, HSTRING
  lifecycle, runtime-class activation, and interface querying, proven by
  live tests.
- `bindings/winrt/...` — GENERATED from the pinned contract winmds
  (`go run ./cmd/generate bindings`, the full namespace list pinned in
  `metadata/emit-roots.txt`): interfaces with
  absolute vtable-slot dispatch, non-composable runtime classes (with
  constructors, statics accessors, and factory constructors), enums, value
  structs, events with typed Go handlers, async operations with a blocking
  `Await()`, and monomorphized generic instantiations — with a determinism
  gate and a diagnostics ratchet in CI.
  Live acceptance tests and the `examples/calendar` vertical run entirely
  over generated code, including the full toast pipeline (template XML →
  `XmlDocument` → `ToastNotification` → `ToastNotifier.Show`), a BLE
  advertisement-watcher scan cycle, `PackageManager` package queries, and
  `SpeechSynthesizer` voice enumeration.

Composition (composable classes) follows per
[docs/ROADMAP.md](docs/ROADMAP.md).

```go
import "github.com/deploymenttheory/go-bindings-winrt/bindings/winrt/globalization"

calendar, err := globalization.NewCalendar()
// calendar.SetToNow(), calendar.Year(), calendar.MonthAsFullString(), …
// Release when done
```

```go
import "github.com/deploymenttheory/go-bindings-winrt/bindings/winrt/ui/notifications"

statics, err := notifications.ToastNotificationManagerStatics()
doc, err := statics.GetTemplateContent(notifications.ToastTemplateTypeToastText01)
toast, err := notifications.CreateToastNotification(doc)
notifier, err := statics.CreateToastNotifierWithId("my.app.aumid")
err = notifier.Show(&toast.IToastNotification)
```

## Related projects

Part of the deploymenttheory Windows bindings family:

- [go-winmd](https://github.com/deploymenttheory/go-winmd) — the shared ECMA-335 `.winmd` metadata reader
- [go-bindings-win32](https://github.com/deploymenttheory/go-bindings-win32) — the Win32 API surface — functions, structs, enums, COM
- [go-bindings-wdk](https://github.com/deploymenttheory/go-bindings-wdk) — the Windows Driver Kit / user-mode Native API surface
- [go-bindings-wmi](https://github.com/deploymenttheory/go-bindings-wmi) — typed WMI/CIM classes
- **go-bindings-winrt** — the WinRT API surface (shipping) *(this repo)*

## License

[MIT](LICENSE).
