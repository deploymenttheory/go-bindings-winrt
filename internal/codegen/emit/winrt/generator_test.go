package emitwinrt

import (
	"strings"
	"testing"

	"github.com/deploymenttheory/go-bindings-winrt/internal/codegen/emit/winrt/view"
	"github.com/deploymenttheory/go-bindings-winrt/internal/codegen/pipeline"
	"github.com/deploymenttheory/go-bindings-winrt/internal/codegen/typemap"
	"github.com/deploymenttheory/go-bindings-winrt/internal/winrtmeta"
)

func TestGuidLiteral(t *testing.T) {
	literal, err := guidLiteral("ca30221d-86d9-40fb-a26b-d44eb7cf08ea")
	if err != nil {
		t.Fatalf("guidLiteral: %v", err)
	}
	want := "win32.GUID{Data1: 0xca30221d, Data2: 0x86d9, Data3: 0x40fb, Data4: [8]byte{0xa2, 0x6b, 0xd4, 0x4e, 0xb7, 0xcf, 0x08, 0xea}}"
	if literal != want {
		t.Errorf("guidLiteral = %s, want %s", literal, want)
	}
	if _, err := guidLiteral("not-a-guid"); err == nil {
		t.Error("guidLiteral accepted a malformed GUID")
	}
}

// testRegistry builds a minimal two-namespace registry: a Windows.Test
// namespace under emit plus the Windows.Foundation types it references.
func testRegistry() *pipeline.Registry {
	native := func(name string) winrtmeta.TypeRef { return winrtmeta.TypeRef{Kind: "Native", Name: name} }
	dateTimeRef := winrtmeta.TypeRef{Kind: "ApiRef", Namespace: "Windows.Foundation", Name: "DateTime", TargetKind: "Struct"}
	pointRef := winrtmeta.TypeRef{Kind: "ApiRef", Namespace: "Windows.Foundation", Name: "Point", TargetKind: "Struct"}
	genericRef := winrtmeta.TypeRef{Kind: "GenericInst", Namespace: "Windows.Foundation.Collections", Name: "IVectorView`1",
		TargetKind: "Interface", Args: []winrtmeta.TypeRef{native("HString")}}

	foundation := &winrtmeta.NamespaceMeta{
		Namespace: "Windows.Foundation",
		Structs: map[string]winrtmeta.Struct{
			"DateTime": {Fields: []winrtmeta.StructField{{Name: "UniversalTime", Type: native("I8")}}},
			"Point": {Fields: []winrtmeta.StructField{
				{Name: "X", Type: native("F32")}, {Name: "Y", Type: native("F32")}}},
		},
	}
	test := &winrtmeta.NamespaceMeta{
		Namespace: "Windows.Test",
		Interfaces: map[string]winrtmeta.Interface{
			"IThing": {
				GUID: "ca30221d-86d9-40fb-a26b-d44eb7cf08ea",
				Methods: []winrtmeta.Method{
					{Name: "GetName", Return: refPtr(native("HString"))},                                        // slot 6
					{Name: "get_Languages", Return: &genericRef},                                                // slot 7: skipped, never renumbers
					{Name: "SetWhen", Params: []winrtmeta.Param{{Name: "value", Type: dateTimeRef}}},            // slot 8
					{Name: "get_IsEnabled", Return: refPtr(native("Bool"))},                                     // slot 9
					{Name: "put_IsEnabled", Params: []winrtmeta.Param{{Name: "value", Type: native("Bool")}}},   // slot 10
					{Name: "Move", Params: []winrtmeta.Param{{Name: "point", Type: pointRef}}},                  // slot 11: by-value struct skipped
					{Name: "GetRatio", Return: refPtr(native("F64"))},                                           // slot 12: float skipped
					{Name: "add_Changed", Params: []winrtmeta.Param{{Name: "handler", Type: native("Object")}}}, // slot 13: event skipped
					{Name: "AsString", Overload: "AsFullString", Return: refPtr(native("HString"))},             // slot 14: [Overload] name
					{Name: "Describe", Params: []winrtmeta.Param{{Name: "prefix", Type: native("HString")}}, // slot 15
						Return: refPtr(native("HString"))},
				},
			},
		},
	}
	registry := &pipeline.Registry{
		Namespaces:     []*winrtmeta.NamespaceMeta{foundation, test},
		ByNamespace:    map[string]*winrtmeta.NamespaceMeta{"Windows.Foundation": foundation, "Windows.Test": test},
		EnumIndex:      map[string]*winrtmeta.Enum{},
		StructIndex:    map[string]*winrtmeta.Struct{},
		InterfaceIndex: map[string]*winrtmeta.Interface{},
		ClassIndex:     map[string]*winrtmeta.Class{},
		DelegateIndex:  map[string]*winrtmeta.Delegate{},
	}
	for name := range foundation.Structs {
		definition := foundation.Structs[name]
		registry.StructIndex["Windows.Foundation."+name] = &definition
	}
	for name := range test.Interfaces {
		definition := test.Interfaces[name]
		registry.InterfaceIndex["Windows.Test."+name] = &definition
	}
	return registry
}

func refPtr(ref winrtmeta.TypeRef) *winrtmeta.TypeRef { return &ref }

// TestMethodLowering pins the ABI lowering shapes: slots never renumber,
// property accessors rename, HSTRING/bool/by-value-struct marshaling, and
// the skip reasons for the shapes this wave does not emit.
func TestMethodLowering(t *testing.T) {
	registry := testRegistry()
	generator := New(registry, "example.com/mod", t.TempDir())
	meta := registry.ByNamespace["Windows.Test"]
	generator.prepareNamespaceClaims(meta)

	imports := typemap.ImportSet{}
	models := generator.buildInterfaceModels(meta, imports)
	if len(models) != 1 {
		t.Fatalf("interface models = %d, want 1", len(models))
	}
	model := models[0]
	if model.IIDVar != "IID_IThing" || !strings.HasPrefix(model.IIDLiteral, "win32.GUID{Data1: 0xca30221d") {
		t.Errorf("IID declaration = %s / %s", model.IIDVar, model.IIDLiteral)
	}
	if len(model.Methods) != 10 {
		t.Fatalf("method entries = %d, want 10 (skips preserved)", len(model.Methods))
	}

	byName := map[string]view.MethodModel{}
	for _, method := range model.Methods {
		if method.SkipComment == "" {
			byName[method.GoName] = method
		}
	}

	// Slots are absolute: skipped members never renumber.
	for name, slot := range map[string]int{
		"GetName": 6, "SetWhen": 8, "IsEnabled": 9, "SetIsEnabled": 10,
		"AsFullString": 14, "Describe": 15,
	} {
		method, ok := byName[name]
		if !ok {
			t.Errorf("method %s not emitted", name)
			continue
		}
		if method.Slot != slot {
			t.Errorf("%s slot = %d, want %d", name, method.Slot, slot)
		}
	}

	// Skip comments hold their slots in order.
	for i, want := range map[int]string{
		1: "slot 7: get_Languages skipped:",
		5: "slot 11: Move skipped:",
		6: "slot 12: GetRatio skipped:",
		7: "slot 13: add_Changed skipped:",
	} {
		if !strings.HasPrefix(model.Methods[i].SkipComment, want) {
			t.Errorf("entry %d skip comment = %q, want prefix %q", i, model.Methods[i].SkipComment, want)
		}
	}

	// HSTRING retval: short-circuit shape.
	getName := byName["GetName"]
	if getName.ReturnKind != view.RetString || getName.ReturnSig != "(string, error)" ||
		getName.ResultExpr != "winrt.TakeHString(result)" {
		t.Errorf("GetName lowering = %+v", getName)
	}
	// By-value single-word struct flattens to its field.
	setWhen := byName["SetWhen"]
	if len(setWhen.ArgExprs) != 1 || setWhen.ArgExprs[0] != "uintptr(value.UniversalTime)" {
		t.Errorf("SetWhen args = %v", setWhen.ArgExprs)
	}
	// Bool retval reads a byte.
	isEnabled := byName["IsEnabled"]
	if isEnabled.ResultDecl != "var result byte" || isEnabled.ResultExpr != "result != 0" {
		t.Errorf("IsEnabled lowering = %+v", isEnabled)
	}
	// Bool input becomes a 0/1 word.
	setEnabled := byName["SetIsEnabled"]
	if len(setEnabled.ArgExprs) != 1 || setEnabled.ArgExprs[0] != "_value" {
		t.Errorf("SetIsEnabled args = %v", setEnabled.ArgExprs)
	}
	// HSTRING input: RAII conversion with the zero-value error return.
	describe := byName["Describe"]
	joined := strings.Join(describe.Preamble, "\n")
	if !strings.Contains(joined, "winrt.NewHString(prefix)") || !strings.Contains(joined, `return "", err`) ||
		!strings.Contains(joined, "defer hPrefix.Close()") {
		t.Errorf("Describe preamble = %v", describe.Preamble)
	}

	// Diagnostics carry the ratchet keys.
	diagnostics := strings.Join(generator.Diagnostics, "\n")
	for _, key := range []string{"generic-member-skipped", "byval-struct-param-skipped", "float-abi-skipped", "event-skipped"} {
		if !strings.Contains(diagnostics, key) {
			t.Errorf("diagnostics missing key %s: %v", key, generator.Diagnostics)
		}
	}
}
