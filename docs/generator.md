# The generator (for contributors)

Everything under `bindings/winrt/` is generated — about 571,000 lines across
282 packages — from Windows metadata. This guide is for changing the
generator or updating the metadata. If you are only calling the bindings,
you do not need it.

## The three commands

Metadata goes through three stages, and the output of each is committed to
the repository:

```text
NuGet contract winmds ──fetch-metadata──▶ metadata/winmd/*.winmd
                       ──ingest─────────▶ metadata/winrt/*.winrtmeta.json
                       ──bindings───────▶ bindings/winrt/**
```

```sh
go run ./cmd/generate fetch-metadata   # download the pinned winmds (--version latest to bump)
go run ./cmd/generate ingest           # winmd → one JSON file per namespace
go run ./cmd/generate validate         # check the JSON for structural problems
go run ./cmd/generate bindings \
  --diagnostics-baseline metadata/diagnostics-baseline.json   # JSON → Go
go run ./cmd/generate diff --old <dir> --new <dir>            # compare two metadata trees
go run ./cmd/generate list             # namespaces and how many types each holds
```

Committing every stage means each one shows up as a reviewable diff, and CI
can check the whole chain rather than just the end of it.

**fetch-metadata** downloads the winmds for the two API contracts this repo
projects — `Windows.Foundation.FoundationContract` and
`Windows.Foundation.UniversalApiContract` — from the
`Microsoft.Windows.SDK.Contracts` NuGet package, and records the package
version and SHA-256 of each file in `metadata/winmd/PROVENANCE.json`.

**ingest** (`internal/winrtmeta`) reads those winmds with
[go-winmd](https://github.com/deploymenttheory/go-winmd) and writes one JSON
file per namespace. The JSON records each method's *logical* signature — the
return type a Go caller sees. Converting that to the actual ABI signature
(HRESULT return, value written through a trailing `[out, retval]` pointer)
happens later, in `bindings`.

**bindings** (`internal/codegen`) reads the JSON and writes Go. It runs in
three stages, and the split is deliberate:

| Stage | Package | Responsibility |
|---|---|---|
| Decide types | `typemap/` | The only place that decides what a metadata type becomes in Go, and how it crosses the ABI. |
| Collect | `emit/winrt/` | Ask `typemap` about everything, and build plain data structures describing what to write. |
| Write | `render/` | Run Go templates over those structures. |

The templates contain no decisions — they only read fields that the collect
stage already worked out and choose between fixed alternatives. Anything
resembling a judgement call belongs in `typemap`, so there is one place to
look when output is wrong.

The generator also deletes generated files that no longer correspond to
anything in the metadata. It only ever deletes files carrying the
`DO-NOT-EDIT` header, so a hand-written file dropped into the tree is safe.

## Which namespaces get generated

`metadata/emit-roots.txt` lists them. `bindings` reads it unless you pass
`--namespace A,B`, which is an override for experiments rather than
something the committed tree depends on.

The list currently names every namespace in the ingested metadata, written
out in full rather than inferred, so that a metadata update which adds or
removes a namespace appears as a line in a diff. The generated tree is those
namespaces plus everything they reference.

Regenerate the list after an ingest with the command in the file's header:

```sh
ls metadata/winrt | sed 's/\.winrtmeta\.json$//' | LC_ALL=C sort
```

A namespace whose last segment is a Go keyword gets a trailing underscore:
`Windows.Media.Import` becomes package `import_` in `media/import_`.

## When a member cannot be generated

Some metadata has no reasonable Go equivalent yet — open generic types,
arrays, methods returning delegates. Rather than fail the build or emit
something that does not work, the generator skips that one member, records
why, and carries on.

A skipped member never changes the vtable slot numbers of the members around
it. It leaves a comment in its place:

```go
// slot 11: get_Clip skipped: reference to Windows.UI.Xaml.Media.RectangleGeometry crosses a severed import edge
```

so calls to every other method on that interface stay correct.

`metadata/diagnostics-baseline.json` is the committed list of everything
currently skipped. Running `bindings --diagnostics-baseline` fails if a
member is skipped that is not already on the list. The list may shrink — if
your change makes something generatable, delete its entry in the same pull
request — but it may not grow without you noticing.

## Regeneration must be reproducible

CI's `regen` job re-runs `bindings` and fails if any file changes: the
committed tree has to regenerate exactly, byte for byte, from the committed
JSON. In practice that means:

- Never edit a generated file. Change the generator and regenerate.
- A generator change and its regenerated output belong in the same commit.
- Anything with unstable ordering inside the generator — iterating a Go map,
  for instance — must be sorted first, or it shows up here as a diff nobody
  can reproduce.

`internal/verify` is a second check: it recalculates the vtable slot numbers
and interface IDs for `Calendar` (and the runtime's collection IIDs) straight
from the committed winmd and compares them against what was generated. A
metadata bump or a slot-numbering bug fails there, rather than as a call into
the wrong function at runtime.

## Updating to newer metadata

1. `go run ./cmd/generate fetch-metadata --version latest` — updates
   `metadata/winmd/` and `PROVENANCE.json`.
2. `go run ./cmd/generate ingest` — the resulting JSON diff *is* the API
   change; `generate diff --old --new` summarises it in readable form.
3. Regenerate `metadata/emit-roots.txt` if namespaces appeared or vanished.
4. `go run ./cmd/generate bindings --diagnostics-baseline metadata/diagnostics-baseline.json`
   — new APIs may not all be generatable. Look at each new skip and decide:
   teach the generator, or add the entry to the baseline on purpose.
5. `go build ./... && go test ./internal/... ./acceptance/...` — the slot and
   IID checks plus the tests that make real WinRT calls have the last word.

`.github/workflows/winmd-update.yml` does steps 1 and 2 automatically and
opens a pull request.

## Rules to know before changing the emitter

- **Vtable slots.** `IInspectable` occupies slots 0–5, so an interface's
  first method is slot 6, numbered in MethodDef order. Where the `[Overload]`
  attribute gives a method a distinct name, that name wins over the metadata
  name (`MonthAsFullString`, not `MonthAsString`).
- **Classes** embed their default interface directly, so its methods are
  available without a cast. Other interfaces the class implements are reached
  through generated `As<Name>()` methods.
- **Statics and constructors.** A `[Static]` interface becomes a
  package-level accessor; an `[Activatable]` factory method that returns the
  class's default interface becomes a package-level constructor. Where a bare
  constructor name would collide across classes, the class name is appended.
- **Composable classes** can be created but not derived from. A `[Composable]`
  factory method whose last two parameters are the COM aggregation pair
  (controlling outer in, inner out) becomes a `New<Class>` constructor that
  passes a null outer. Writing a Go type that derives from a WinRT class is
  not supported. A class with no factory at all — `SpriteVisual` — is a
  normal platform shape, not an error.
- **Generic interfaces** are generated once per closed instantiation, into
  the package that uses it: `IVectorView<String>` becomes
  `IVectorViewOfString`. Two packages using the same instantiation each get
  their own copy — different Go types, identical ABI.
- **The ABI layer is never redeclared here.** `HSTRING`, `IInspectable`, the
  `Ro*` activation functions, `HRESULT`, and `GUID` all come from
  go-bindings-win32.
- **amd64 only.** See the architecture notes in [CLAUDE.md](../CLAUDE.md) for
  why arm64 is excluded rather than generated for.

The complete rules live in [CLAUDE.md](../CLAUDE.md); what is done and what
is deferred is in [ROADMAP.md](ROADMAP.md).
