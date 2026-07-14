// Package ingest projects the pinned WinRT contract winmds into the
// winrtmeta IR: one NamespaceMeta per Windows.* namespace, merged across
// every contract file.
//
// Both pinned contracts are LOCAL — every projected type is emitted by this
// module — so the merge is a plain union: types append into their namespace
// and a duplicate full type name across files is a hard error (unlike the
// wdk generator, there is no external-assembly prefix). Classification of
// TypeRef targets uses a union KindIndex built over ALL files before any
// projection, because the contracts cross-reference freely (system winmds
// use TypeRef indirection everywhere, even same-file).
package ingest

import (
	"fmt"
	"sort"

	"github.com/deploymenttheory/go-bindings-winrt/internal/winrtmeta"
	winmd "github.com/deploymenttheory/go-winmd"
)

// Source is one contract winmd to ingest.
type Source struct {
	// Name identifies the file in error messages and diagnostics
	// (e.g. "Windows.Foundation.UniversalApiContract.winmd").
	Name string
	// File is the parsed winmd.
	File *winmd.File
}

// Ingester projects a set of contract winmds into NamespaceMeta values.
type Ingester struct {
	sources      []Source
	winmdVersion string

	// kindIndex classifies every projected TypeDef across ALL sources by
	// full name ("Namespace.Name") → construct kind ("Enum", "Struct",
	// "Interface", "Class", "Delegate").
	kindIndex map[string]string

	// Diagnostics collects non-fatal projection notes as "key: detail"
	// strings (e.g. "unresolved-typeref: Windows.Foo.IBar").
	Diagnostics []string
}

// New builds an Ingester over the parsed contract files and runs the index
// pass: every source's TypeDefs are classified into the union kindIndex. A
// duplicate full type name across files is a hard error (the contracts
// partition the surface; a collision means a mispinned file set).
func New(sources []Source, winmdVersion string) (*Ingester, error) {
	in := &Ingester{sources: sources, winmdVersion: winmdVersion}
	in.kindIndex = map[string]string{}
	definedIn := map[string]string{}
	for _, source := range sources {
		tables := &source.File.Tables
		for typeDefRow := range tables.TypeDefs {
			typeDef := &tables.TypeDefs[typeDefRow]
			// Skip the <Module> pseudo-type only: [ExclusiveTo] interfaces
			// are marked NotPublic in the contracts but ARE the ABI surface.
			if typeDef.Namespace == "" {
				continue
			}
			kind := classifyTypeDef(source.File, typeDef)
			if kind == "" {
				continue // attribute definitions are not TypeRef targets
			}
			fullName := typeDef.Namespace + "." + typeDef.Name
			if previous, seen := definedIn[fullName]; seen {
				return nil, fmt.Errorf("ingest: type %s defined in both %s and %s", fullName, previous, source.Name)
			}
			definedIn[fullName] = source.Name
			in.kindIndex[fullName] = kind
		}
	}
	return in, nil
}

// KindIndex exposes the union full-name → construct-kind classification.
func (in *Ingester) KindIndex() map[string]string {
	return in.kindIndex
}

// Ingest projects every namespace from every source, merged by namespace
// and sorted by namespace name.
func (in *Ingester) Ingest() ([]*winrtmeta.NamespaceMeta, error) {
	byNamespace := map[string]*winrtmeta.NamespaceMeta{}
	for _, source := range in.sources {
		projector := newFileProjector(in, source)
		if err := projector.project(byNamespace); err != nil {
			return nil, err
		}
	}
	namespaces := make([]*winrtmeta.NamespaceMeta, 0, len(byNamespace))
	for _, meta := range byNamespace {
		namespaces = append(namespaces, meta)
	}
	sort.Slice(namespaces, func(i, j int) bool { return namespaces[i].Namespace < namespaces[j].Namespace })
	return namespaces, nil
}

// diag records one "key: detail" diagnostic.
func (in *Ingester) diag(key, format string, args ...any) {
	in.Diagnostics = append(in.Diagnostics, key+": "+fmt.Sprintf(format, args...))
}

// classifyTypeDef determines a TypeDef's construct kind. Interfaces carry
// the flag; everything else classifies by its resolved Extends name — the
// mscorlib marker types (System.Enum/ValueType/MulticastDelegate/Attribute/
// Object) are type-system signals, never resolved as real types. An empty
// result marks an attribute definition (skipped as a type; its instances
// are consumed).
func classifyTypeDef(file *winmd.File, typeDef *winmd.TypeDefRow) string {
	if typeDef.Flags&winmd.TypeAttrInterface != 0 {
		return "Interface"
	}
	switch namespace, name := extendsOf(file, typeDef); namespace + "." + name {
	case "System.Enum":
		return "Enum"
	case "System.ValueType":
		return "Struct"
	case "System.MulticastDelegate":
		return "Delegate"
	case "System.Attribute":
		return "" // attribute definitions are not part of the API surface
	}
	// Extends System.Object (non-composable) or another runtime class
	// (composable).
	return "Class"
}

// extendsOf resolves a TypeDef's Extends coded index to (namespace, name).
func extendsOf(file *winmd.File, typeDef *winmd.TypeDefRow) (string, string) {
	tables := &file.Tables
	switch typeDef.Extends.Table {
	case winmd.TableTypeRef:
		if typeDef.Extends.Row != 0 && int(typeDef.Extends.Row) <= len(tables.TypeRefs) {
			ref := &tables.TypeRefs[typeDef.Extends.Row-1]
			return ref.Namespace, ref.Name
		}
	case winmd.TableTypeDef:
		if typeDef.Extends.Row != 0 && int(typeDef.Extends.Row) <= len(tables.TypeDefs) {
			def := &tables.TypeDefs[typeDef.Extends.Row-1]
			return def.Namespace, def.Name
		}
	}
	return "", ""
}

func typeDefTarget(row uint32) winmd.CodedIndex {
	return winmd.CodedIndex{Table: winmd.TableTypeDef, Row: row}
}
