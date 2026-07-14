# go-bindings-winrt

**Status: generator online — generated bindings for `Windows.Globalization`
(+ its `Windows.Foundation` closure) shipping.** Idiomatic Go bindings for
the **Windows Runtime** (`Windows.*` namespaces: toasts/notifications,
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
- `bindings/winrt/globalization` + `bindings/winrt/foundation` — GENERATED
  from the pinned contract winmds (`go run ./cmd/generate bindings
  --namespace Windows.Globalization`): interfaces with absolute vtable-slot
  dispatch, non-composable runtime classes, enums, and value structs, with
  a determinism gate and a diagnostics ratchet in CI. Live acceptance tests
  and the `examples/calendar` vertical run entirely over generated code.

Wider namespace coverage, events/delegates, statics, and factory
constructors follow per [docs/ROADMAP.md](docs/ROADMAP.md).

```go
import "github.com/deploymenttheory/go-bindings-winrt/bindings/winrt/globalization"

calendar, err := globalization.NewCalendar()
// calendar.SetToNow(), calendar.Year(), calendar.MonthAsFullString(), …
// Release when done
```

## Related projects

Part of the deploymenttheory Windows bindings family:

- [go-winmd](https://github.com/deploymenttheory/go-winmd) — the shared ECMA-335 `.winmd` metadata reader
- [go-bindings-win32](https://github.com/deploymenttheory/go-bindings-win32) — the Win32 API surface — functions, structs, enums, COM
- [go-bindings-wdk](https://github.com/deploymenttheory/go-bindings-wdk) — the Windows Driver Kit / user-mode Native API surface
- [go-bindings-wmi](https://github.com/deploymenttheory/go-bindings-wmi) — typed WMI/CIM classes
- **go-bindings-winrt** — WinRT bindings (in progress) *(this repo)*

## License

[MIT](LICENSE).
