# go-bindings-winrt — roadmap and prerequisites

Research snapshot (2026-07). Sources: the WinMD file format reference
(learn.microsoft.com/uwp/winrt-cref/winmd-files), windows-rs's metadata
handling, CsWinRT.

## Metadata source

- **Consume the pre-merged `Windows.winmd`** from the
  `Microsoft.Windows.SDK.Contracts` NuGet package (what windows-rs uses),
  rather than merging the per-contract winmds under
  `C:\Program Files (x86)\Windows Kits\10\References\<ver>\` or the
  per-namespace files in `C:\Windows\System32\WinMetadata\` ourselves
  (Microsoft's `mdmerge` produces the union).
- Same ECMA-335 physical format as win32metadata, with WinRT-specific rules:
  version string "Windows Runtime 1.2", `tdWindowsRuntime` flag on every
  public type, and **TypeRef indirection everywhere** (system winmds never
  reference TypeDefs directly, even same-file — required for projection
  substitutions like `IVector<T>` → `IList<T>`).
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
- **Delegates** (TypeDef extending `System.MulticastDelegate`, `Invoke`
  method, `[Guid]`), **events** returning `EventRegistrationToken`.
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
   then composition/statics.
4. First namespace targets, chosen for product value:
   `Windows.Management.*` (MDM enrollment/provisioning),
   `Windows.UI.Notifications` (toasts), `Windows.Devices.Bluetooth`.

## CI note

The Go lint/build workflows are disabled (`.github/workflows/go-lint.yml.disabled`)
until this repository has a `go.mod` and Go code. Re-enable them when the
generator lands.
