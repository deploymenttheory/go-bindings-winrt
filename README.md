# go-bindings-winrt

**Status: pre-work — no code yet.** This repository is the reserved home for
idiomatic Go bindings for the **Windows Runtime** (`Windows.*` namespaces:
toasts/notifications, Bluetooth LE, Windows Hello, geolocation, camera,
`Windows.Management.*` MDM/provisioning, …), the third member of the
deploymenttheory Windows bindings family:

- [go-winmd](https://github.com/deploymenttheory/go-winmd) — the shared
  ECMA-335 metadata reader
- [go-bindings-win32](https://github.com/deploymenttheory/go-bindings-win32) —
  the flat Win32 + COM surface (shipping)
- [go-bindings-wdk](https://github.com/deploymenttheory/go-bindings-wdk) —
  the WDK / user-mode Native API surface (shipping)

WinRT is a substantially larger lift than Win32/WDK — parameterized types,
events, activation factories, and a distinct calling convention — so this
repo starts when the prerequisites in [docs/ROADMAP.md](docs/ROADMAP.md)
land. The generator will be repo-specific (as in the sisters); only the
reader work is shared via go-winmd.

## Related projects

Part of the deploymenttheory Windows bindings family:

- [go-winmd](https://github.com/deploymenttheory/go-winmd) — the shared ECMA-335 `.winmd` metadata reader
- [go-bindings-win32](https://github.com/deploymenttheory/go-bindings-win32) — the Win32 API surface — functions, structs, enums, COM
- [go-bindings-wdk](https://github.com/deploymenttheory/go-bindings-wdk) — the Windows Driver Kit / user-mode Native API surface
- [go-bindings-wmi](https://github.com/deploymenttheory/go-bindings-wmi) — typed WMI/CIM classes
- **go-bindings-winrt** — WinRT bindings (in progress) *(this repo)*

## License

[MIT](LICENSE).
