// Package pipeline loads the ingested .winrtmeta.json IR into a Registry
// and drives the emitters.
package pipeline

import (
	"fmt"

	"github.com/deploymenttheory/go-bindings-winrt/internal/winrtmeta"
)

// Registry is the cross-namespace resolution index. The winmd namespaces
// are authoritative, so every index is a straight projection of the IR.
type Registry struct {
	Namespaces []*winrtmeta.NamespaceMeta

	// ByNamespace maps the full namespace ("Windows.Globalization") to its
	// meta.
	ByNamespace map[string]*winrtmeta.NamespaceMeta

	// EnumIndex maps "Namespace.Name" → the enum definition.
	EnumIndex map[string]*winrtmeta.Enum
	// StructIndex maps "Namespace.Name" → the struct definition.
	StructIndex map[string]*winrtmeta.Struct
	// InterfaceIndex maps "Namespace.Name" → the interface definition.
	InterfaceIndex map[string]*winrtmeta.Interface
	// ClassIndex maps "Namespace.Name" → the runtime-class definition.
	ClassIndex map[string]*winrtmeta.Class
	// DelegateIndex maps "Namespace.Name" → the delegate definition.
	DelegateIndex map[string]*winrtmeta.Delegate
}

// qualified builds the "Namespace.Name" index key.
func qualified(namespace, name string) string { return namespace + "." + name }

// LoadAll reads every namespace metadata file in the given dirs into one
// merged Registry.
func LoadAll(dirs ...string) (*Registry, error) {
	var namespaces []*winrtmeta.NamespaceMeta
	for _, dir := range dirs {
		loaded, err := winrtmeta.ReadAll(dir)
		if err != nil {
			return nil, err
		}
		namespaces = append(namespaces, loaded...)
	}
	if len(namespaces) == 0 {
		return nil, fmt.Errorf("pipeline: no .winrtmeta.json files in %v (run 'generate ingest')", dirs)
	}
	registry := &Registry{
		Namespaces:     namespaces,
		ByNamespace:    make(map[string]*winrtmeta.NamespaceMeta, len(namespaces)),
		EnumIndex:      map[string]*winrtmeta.Enum{},
		StructIndex:    map[string]*winrtmeta.Struct{},
		InterfaceIndex: map[string]*winrtmeta.Interface{},
		ClassIndex:     map[string]*winrtmeta.Class{},
		DelegateIndex:  map[string]*winrtmeta.Delegate{},
	}
	for _, meta := range namespaces {
		registry.ByNamespace[meta.Namespace] = meta
		for name := range meta.Enums {
			enum := meta.Enums[name]
			registry.EnumIndex[qualified(meta.Namespace, name)] = &enum
		}
		for name := range meta.Structs {
			definition := meta.Structs[name]
			registry.StructIndex[qualified(meta.Namespace, name)] = &definition
		}
		for name := range meta.Interfaces {
			definition := meta.Interfaces[name]
			registry.InterfaceIndex[qualified(meta.Namespace, name)] = &definition
		}
		for name := range meta.Classes {
			definition := meta.Classes[name]
			registry.ClassIndex[qualified(meta.Namespace, name)] = &definition
		}
		for name := range meta.Delegates {
			definition := meta.Delegates[name]
			registry.DelegateIndex[qualified(meta.Namespace, name)] = &definition
		}
	}
	return registry, nil
}

// Interface resolves an interface reference, or nil.
func (r *Registry) Interface(namespace, name string) *winrtmeta.Interface {
	return r.InterfaceIndex[qualified(namespace, name)]
}

// Class resolves a runtime-class reference, or nil.
func (r *Registry) Class(namespace, name string) *winrtmeta.Class {
	return r.ClassIndex[qualified(namespace, name)]
}

// Struct resolves a struct reference, or nil.
func (r *Registry) Struct(namespace, name string) *winrtmeta.Struct {
	return r.StructIndex[qualified(namespace, name)]
}

// Delegate resolves a delegate reference, or nil.
func (r *Registry) Delegate(namespace, name string) *winrtmeta.Delegate {
	return r.DelegateIndex[qualified(namespace, name)]
}

// EnumBase resolves an enum's Go base type, or "".
func (r *Registry) EnumBase(namespace, name string) string {
	if enum := r.EnumIndex[qualified(namespace, name)]; enum != nil {
		return enum.BaseType
	}
	return ""
}
