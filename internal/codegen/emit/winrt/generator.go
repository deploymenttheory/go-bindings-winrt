// Package emitwinrt emits the WinRT bindings: one Go package per Windows.*
// namespace under bindings/winrt/, with interfaces dispatching through
// syscall.SyscallN and non-composable runtime classes embedding their
// default interface. The emit pipeline is gather (this package's builders,
// which resolve everything through the typemap) → view (pure data) → render
// (templates that never decide).
package emitwinrt

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/deploymenttheory/go-bindings-winrt/internal/codegen/emit/winrt/render"
	"github.com/deploymenttheory/go-bindings-winrt/internal/codegen/emit/winrt/view"
	"github.com/deploymenttheory/go-bindings-winrt/internal/codegen/naming"
	"github.com/deploymenttheory/go-bindings-winrt/internal/codegen/pipeline"
	"github.com/deploymenttheory/go-bindings-winrt/internal/codegen/shared/fileasm"
	"github.com/deploymenttheory/go-bindings-winrt/internal/codegen/typemap"
	"github.com/deploymenttheory/go-bindings-winrt/internal/winrtmeta"
)

// Generator emits the bindings tree for a loaded Registry.
type Generator struct {
	registry *pipeline.Registry
	mapper   *typemap.Mapper
	// outDir is the bindings/winrt output root.
	outDir string

	// claimedNames tracks package-level identifiers in the namespace being
	// emitted, preventing collisions between types, enum members, IID vars,
	// and constructors.
	claimedNames map[string]bool
	// typeNames pre-claims all type names before any value names, so an
	// enum member or IID var can never steal a name a type needs.
	typeNames map[string]bool

	// Per-namespace generic-instantiation state (reset alongside the name
	// claims): pinstByName maps the mangled name to the grounded
	// instantiation, pinstIID to its derived pinterface IID, and pinstQueue
	// is the worklist buildPinterfaceModels drains to a fixed point.
	pinstByName map[string]*winrtmeta.TypeRef
	pinstIID    map[string]string
	pinstQueue  []string

	// Per-namespace event-delegate state (reset alongside the name claims):
	// pdelByName maps the handler type name to the delegate reference it was
	// grounded from (dedup), pdelModels accumulates the handler render
	// models (built eagerly at request time), and pdelImports collects the
	// <pkg>_delegates.go import edges.
	pdelByName  map[string]*winrtmeta.TypeRef
	pdelModels  []view.DelegateModel
	pdelImports typemap.ImportSet

	// ifaceMethods records each built interface's emitted method surface by
	// MethodDef index (full metadata name → per-slot records), reset per
	// namespace. The factory-constructor gather consults it so package-level
	// wrappers always mirror the generated interface methods exactly.
	ifaceMethods map[string][]emittedMethod

	// writtenFiles records every path this run produced, so stale generated
	// files from earlier runs can be pruned afterwards.
	writtenFiles map[string]bool

	// referenced collects the namespaces emitted code actually imports —
	// the transitive-closure worklist. Only imports that survive pruning
	// count, so skipped members never pull a namespace in.
	referenced map[string]bool

	// Diagnostics collects all degradations and skips (ratchet input) as
	// "key: detail" strings.
	Diagnostics []string
}

// New builds a Generator. Import cycles among namespaces are computed up
// front; references along severed edges degrade instead of importing.
func New(registry *pipeline.Registry, modulePath, outDir string) *Generator {
	return &Generator{
		registry: registry,
		mapper: &typemap.Mapper{
			Registry:   registry,
			ModulePath: modulePath,
			Blocked:    pipeline.ComputeBlockedImports(registry),
		},
		outDir: outDir,
	}
}

// EmitAll generates every loaded namespace (or, when filter is non-empty,
// the filter set plus the transitive closure of namespaces its EMITTED
// members reference — generated packages must always compile). Returns the
// package count.
func (g *Generator) EmitAll(filter map[string]bool) (int, error) {
	g.writtenFiles = map[string]bool{}
	g.referenced = map[string]bool{}
	emitted := map[string]bool{}
	pending := make([]string, 0, len(g.registry.Namespaces))
	if len(filter) > 0 {
		for namespace := range filter {
			pending = append(pending, namespace)
		}
		sort.Strings(pending)
	} else {
		for _, meta := range g.registry.Namespaces {
			pending = append(pending, meta.Namespace)
		}
		sort.Strings(pending)
	}

	for len(pending) > 0 {
		namespace := pending[0]
		pending = pending[1:]
		if emitted[namespace] {
			continue
		}
		meta := g.registry.ByNamespace[namespace]
		if meta == nil {
			return len(emitted), fmt.Errorf("referenced namespace %s not in loaded metadata (re-run ingest without a filter)", namespace)
		}
		emitted[namespace] = true
		if err := g.emitNamespace(meta); err != nil {
			return len(emitted), fmt.Errorf("emitting %s: %w", namespace, err)
		}
		// Chase the namespaces this one's emitted code referenced.
		var discovered []string
		for referenced := range g.referenced {
			if !emitted[referenced] {
				discovered = append(discovered, referenced)
			}
		}
		sort.Strings(discovered)
		pending = append(pending, discovered...)
	}
	// Remove generated files from earlier runs that this run did not
	// rewrite (renamed constructs, removed namespaces). A filtered run
	// prunes only inside the packages it emitted; a full run sweeps the
	// whole output tree.
	if err := g.pruneStale(len(filter) == 0); err != nil {
		return len(emitted), err
	}
	sort.Strings(g.Diagnostics)
	return len(emitted), nil
}

// pruneStale deletes generated .go files not written by this run, then
// removes directories left empty. Only files carrying the DO-NOT-EDIT
// header are ever touched.
func (g *Generator) pruneStale(fullSweep bool) error {
	if _, err := os.Stat(g.outDir); err != nil {
		return nil // nothing emitted yet
	}
	emittedDirs := map[string]bool{}
	for path := range g.writtenFiles {
		emittedDirs[filepath.Dir(path)] = true
	}
	var emptied []string
	err := filepath.WalkDir(g.outDir, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			emptied = append(emptied, path)
			return nil
		}
		if !strings.HasSuffix(path, ".go") || g.writtenFiles[path] {
			return nil
		}
		if !fullSweep && !emittedDirs[filepath.Dir(path)] {
			return nil
		}
		content, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		if !strings.HasPrefix(string(content), fileasm.Header) {
			return nil // never touch hand-written files
		}
		return os.Remove(path)
	})
	if err != nil {
		return err
	}
	// Deepest-first: removing empty leaves may empty their parents.
	sort.Sort(sort.Reverse(sort.StringSlice(emptied)))
	for _, dir := range emptied {
		if entries, readErr := os.ReadDir(dir); readErr == nil && len(entries) == 0 {
			_ = os.Remove(dir)
		}
	}
	return nil
}

// emitNamespace writes one namespace's package: doc.go plus the per-construct
// files (enums, structs, interfaces, classes, pinterfaces, delegates), only
// when non-empty.
func (g *Generator) emitNamespace(meta *winrtmeta.NamespaceMeta) error {
	g.prepareNamespaceClaims(meta)
	packageName := naming.PackageName(meta.Namespace)
	packageDir := filepath.Join(g.outDir, filepath.FromSlash(naming.PackagePath(meta.Namespace)))

	// Enums file: String() methods use fmt; writeFile prunes it when the
	// package has no enums using the default case (it always does).
	enumImports := typemap.ImportSet{"fmt": {Path: "fmt"}}
	var enumBody strings.Builder
	for _, model := range g.buildEnumModels(meta) {
		if err := renderInto(&enumBody, render.Enum, model); err != nil {
			return err
		}
	}
	if err := g.writeFile(packageDir, packageName+"_enums.go", packageName, enumImports, enumBody.String()); err != nil {
		return err
	}

	structImports := typemap.ImportSet{}
	var structBody strings.Builder
	for _, model := range g.buildStructModels(meta, structImports) {
		if err := renderInto(&structBody, render.Struct, model); err != nil {
			return err
		}
	}
	if err := g.writeFile(packageDir, packageName+"_structs.go", packageName, structImports, structBody.String()); err != nil {
		return err
	}

	interfaceImports := typemap.ImportSet{}
	var interfaceBody strings.Builder
	for _, model := range g.buildInterfaceModels(meta, interfaceImports) {
		if err := renderInto(&interfaceBody, render.Interface, model); err != nil {
			return err
		}
	}
	if err := g.writeFile(packageDir, packageName+"_interfaces.go", packageName, interfaceImports, interfaceBody.String()); err != nil {
		return err
	}

	classImports := typemap.ImportSet{}
	var classBody strings.Builder
	for _, model := range g.buildClassModels(meta, classImports) {
		if err := renderInto(&classBody, render.Class, model); err != nil {
			return err
		}
	}
	if err := g.writeFile(packageDir, packageName+"_classes.go", packageName, classImports, classBody.String()); err != nil {
		return err
	}

	// Generic instantiations requested by the members built above (plus the
	// transitive instantiations their synthesized methods surfaced) — the
	// worklist is drained only after every requester has run.
	pinterfaceImports := typemap.ImportSet{}
	var pinterfaceBody strings.Builder
	for _, model := range g.buildPinterfaceModels(meta, pinterfaceImports) {
		if err := renderInto(&pinterfaceBody, render.Interface, model); err != nil {
			return err
		}
	}
	if err := g.writeFile(packageDir, packageName+"_pinterfaces.go", packageName, pinterfaceImports, pinterfaceBody.String()); err != nil {
		return err
	}

	// Event-delegate handlers requested by the Add/Remove accessors built
	// above (declared interfaces and instantiated pinterfaces alike). The
	// models were built eagerly at request time; sort for determinism.
	sort.Slice(g.pdelModels, func(i, j int) bool { return g.pdelModels[i].TypeName < g.pdelModels[j].TypeName })
	var delegateBody strings.Builder
	for _, model := range g.pdelModels {
		if err := renderInto(&delegateBody, render.Delegate, model); err != nil {
			return err
		}
	}
	if err := g.writeFile(packageDir, packageName+"_delegates.go", packageName, g.pdelImports, delegateBody.String()); err != nil {
		return err
	}

	// Delegate TypeDefs are still not emitted into their home namespace —
	// events ground per-package handler copies on demand instead; record one
	// diagnostic per unprojected delegate type.
	for _, name := range sortedKeys(meta.Delegates) {
		g.diag("delegate-type-skipped", "%s.%s", meta.Namespace, name)
	}

	// Package doc: written directly because the package comment must sit
	// above the package clause, which fileasm's scaffold doesn't model.
	doc := fmt.Sprintf(
		"%s\n\n//go:build %s\n\n// Package %s binds the %s WinRT API surface.\npackage %s\n",
		fileasm.Header, fileasm.GeneratedBuildTag, packageName, meta.Namespace, packageName)
	docPath := filepath.Join(packageDir, "doc.go")
	g.writtenFiles[docPath] = true
	return writeRawFile(docPath, []byte(doc))
}

// renderInto appends one rendered construct to the file body.
func renderInto[T any](body *strings.Builder, renderFunc func(T) (string, error), model T) error {
	block, err := renderFunc(model)
	if err != nil {
		return err
	}
	body.WriteString(block)
	return nil
}

// writeFile prunes unused imports (a resolution may have registered an
// import that a later skip made unnecessary), records the namespaces the
// surviving imports reference (the closure input), and assembles the file.
// Empty bodies produce no file.
func (g *Generator) writeFile(dir, fileName, packageName string, imports typemap.ImportSet, body string) error {
	if strings.TrimSpace(body) == "" {
		return nil
	}
	// Import usage is detected against code only — doc comments mention
	// qualified types without using them.
	code := stripComments(body)
	pruned := map[string]string{}
	for alias, entry := range imports {
		if referencesAlias(code, alias) {
			pruned[alias] = entry.Path
			if entry.Namespace != "" {
				g.referenced[entry.Namespace] = true
			}
		}
	}
	// Bodies reference these by fixed name; detect rather than track.
	for alias, path := range map[string]string{
		"unsafe":   "unsafe",
		"syscall":  "syscall",
		"win32":    typemap.Win32RuntimeImport,
		"syswinrt": typemap.SysWinRTImport,
		"winrt":    g.mapper.RuntimeImportPath(),
	} {
		if referencesAlias(code, alias) {
			pruned[alias] = path
		}
	}
	path := filepath.Join(dir, fileName)
	g.writtenFiles[path] = true
	return fileasm.WriteGoFile(path, fileasm.File{
		PackageName: packageName,
		BuildTag:    fileasm.GeneratedBuildTag,
		Imports:     pruned,
		Body:        body,
	})
}

// referencesAlias reports whether code uses the import alias as a package
// qualifier (`alias.`), requiring a word boundary so a shorter alias is not
// falsely matched inside a longer one.
func referencesAlias(code, alias string) bool {
	needle := alias + "."
	for from := 0; ; {
		index := strings.Index(code[from:], needle)
		if index < 0 {
			return false
		}
		position := from + index
		if position == 0 || !isIdentByte(code[position-1]) {
			return true
		}
		from = position + 1
	}
}

func isIdentByte(b byte) bool {
	return b == '_' || (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9')
}

// stripComments removes //-comment text from a body so import-usage scans
// only see code.
func stripComments(body string) string {
	lines := strings.Split(body, "\n")
	kept := lines[:0]
	for _, line := range lines {
		if index := strings.Index(line, "//"); index >= 0 {
			line = line[:index]
		}
		kept = append(kept, line)
	}
	return strings.Join(kept, "\n")
}

// writeRawFile writes pre-formatted content, creating parent directories.
func writeRawFile(path string, content []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, content, 0o644)
}

// prepareNamespaceClaims resets the per-namespace name state and pre-claims
// every top-level type name (types win any type-vs-value collision).
func (g *Generator) prepareNamespaceClaims(meta *winrtmeta.NamespaceMeta) {
	g.claimedNames = map[string]bool{}
	g.typeNames = map[string]bool{}
	g.pinstByName = map[string]*winrtmeta.TypeRef{}
	g.pinstIID = map[string]string{}
	g.pinstQueue = nil
	g.pdelByName = map[string]*winrtmeta.TypeRef{}
	g.pdelModels = nil
	g.pdelImports = typemap.ImportSet{}
	g.ifaceMethods = map[string][]emittedMethod{}
	claimType := func(name string) {
		exported := naming.Export(name)
		if !g.claimedNames[exported] {
			g.claimedNames[exported] = true
			g.typeNames[exported] = true
		}
	}
	for _, name := range sortedKeys(meta.Enums) {
		claimType(name)
	}
	for _, name := range sortedKeys(meta.Structs) {
		claimType(name)
	}
	for _, name := range sortedKeys(meta.Interfaces) {
		claimType(name)
	}
	for _, name := range sortedKeys(meta.Classes) {
		// Classes that can never emit a type — composable (skipped outright)
		// and statics-only (no default interface: the accessors are the whole
		// projection) — must not hold a name claim: a statics-only class
		// named X with statics interface IX would otherwise block its own
		// X() accessor (CurrentApp, SystemProperties, PlayReadyStatics…).
		if class := meta.Classes[name]; class.Composable || class.DefaultInterface == nil {
			continue
		}
		claimType(name)
	}
}

// claimTypeName consumes a pre-claimed type name; false when the type lost
// its pre-claim to an earlier same-named type.
func (g *Generator) claimTypeName(name string) bool {
	if !g.typeNames[name] {
		return false
	}
	delete(g.typeNames, name) // consumed: a second same-named type is a dupe
	return true
}

// claimName reserves a package-level identifier for a value (enum member,
// IID var, constructor); false when already used.
func (g *Generator) claimName(name string) bool {
	if g.claimedNames[name] {
		return false
	}
	g.claimedNames[name] = true
	return true
}

// resolveContext builds the typemap context for the namespace being
// emitted, wiring the demand-driven instantiation-request seam.
func (g *Generator) resolveContext(namespace string) typemap.Context {
	return typemap.Context{Namespace: namespace, RequestInstantiation: g.requestInstantiation}
}

// diag records one "key: detail" diagnostic.
func (g *Generator) diag(key, format string, args ...any) {
	g.Diagnostics = append(g.Diagnostics, key+": "+fmt.Sprintf(format, args...))
}

// sortedKeys returns the map's keys in sorted order (determinism).
func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// guidLiteral renders a canonical GUID string as a win32.GUID literal with
// lowercase hex.
func guidLiteral(guid string) (string, error) {
	parts := strings.Split(guid, "-")
	if len(parts) != 5 || len(parts[0]) != 8 || len(parts[1]) != 4 || len(parts[2]) != 4 || len(parts[3]) != 4 || len(parts[4]) != 12 {
		return "", fmt.Errorf("malformed GUID %q", guid)
	}
	data1, err1 := strconv.ParseUint(parts[0], 16, 32)
	data2, err2 := strconv.ParseUint(parts[1], 16, 16)
	data3, err3 := strconv.ParseUint(parts[2], 16, 16)
	if err1 != nil || err2 != nil || err3 != nil {
		return "", fmt.Errorf("malformed GUID %q", guid)
	}
	tail := parts[3] + parts[4]
	var data4 [8]string
	for i := range 8 {
		byteValue, err := strconv.ParseUint(tail[i*2:i*2+2], 16, 8)
		if err != nil {
			return "", fmt.Errorf("malformed GUID %q", guid)
		}
		data4[i] = fmt.Sprintf("0x%02x", byteValue)
	}
	return fmt.Sprintf("win32.GUID{Data1: 0x%08x, Data2: 0x%04x, Data3: 0x%04x, Data4: [8]byte{%s}}",
		data1, data2, data3, strings.Join(data4[:], ", ")), nil
}
