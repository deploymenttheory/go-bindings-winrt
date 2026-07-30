package emitwinrt

import (
	"strings"
	"testing"

	"github.com/deploymenttheory/go-bindings-winrt/internal/codegen/pipeline"
	"github.com/deploymenttheory/go-bindings-winrt/internal/codegen/typemap"
	"github.com/deploymenttheory/go-bindings-winrt/internal/winrtmeta"
)

// stringStructRegistry mirrors the four structs in the real metadata that have string
// fields, in the three shapes they come in: strings only, a string beside a bool, and
// a string beside an enum. TypeName is the one that matters most — it is what
// Frame.Navigate and ControlTemplate.TargetType take, and the whole XAML type system
// is addressed by it.
func stringStructRegistry() *pipeline.Registry {
	native := func(name string) winrtmeta.TypeRef { return winrtmeta.TypeRef{Kind: "Native", Name: name} }
	kindRef := winrtmeta.TypeRef{Kind: "ApiRef", Namespace: "Windows.Test", Name: "TypeKind", TargetKind: "Enum"}

	test := &winrtmeta.NamespaceMeta{
		Namespace: "Windows.Test",
		Enums: map[string]winrtmeta.Enum{
			"TypeKind": {BaseType: "int32", Members: []winrtmeta.EnumMember{{Name: "Primitive", Value: "0"}}},
		},
		Structs: map[string]winrtmeta.Struct{
			// Two handles.
			"XmlnsDefinition": {Fields: []winrtmeta.StructField{
				{Name: "XmlNamespace", Type: native("HString")},
				{Name: "Namespace", Type: native("HString")},
			}},
			// A handle and a bool, which have different widths and must not be
			// conflated when the layout is computed.
			"SortEntry": {Fields: []winrtmeta.StructField{
				{Name: "PropertyName", Type: native("HString")},
				{Name: "AscendingOrder", Type: native("Bool")},
			}},
			// A handle and an enum: TypeName's shape.
			"TypeName": {Fields: []winrtmeta.StructField{
				{Name: "Name", Type: native("HString")},
				{Name: "Kind", Type: kindRef},
			}},
			// No strings: must be untouched by any of this.
			"Plain": {Fields: []winrtmeta.StructField{{Name: "Count", Type: native("U4")}}},
		},
	}

	registry := &pipeline.Registry{
		Namespaces:     []*winrtmeta.NamespaceMeta{test},
		ByNamespace:    map[string]*winrtmeta.NamespaceMeta{"Windows.Test": test},
		EnumIndex:      map[string]*winrtmeta.Enum{},
		StructIndex:    map[string]*winrtmeta.Struct{},
		InterfaceIndex: map[string]*winrtmeta.Interface{},
		ClassIndex:     map[string]*winrtmeta.Class{},
		DelegateIndex:  map[string]*winrtmeta.Delegate{},
	}
	for name := range test.Structs {
		definition := test.Structs[name]
		registry.StructIndex["Windows.Test."+name] = &definition
	}
	for name := range test.Enums {
		definition := test.Enums[name]
		registry.EnumIndex["Windows.Test."+name] = &definition
	}
	return registry
}

// TestStringStructFieldsAreHandles is the whole change, stated as one test.
//
// A struct crosses the ABI as a block of bytes, so every field has to BE its ABI
// form: there is no call boundary inside a struct at which a conversion could run.
// For a string field that means syswinrt.HSTRING, not string. A parameter or a return
// converts precisely because there IS a boundary.
//
// Emitting `string` here instead would compile and then corrupt every call passing
// the struct, because a Go string header is two words where the ABI has one handle.
func TestStringStructFieldsAreHandles(t *testing.T) {
	registry := stringStructRegistry()
	generator := New(registry, "example.com/mod", t.TempDir())
	meta := registry.ByNamespace["Windows.Test"]
	generator.prepareNamespaceClaims(meta)

	imports := typemap.ImportSet{}
	models := generator.buildStructModels(meta, imports)

	byName := map[string]int{}
	for i, model := range models {
		byName[model.TypeName] = i
	}
	for _, want := range []string{"XmlnsDefinition", "SortEntry", "TypeName", "Plain"} {
		if _, ok := byName[want]; !ok {
			t.Fatalf("%s was not emitted; every one of these has only representable fields", want)
		}
	}

	// The handle, per field, and nothing else changed about the neighbours.
	fields := func(name string) map[string]string {
		out := map[string]string{}
		for _, field := range models[byName[name]].Fields {
			out[field.Name] = field.GoType
		}
		return out
	}
	if got := fields("TypeName"); got["Name"] != "syswinrt.HSTRING" || got["Kind"] != "TypeKind" {
		t.Errorf("TypeName fields = %v, want Name syswinrt.HSTRING and Kind TypeKind", got)
	}
	if got := fields("SortEntry"); got["PropertyName"] != "syswinrt.HSTRING" || got["AscendingOrder"] != "bool" {
		t.Errorf("SortEntry fields = %v, want PropertyName syswinrt.HSTRING and AscendingOrder bool", got)
	}
	if got := fields("Plain"); got["Count"] != "uint32" {
		t.Errorf("Plain.Count = %q, want uint32: a struct with no string fields must be unaffected", got["Count"])
	}

	// The import has to be recorded, or the file names a package it did not import.
	if entry, ok := imports["syswinrt"]; !ok || entry.Path != typemap.SysWinRTImport {
		t.Error("the syswinrt import was not recorded for the HSTRING fields")
	}
}

// TestStringStructsStateTheOwnershipRule is not decoration. A syswinrt.HSTRING field
// with no explanation reads like a string-shaped thing: a caller who assigns one and
// forgets leaks the string, and a caller who reads one the callee wrote and does not
// take it leaks it too. The type cannot say that; the doc comment has to.
func TestStringStructsStateTheOwnershipRule(t *testing.T) {
	registry := stringStructRegistry()
	generator := New(registry, "example.com/mod", t.TempDir())
	meta := registry.ByNamespace["Windows.Test"]
	generator.prepareNamespaceClaims(meta)
	models := generator.buildStructModels(meta, typemap.ImportSet{})

	byName := map[string]int{}
	for i, model := range models {
		byName[model.TypeName] = i
	}

	note := strings.Join(models[byName["TypeName"]].NoteLines, "\n")
	for _, want := range []string{
		"Name is an HSTRING handle, not a Go string.",
		"winrt.NewHString",
		"winrt.TakeHString",
	} {
		if !strings.Contains(note, want) {
			t.Errorf("TypeName's note does not mention %q", want)
		}
	}
	// Plural, and every handle named: a caller has to know which fields carry the
	// obligation, not merely that some do.
	plural := strings.Join(models[byName["XmlnsDefinition"]].NoteLines, "\n")
	if !strings.Contains(plural, "XmlNamespace and Namespace are HSTRING handles, not Go strings.") {
		t.Errorf("XmlnsDefinition's note does not name both handles: %q", plural)
	}
	// And a struct with no handles gets no note at all.
	if len(models[byName["Plain"]].NoteLines) != 0 {
		t.Errorf("Plain carries an HSTRING note but has no HSTRING field: %v",
			models[byName["Plain"]].NoteLines)
	}
}

// TestStringStructLayout checks the sizing that makes the field safe to pass by value.
//
// The amd64 by-value aggregate rule keys off the struct's SIZE — 1, 2, 4 or 8 bytes go
// in a register and anything else goes by pointer — so a mis-sized field puts the
// whole struct in the wrong place. An HSTRING is an opaque pointer-sized handle.
func TestStringStructLayout(t *testing.T) {
	registry := stringStructRegistry()
	mapper := &typemap.Mapper{Registry: registry, ModulePath: "example.com/mod"}

	for _, testCase := range []struct {
		name string
		size int
	}{
		// Two handles.
		{"XmlnsDefinition", 16},
		// A handle then a bool, padded out to the handle's 8-byte alignment.
		{"SortEntry", 16},
		// A handle then an int32 enum, likewise padded.
		{"TypeName", 16},
	} {
		layout, ok := mapper.StructLayout("Windows.Test", testCase.name)
		if !ok {
			t.Errorf("%s has no computed layout, so it cannot be passed by value", testCase.name)
			continue
		}
		if layout.Size != testCase.size {
			t.Errorf("%s size = %d, want %d", testCase.name, layout.Size, testCase.size)
		}
		if layout.Align != 8 {
			t.Errorf("%s align = %d, want 8 (the handle's)", testCase.name, layout.Align)
		}
		// None of these fits the register rule, so all three travel by pointer. Worth
		// asserting: a struct wrongly sized to 8 would be passed in a register and the
		// callee would read the handle as the whole struct.
		if layout.Size == 1 || layout.Size == 2 || layout.Size == 4 || layout.Size == 8 {
			t.Errorf("%s sized %d would be passed in a register", testCase.name, layout.Size)
		}
	}
}
