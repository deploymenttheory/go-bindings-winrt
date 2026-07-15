# go-bindings-winrt

**Status: bootstrapping — runtime layer landed.** Idiomatic Go bindings for
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

What works today: `bindings/runtime/winrt` — Windows Runtime
initialization, HSTRING lifecycle, runtime-class activation, and interface
querying, proven by live tests. The hand-written
`Windows.Globalization.Calendar` vertical and the generator follow per
[docs/ROADMAP.md](docs/ROADMAP.md).

```go
import winrt "github.com/deploymenttheory/go-bindings-winrt/bindings/runtime/winrt"

inspectable, err := winrt.ActivateInstance("Windows.Globalization.Calendar")
// query to a typed interface with winrt.QueryInterface[T], call methods,
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
