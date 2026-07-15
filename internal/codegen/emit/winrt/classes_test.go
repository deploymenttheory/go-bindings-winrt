package emitwinrt

import (
	"strings"
	"testing"

	"github.com/deploymenttheory/go-bindings-winrt/internal/codegen/emit/winrt/view"
	"github.com/deploymenttheory/go-bindings-winrt/internal/codegen/pipeline"
	"github.com/deploymenttheory/go-bindings-winrt/internal/codegen/typemap"
	"github.com/deploymenttheory/go-bindings-winrt/internal/winrtmeta"
)

// classesRegistry builds a registry exercising the statics and factory
// projections: an emittable statics interface, one without an IID, a generic
// one, an unresolved one, a statics-only class, a factory with an emitted
// creator plus per-method skip shapes, a colliding second factory, and a
// generic factory.
func classesRegistry() *pipeline.Registry {
	widgetRef := winrtmeta.TypeRef{Kind: "ApiRef", Namespace: "Windows.Test", Name: "IWidget", TargetKind: "Interface"}
	widgetClassRef := winrtmeta.TypeRef{Kind: "ApiRef", Namespace: "Windows.Test", Name: "Widget", TargetKind: "Class"}

	test := &winrtmeta.NamespaceMeta{
		Namespace: "Windows.Test",
		Interfaces: map[string]winrtmeta.Interface{
			"IWidget": {
				GUID: "ca30221d-86d9-40fb-a26b-d44eb7cf08ea",
				Methods: []winrtmeta.Method{
					{Name: "get_Name", Return: refPtr(nativeRef("HString"))}, // slot 6
				},
			},
			"IWidgetStatics": {
				GUID: "11111111-2222-4333-8444-555555555555", ExclusiveTo: "Windows.Test.Widget",
				Methods: []winrtmeta.Method{
					{Name: "get_Version", Return: refPtr(nativeRef("HString"))}, // slot 6
				},
			},
			"IWidgetStatics2": { // no GUID: the accessor cannot name an IID var
				ExclusiveTo: "Windows.Test.Widget",
			},
			"IGadgetStatics": { // generic: statics stay skipped
				GUID: "21111111-2222-4333-8444-555555555555", Arity: 1,
			},
			"IWidgetFactory": {
				GUID: "31111111-2222-4333-8444-555555555555", ExclusiveTo: "Windows.Test.Widget",
				Methods: []winrtmeta.Method{
					{Name: "CreateWidget", // slot 6: emitted
						Params: []winrtmeta.Param{{Name: "name", Type: nativeRef("HString")}},
						Return: refPtr(widgetClassRef)},
					{Name: "CreateWidgetFromValues", // slot 7: array param degrades the method
						Params: []winrtmeta.Param{{Name: "values", Type: winrtmeta.TypeRef{Kind: "Array", Elem: refPtr(nativeRef("I4"))}}},
						Return: refPtr(widgetClassRef)},
					{Name: "MakeLabel", // slot 8: emitted, but not a constructor shape
						Return: refPtr(nativeRef("HString"))},
					{Name: "CreateWidgetWithOptions", // slot 9: cross-namespace param — the
						// wrapper restates the signature in the classes file, whose import
						// set must gain the parameter's namespace.
						Params: []winrtmeta.Param{{Name: "options", Type: winrtmeta.TypeRef{Kind: "ApiRef", Namespace: "Windows.Other", Name: "IOptions", TargetKind: "Interface"}}},
						Return: refPtr(widgetClassRef)},
				},
			},
			"IWidgetFactory2": {
				GUID: "41111111-2222-4333-8444-555555555555", ExclusiveTo: "Windows.Test.Widget",
				Methods: []winrtmeta.Method{
					{Name: "CreateWidget", // collides with IWidgetFactory's: ordinal suffix
						Params: []winrtmeta.Param{{Name: "id", Type: nativeRef("I4")}},
						Return: refPtr(widgetClassRef)},
				},
			},
			"IGenFactory": {
				GUID: "51111111-2222-4333-8444-555555555555", Arity: 1,
			},
			"IHelpersStatics": {
				GUID: "61111111-2222-4333-8444-555555555555", ExclusiveTo: "Windows.Test.Helpers",
				Methods: []winrtmeta.Method{
					{Name: "get_Ready", Return: refPtr(nativeRef("Bool"))}, // slot 6
				},
			},
			// Statics-only class named exactly like its accessor: the class
			// name must not hold a type claim (no type is ever emitted), or
			// the Toolbox() accessor would lose to it.
			"IToolbox": {
				GUID: "81111111-2222-4333-8444-555555555555", ExclusiveTo: "Windows.Test.Toolbox",
				Methods: []winrtmeta.Method{
					{Name: "get_Ready", Return: refPtr(nativeRef("Bool"))}, // slot 6
				},
			},
			// Two classes whose factories share the bare name Create: the
			// first claims it, the second falls back to the class-qualified
			// CreateBeta (not skipped, not the within-class ordinal form).
			"IAlpha": {GUID: "91111111-2222-4333-8444-555555555555"},
			"IAlphaFactory": {
				GUID: "a1111111-2222-4333-8444-555555555555", ExclusiveTo: "Windows.Test.Alpha",
				Methods: []winrtmeta.Method{
					{Name: "Create", Return: refPtr(winrtmeta.TypeRef{Kind: "ApiRef", Namespace: "Windows.Test", Name: "Alpha", TargetKind: "Class"})}, // slot 6
				},
			},
			"IBeta": {GUID: "b1111111-2222-4333-8444-555555555555"},
			"IBetaFactory": {
				GUID: "c1111111-2222-4333-8444-555555555555", ExclusiveTo: "Windows.Test.Beta",
				Methods: []winrtmeta.Method{
					{Name: "Create", Return: refPtr(winrtmeta.TypeRef{Kind: "ApiRef", Namespace: "Windows.Test", Name: "Beta", TargetKind: "Class"})}, // slot 6
				},
			},
		},
		Classes: map[string]winrtmeta.Class{
			"Widget": {
				DefaultInterface: refPtr(widgetRef),
				Interfaces:       []winrtmeta.TypeRef{widgetRef},
				ActivatableFactories: []string{
					"Windows.Test.IWidgetFactory",
					"Windows.Test.IWidgetFactory2",
				},
				StaticInterfaces: []string{
					"Windows.Test.IWidgetStatics",
					"Windows.Test.IWidgetStatics2",
					"Windows.Test.IGadgetStatics",
				},
			},
			// Statics-only: no class type, only the accessor.
			"Helpers": {
				StaticInterfaces: []string{"Windows.Test.IHelpersStatics"},
			},
			// Every statics reference fails: no model at all.
			"Orphan": {
				StaticInterfaces: []string{"Windows.Test.IMissing"},
			},
			"Gen": {
				DefaultInterface:     refPtr(widgetRef),
				ActivatableFactories: []string{"Windows.Test.IGenFactory"},
			},
			// Statics-only, accessor name == class name (see IToolbox).
			"Toolbox": {
				StaticInterfaces: []string{"Windows.Test.IToolbox"},
			},
			// Cross-class factory-name collision pair (see IAlphaFactory).
			"Alpha": {
				DefaultInterface:     refPtr(winrtmeta.TypeRef{Kind: "ApiRef", Namespace: "Windows.Test", Name: "IAlpha", TargetKind: "Interface"}),
				ActivatableFactories: []string{"Windows.Test.IAlphaFactory"},
			},
			"Beta": {
				DefaultInterface:     refPtr(winrtmeta.TypeRef{Kind: "ApiRef", Namespace: "Windows.Test", Name: "IBeta", TargetKind: "Interface"}),
				ActivatableFactories: []string{"Windows.Test.IBetaFactory"},
			},
		},
	}
	// A second namespace: the home of a cross-namespace factory parameter.
	other := &winrtmeta.NamespaceMeta{
		Namespace: "Windows.Other",
		Interfaces: map[string]winrtmeta.Interface{
			"IOptions": {GUID: "71111111-2222-4333-8444-555555555555"},
		},
	}
	registry := &pipeline.Registry{
		Namespaces:     []*winrtmeta.NamespaceMeta{test, other},
		ByNamespace:    map[string]*winrtmeta.NamespaceMeta{test.Namespace: test, other.Namespace: other},
		EnumIndex:      map[string]*winrtmeta.Enum{},
		StructIndex:    map[string]*winrtmeta.Struct{},
		InterfaceIndex: map[string]*winrtmeta.Interface{},
		ClassIndex:     map[string]*winrtmeta.Class{},
		DelegateIndex:  map[string]*winrtmeta.Delegate{},
	}
	for name := range test.Interfaces {
		definition := test.Interfaces[name]
		registry.InterfaceIndex["Windows.Test."+name] = &definition
	}
	for name := range other.Interfaces {
		definition := other.Interfaces[name]
		registry.InterfaceIndex["Windows.Other."+name] = &definition
	}
	for name := range test.Classes {
		definition := test.Classes[name]
		registry.ClassIndex["Windows.Test."+name] = &definition
	}
	return registry
}

// buildClassesFixture runs the gather in emit order (interfaces first, so
// the factory-method records exist) and indexes the class models by name.
// The interfaces and classes files get SEPARATE import sets, exactly as
// emitNamespace wires them — a wrapper restating an interface signature in
// the classes file must register its own import edges.
func buildClassesFixture(t *testing.T) (*Generator, map[string]view.ClassModel, typemap.ImportSet) {
	t.Helper()
	registry := classesRegistry()
	generator := New(registry, "example.com/mod", t.TempDir())
	meta := registry.ByNamespace["Windows.Test"]
	generator.prepareNamespaceClaims(meta)
	interfaceImports := typemap.ImportSet{}
	generator.buildInterfaceModels(meta, interfaceImports)
	classImports := typemap.ImportSet{}
	models := map[string]view.ClassModel{}
	for _, model := range generator.buildClassModels(meta, classImports) {
		models[model.FullName] = model
	}
	return generator, models, classImports
}

// TestStaticsAccessorEmission pins the statics projection: accessor naming,
// statics-only classes emitting with no class type, and the per-interface
// skip reasons (no IID, generic, unresolved).
func TestStaticsAccessorEmission(t *testing.T) {
	generator, models, _ := buildClassesFixture(t)

	widget, ok := models["Windows.Test.Widget"]
	if !ok {
		t.Fatal("Widget model not built")
	}
	if widget.TypeName != "Widget" {
		t.Errorf("Widget.TypeName = %q", widget.TypeName)
	}
	if len(widget.Statics) != 1 {
		t.Fatalf("Widget statics = %+v, want exactly IWidgetStatics", widget.Statics)
	}
	statics := widget.Statics[0]
	if statics.FuncName != "WidgetStatics" || statics.InterfaceType != "IWidgetStatics" ||
		statics.IIDRef != "&IID_IWidgetStatics" || statics.ClassFullName != "Windows.Test.Widget" {
		t.Errorf("Widget statics accessor = %+v", statics)
	}

	// Statics-only class: accessor emitted, class type absent, no
	// statics-only-class-skipped diagnostic.
	helpers, ok := models["Windows.Test.Helpers"]
	if !ok {
		t.Fatal("Helpers model not built (statics-only classes must emit accessors)")
	}
	if helpers.TypeName != "" || len(helpers.Statics) != 1 || helpers.Statics[0].FuncName != "HelpersStatics" {
		t.Errorf("Helpers model = %+v", helpers)
	}

	// Every accessor failed: no model, only diagnostics.
	if _, ok := models["Windows.Test.Orphan"]; ok {
		t.Error("Orphan emitted a model despite having no emittable statics")
	}

	// A statics-only class named exactly like its accessor: the class name
	// holds no type claim (no type is ever emitted), so Toolbox() wins
	// (CurrentApp, SystemProperties, PlayReadyStatics in the real surface).
	toolbox, ok := models["Windows.Test.Toolbox"]
	if !ok {
		t.Fatal("Toolbox model not built")
	}
	if toolbox.TypeName != "" || len(toolbox.Statics) != 1 || toolbox.Statics[0].FuncName != "Toolbox" {
		t.Errorf("Toolbox model = %+v, want a bare Toolbox() accessor and no class type", toolbox)
	}

	diagnostics := strings.Join(generator.Diagnostics, "\n")
	for _, want := range []string{
		"statics-skipped: Windows.Test.Widget (Windows.Test.IWidgetStatics2 has no IID)",
		"statics-skipped: Windows.Test.Widget (Windows.Test.IGadgetStatics: generic interface Windows.Test.IGadgetStatics)",
		"statics-skipped: Windows.Test.Orphan (Windows.Test.IMissing: Windows.Test.IMissing)",
	} {
		if !strings.Contains(diagnostics, want) {
			t.Errorf("diagnostics missing %q:\n%s", want, diagnostics)
		}
	}
	if strings.Contains(diagnostics, "statics-only-class-skipped") {
		t.Errorf("statics-only-class-skipped still reported:\n%s", diagnostics)
	}
}

// TestFactoryConstructorEmission pins the factory projection: the wrapper
// mirrors the generated method's parameters, name collisions fall back to
// the factory ordinal, and the per-method skip reasons (method not emitted,
// wrong return shape, generic factory).
func TestFactoryConstructorEmission(t *testing.T) {
	generator, models, classImports := buildClassesFixture(t)

	widget := models["Windows.Test.Widget"]
	if len(widget.Factories) != 3 {
		t.Fatalf("Widget factories = %+v, want CreateWidget + CreateWidgetWithOptions + CreateWidget2", widget.Factories)
	}
	first := widget.Factories[0]
	if first.FuncName != "CreateWidget" || first.FactoryType != "IWidgetFactory" ||
		first.FactoryIIDRef != "&IID_IWidgetFactory" || first.MethodName != "CreateWidget" ||
		first.ParamStr != "name string" || strings.Join(first.ArgNames, ",") != "name" {
		t.Errorf("first factory constructor = %+v", first)
	}
	// A cross-namespace parameter: the wrapper restates the signature in the
	// classes file, so the classes import set must gain the parameter's
	// namespace even though the interfaces file registered it separately.
	withOptions := widget.Factories[1]
	if withOptions.FuncName != "CreateWidgetWithOptions" || !strings.Contains(withOptions.ParamStr, "other.IOptions") {
		t.Errorf("cross-namespace factory constructor = %+v", withOptions)
	}
	foundOther := false
	for _, entry := range classImports {
		if entry.Namespace == "Windows.Other" {
			foundOther = true
		}
	}
	if !foundOther {
		t.Errorf("classes import set is missing Windows.Other, needed by CreateWidgetWithOptions: %+v", classImports)
	}
	// The second factory's CreateWidget lost the name claim: the name already
	// carries the class name, so within-class disambiguation is the 1-based
	// ordinal suffix, deterministic.
	second := widget.Factories[2]
	if second.FuncName != "CreateWidget2" || second.FactoryType != "IWidgetFactory2" ||
		second.ParamStr != "id int32" {
		t.Errorf("second factory constructor = %+v", second)
	}

	// A CROSS-class collision on a bare factory name (Create is everywhere in
	// dense packages): the loser gains its class name instead of skipping.
	alpha, beta := models["Windows.Test.Alpha"], models["Windows.Test.Beta"]
	if len(alpha.Factories) != 1 || alpha.Factories[0].FuncName != "Create" {
		t.Errorf("Alpha factories = %+v, want the bare Create", alpha.Factories)
	}
	if len(beta.Factories) != 1 || beta.Factories[0].FuncName != "CreateBeta" {
		t.Errorf("Beta factories = %+v, want the class-qualified CreateBeta", beta.Factories)
	}

	diagnostics := strings.Join(generator.Diagnostics, "\n")
	for _, want := range []string{
		"factory-skipped: Windows.Test.Widget factory Windows.Test.IWidgetFactory (method CreateWidgetFromValues not emitted on the factory interface)",
		"factory-skipped: Windows.Test.Widget factory Windows.Test.IWidgetFactory (method MakeLabel does not return the class default interface)",
		"factory-generic-skipped: Windows.Test.Gen factory Windows.Test.IGenFactory",
	} {
		if !strings.Contains(diagnostics, want) {
			t.Errorf("diagnostics missing %q:\n%s", want, diagnostics)
		}
	}
}
