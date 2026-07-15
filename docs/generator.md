# The generator (for contributors)

Everything under `bindings/winrt/` is generated — ~571k lines across 282
packages — and CI proves it regenerates byte-identically. This guide is the
map for changing the generator or updating the metadata; users of the
bindings never need it.

## The pipeline

```text
NuGet contract winmds ──fetch-metadata──▶ metadata/winmd/*.winmd  (committed, + PROVENANCE.json)
                       ──ingest─────────▶ metadata/winrt/*.winrtmeta.json  (committed IR)
                       ──bindings───────▶ bindings/winrt/**  (committed Go)
```

```sh
go run ./cmd/generate fetch-metadata   # refresh the pinned winmds (--version latest to bump)
go run ./cmd/generate ingest           # winmds → per-namespace IR JSON
go run ./cmd/generate validate         # structural integrity checks over the IR
go run ./cmd/generate bindings \
  --diagnostics-baseline metadata/diagnostics-baseline.json   # regenerate the tree
go run ./cmd/generate diff --old <dir> --new <dir>            # semantic API diff between IR trees
go run ./cmd/generate list             # ingested namespaces with construct counts
```

Every stage's output is committed, so each is diffable in review and CI can
verify the chain end to end.

- **fetch** pins per-contract winmds from `Microsoft.Windows.SDK.Contracts`
  (`Windows.Foundation.FoundationContract` +
  `Windows.Foundation.UniversalApiContract`), recorded in
  `metadata/winmd/PROVENANCE.json`.
- **ingest** (`internal/winrtmeta` + `/ingest`) projects the winmds — read
  with [go-winmd](https://github.com/deploymenttheory/go-winmd) — into
  per-namespace IR files. Methods carry the LOGICAL signature; the HRESULT
  return and trailing `[out, retval]` lowering is emit's job.
- **bindings** (`internal/codegen`) loads the IR into a registry, severs
  import cycles, and emits per package through a strict gather → view →
  render split: `typemap/` is the one type-decision site, `emit/winrt/`
  gathers into pure-data view models, and `render/` executes embedded
  templates that only branch on precomputed kinds — templates never decide.
  The emitter is self-cleaning: it prunes only files bearing the DO-NOT-EDIT
  header.

## emit-roots.txt

`metadata/emit-roots.txt` is the single pinned definition of the generated
surface: `bindings` reads it when `--namespace` is not given (`--namespace
A,B` remains an ad-hoc override for experiments). The committed tree is these
roots plus the transitive closure of every namespace their emitted members
reference.

The list is currently the FULL surface — every namespace in the ingested IR,
listed explicitly so a winmd update that adds or removes a namespace shows up
as a reviewable diff. Regenerate it after an ingest with the one-liner in the
file's header:

```sh
ls metadata/winrt | sed 's/\.winrtmeta\.json$//' | LC_ALL=C sort
```

Go-keyword namespace leaves escape with a trailing underscore
(`Windows.Media.Import` → package `import_` in `media/import_`).

## The diagnostics ratchet

Not every metadata construct lowers to Go yet. Instead of failing or emitting
wrong code, the generator **degrades per member** and records a diagnostic
(e.g. `generic-member-skipped`, `event-delegate-unloweable`,
`import-cycle-skipped`, `GetMany skipped: conformant array`). Skipped members
never renumber vtable slots — they leave a `// slot N: name skipped: reason`
comment, so dispatch for everything else stays correct.

`metadata/diagnostics-baseline.json` is the committed degradation set.
`bindings --diagnostics-baseline` fails on any NEW diagnostic: a generator
change may only ever shrink the set (delete the newly-resolved entries from
the baseline in the same PR) — never silently grow it. That is the ratchet.

## The determinism gate

CI's `regen` job (`.github/workflows/ci.yml`) reruns
`go run ./cmd/generate bindings --diagnostics-baseline ...` and fails on any
dirty file: the committed tree must regenerate **byte-identically** from the
committed IR. Practical consequences:

- Never hand-edit generated files; change the generator and regenerate.
- Any generator change must be accompanied by the regenerated tree in the
  same commit, or the gate fails.
- Map iteration and other ordering hazards inside the generator must be
  sorted; nondeterminism shows up as an unreproducible diff here.

A second guard, `internal/verify`, pins generated Calendar slots/IIDs (and
the runtime collection IIDs) against a fresh derivation from the committed
winmd — a metadata bump or slot-numbering regression fails there rather than
as a corrupted live call.

## How a winmd update flows

1. `go run ./cmd/generate fetch-metadata --version latest` — updates
   `metadata/winmd/` and `PROVENANCE.json`.
2. `go run ./cmd/generate ingest` — the IR diff is the reviewable API delta
   (`generate diff --old --new` summarizes it semantically).
3. Regenerate `metadata/emit-roots.txt` with the one-liner if namespaces
   appeared or vanished.
4. `go run ./cmd/generate bindings --diagnostics-baseline
   metadata/diagnostics-baseline.json` — new API may introduce new
   diagnostics; triage each: either extend the generator or accept the
   degradation by adding it to the baseline deliberately.
5. `go build ./... && go test ./internal/... ./acceptance/...` — the
   slot/IID guards and live acceptance tests are the last word.

(`.github/workflows/winmd-update.yml` automates the fetch-and-PR part of
this.)

## Emit rules worth knowing before changing anything

- Slot = 6 + MethodDef index (IInspectable occupies 0–5); `[Overload]`
  attribute names win over metadata names (`MonthAsFullString`).
- Classes — `[Composable]` ones included — embed their default interface by
  value; other interfaces get `As<Name>()` queries.
- Statics interfaces → package-level accessors; `[Activatable]` factory
  methods returning the class default interface → package-level constructors
  (bare names that collide across classes gain the class name).
- `[Composable]` factory methods ending with the (baseInterface in,
  innerInterface out) Object pair and returning the class default interface
  → null-outer `New<Class>`/`New<Class>With<Suffix>` constructors
  (instantiate-only composition; the returned non-nil inner is Released as a
  second reference to the same object). Shape failures record
  `composable-factory-skipped`; factory-less composables emit type + queries
  + statics with no diagnostic.
- Closed generic instantiations monomorphize into the CONSUMING package —
  two packages get two identical-ABI copies by design.
- The ABI layer (HSTRING, IInspectable, Ro* functions, HRESULT, GUID) comes
  from go-bindings-win32 and is **never redeclared** here.

The full rulebook lives in [CLAUDE.md](../CLAUDE.md); the state of what is
landed vs deferred in [ROADMAP.md](ROADMAP.md).
