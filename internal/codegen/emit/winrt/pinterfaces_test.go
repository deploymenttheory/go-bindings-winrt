package emitwinrt

import (
	"strings"
	"testing"

	"github.com/deploymenttheory/go-bindings-winrt/internal/codegen/pinterface"
	"github.com/deploymenttheory/go-bindings-winrt/internal/codegen/pipeline"
	"github.com/deploymenttheory/go-bindings-winrt/internal/codegen/typemap"
	"github.com/deploymenttheory/go-bindings-winrt/internal/winrtmeta"
)

func nativeRef(name string) winrtmeta.TypeRef { return winrtmeta.TypeRef{Kind: "Native", Name: name} }

func paramRef(index uint32) winrtmeta.TypeRef {
	return winrtmeta.TypeRef{Kind: "GenericParamRef", Index: index}
}

func instRef(name string, args ...winrtmeta.TypeRef) winrtmeta.TypeRef {
	return winrtmeta.TypeRef{
		Kind: "GenericInst", Namespace: "Windows.Foundation.Collections",
		Name: name, TargetKind: "Interface", Args: args,
	}
}

// TestInstantiationName pins the mangling rules: backtick-arity stripping,
// primitive atoms, "Of"/"And" joining, ApiRef names, and nested recursion.
func TestInstantiationName(t *testing.T) {
	regionRef := winrtmeta.TypeRef{Kind: "ApiRef", Namespace: "Windows.Globalization", Name: "GeographicRegion", TargetKind: "Class"}
	for _, testCase := range []struct {
		ref  winrtmeta.TypeRef
		want string
	}{
		{instRef("IVectorView`1", nativeRef("HString")), "IVectorViewOfString"},
		{instRef("IIterator`1", nativeRef("HString")), "IIteratorOfString"},
		{instRef("IVector`1", nativeRef("Bool")), "IVectorOfBool"},
		{instRef("IMap`2", nativeRef("HString"), nativeRef("I4")), "IMapOfStringAndInt32"},
		{instRef("IKeyValuePair`2", nativeRef("Guid"), nativeRef("Object")), "IKeyValuePairOfGuidAndObject"},
		{instRef("IVectorView`1", regionRef), "IVectorViewOfGeographicRegion"},
		{instRef("IMapView`2", nativeRef("HString"), instRef("IVectorView`1", nativeRef("HString"))),
			"IMapViewOfStringAndIVectorViewOfString"},
		{instRef("IIterable`1", instRef("IKeyValuePair`2", nativeRef("HString"), nativeRef("U8"))),
			"IIterableOfIKeyValuePairOfStringAndUInt64"},
	} {
		got, err := instantiationName(&testCase.ref)
		if err != nil {
			t.Errorf("instantiationName(%s): %v", refDisplay(&testCase.ref), err)
			continue
		}
		if got != testCase.want {
			t.Errorf("instantiationName(%s) = %s, want %s", refDisplay(&testCase.ref), got, testCase.want)
		}
	}

	// Unnameable shapes must error (the requesting member degrades).
	for _, bad := range []winrtmeta.TypeRef{
		instRef("IVectorView`1", winrtmeta.TypeRef{Kind: "Array", Elem: refPtr(nativeRef("I4"))}),
		instRef("IVectorView`1", nativeRef("Void")),
		instRef("IVectorView`1", paramRef(0)), // unbound parameter
		nativeRef("HString"),                  // not an instantiation at all
	} {
		if name, err := instantiationName(&bad); err == nil {
			t.Errorf("instantiationName(%s) = %s, want error", refDisplay(&bad), name)
		}
	}
}

// TestSubstituteRef pins substitution: arity-2 index mapping, recursion into
// nested instantiation arguments, array elements, and deep-copy isolation.
func TestSubstituteRef(t *testing.T) {
	args := []winrtmeta.TypeRef{nativeRef("HString"), nativeRef("I4")}

	if got := substituteRef(refPtr(paramRef(1)), args); got.Kind != "Native" || got.Name != "I4" {
		t.Errorf("T1 substitution = %+v, want Native I4", got)
	}

	// IIterable`1<IKeyValuePair`2<T0, T1>> → IIterable`1<IKeyValuePair`2<String, Int32>>.
	nested := instRef("IIterable`1", instRef("IKeyValuePair`2", paramRef(0), paramRef(1)))
	grounded := substituteRef(&nested, args)
	pair := grounded.Args[0]
	if pair.Args[0].Name != "HString" || pair.Args[1].Name != "I4" {
		t.Errorf("nested substitution = %+v", grounded)
	}
	// The source must be untouched (deep copy, not aliasing).
	if nested.Args[0].Args[0].Kind != "GenericParamRef" {
		t.Error("substituteRef mutated its input")
	}

	// Array elements substitute too (the member still degrades at emit, but
	// the shape must ground for diagnostics).
	array := winrtmeta.TypeRef{Kind: "Array", Elem: refPtr(paramRef(0))}
	if got := substituteRef(&array, args); got.Elem.Name != "HString" {
		t.Errorf("array elem substitution = %+v", got)
	}
}

// collectionsRegistry builds a registry holding the real open Collections
// shapes (methods in true MethodDef order) plus a consumer namespace whose
// members reference closed instantiations of them.
func collectionsRegistry() *pipeline.Registry {
	collections := &winrtmeta.NamespaceMeta{
		Namespace: "Windows.Foundation.Collections",
		Interfaces: map[string]winrtmeta.Interface{
			"IIterable`1": {
				GUID: "faa585ea-6214-4217-afda-7f46de5869b3", Arity: 1,
				Methods: []winrtmeta.Method{
					{Name: "First", Return: refPtr(instRef("IIterator`1", paramRef(0)))}, // slot 6
				},
			},
			"IIterator`1": {
				GUID: "6a79e863-4300-459a-9966-cbb660963ee1", Arity: 1,
				Methods: []winrtmeta.Method{
					{Name: "get_Current", Return: refPtr(paramRef(0))},          // slot 6
					{Name: "get_HasCurrent", Return: refPtr(nativeRef("Bool"))}, // slot 7
					{Name: "MoveNext", Return: refPtr(nativeRef("Bool"))},       // slot 8
					{Name: "GetMany", Params: []winrtmeta.Param{{Name: "items", Out: true, // slot 9: array skip
						Type: winrtmeta.TypeRef{Kind: "Array", Elem: refPtr(paramRef(0))}}},
						Return: refPtr(nativeRef("U4"))},
				},
			},
			"IVectorView`1": {
				GUID: "bbe1fa4c-b0e3-4583-baef-1f1b2e483e56", Arity: 1,
				Requires: []winrtmeta.TypeRef{instRef("IIterable`1", paramRef(0))},
				Methods: []winrtmeta.Method{
					{Name: "GetAt", Params: []winrtmeta.Param{{Name: "index", Type: nativeRef("U4")}},
						Return: refPtr(paramRef(0))}, // slot 6
					{Name: "get_Size", Return: refPtr(nativeRef("U4"))}, // slot 7
					{Name: "IndexOf", Params: []winrtmeta.Param{ // slot 8
						{Name: "value", Type: paramRef(0)},
						{Name: "index", Type: nativeRef("U4"), Out: true}},
						Return: refPtr(nativeRef("Bool"))},
					{Name: "GetMany", Params: []winrtmeta.Param{ // slot 9: array skip
						{Name: "startIndex", Type: nativeRef("U4")},
						{Name: "items", Out: true, Type: winrtmeta.TypeRef{Kind: "Array", Elem: refPtr(paramRef(0))}}},
						Return: refPtr(nativeRef("U4"))},
				},
			},
			"IVector`1": {
				GUID: "913337e9-11a1-4345-a3a2-4e7f956e222d", Arity: 1,
				Methods: []winrtmeta.Method{
					{Name: "GetAt", Params: []winrtmeta.Param{{Name: "index", Type: nativeRef("U4")}},
						Return: refPtr(paramRef(0))}, // slot 6
					{Name: "get_Size", Return: refPtr(nativeRef("U4"))},                      // slot 7
					{Name: "GetView", Return: refPtr(instRef("IVectorView`1", paramRef(0)))}, // slot 8
				},
			},
			"IKeyValuePair`2": {
				GUID: "02b51929-c1c4-4a7e-8940-0312b5c18500", Arity: 2,
				Methods: []winrtmeta.Method{
					{Name: "get_Key", Return: refPtr(paramRef(0))},   // slot 6
					{Name: "get_Value", Return: refPtr(paramRef(1))}, // slot 7
				},
			},
			"IMap`2": {
				GUID: "3c2925fe-8519-45c1-aa79-197b6718c1c1", Arity: 2,
				Requires: []winrtmeta.TypeRef{
					instRef("IIterable`1", instRef("IKeyValuePair`2", paramRef(0), paramRef(1))),
				},
				Methods: []winrtmeta.Method{
					{Name: "Lookup", Params: []winrtmeta.Param{{Name: "key", Type: paramRef(0)}},
						Return: refPtr(paramRef(1))}, // slot 6
					{Name: "get_Size", Return: refPtr(nativeRef("U4"))}, // slot 7
				},
			},
		},
	}
	test := &winrtmeta.NamespaceMeta{
		Namespace: "Windows.Test",
		Interfaces: map[string]winrtmeta.Interface{
			"IConsumer": {
				GUID: "ca30221d-86d9-40fb-a26b-d44eb7cf08ea",
				Methods: []winrtmeta.Method{
					{Name: "get_Names", Return: refPtr(instRef("IVectorView`1", nativeRef("HString")))},
					{Name: "GetEditable", Return: refPtr(instRef("IVector`1", nativeRef("HString")))},
					{Name: "Feed", Params: []winrtmeta.Param{
						{Name: "source", Type: instRef("IIterable`1", nativeRef("HString"))}}},
					{Name: "GetCounts", Return: refPtr(instRef("IMap`2", nativeRef("HString"), nativeRef("I4")))},
					// A second reference to the same instantiation dedupes.
					{Name: "get_MoreNames", Return: refPtr(instRef("IVectorView`1", nativeRef("HString")))},
				},
			},
		},
	}
	registry := &pipeline.Registry{
		Namespaces:     []*winrtmeta.NamespaceMeta{collections, test},
		ByNamespace:    map[string]*winrtmeta.NamespaceMeta{collections.Namespace: collections, test.Namespace: test},
		EnumIndex:      map[string]*winrtmeta.Enum{},
		StructIndex:    map[string]*winrtmeta.Struct{},
		InterfaceIndex: map[string]*winrtmeta.Interface{},
		ClassIndex:     map[string]*winrtmeta.Class{},
		DelegateIndex:  map[string]*winrtmeta.Delegate{},
	}
	for _, meta := range registry.Namespaces {
		for name := range meta.Interfaces {
			definition := meta.Interfaces[name]
			registry.InterfaceIndex[meta.Namespace+"."+name] = &definition
		}
	}
	return registry
}

// TestInstantiationEmission drives the demand-driven pipeline end to end:
// consumer members request instantiations, the closure runs to a fixed
// point (First → IIterator, GetView → IVectorView), models come out sorted
// and deduped, vtable slots are preserved across substituted methods, and
// each IID literal matches the pinterface engine.
func TestInstantiationEmission(t *testing.T) {
	registry := collectionsRegistry()
	generator := New(registry, "example.com/mod", t.TempDir())
	meta := registry.ByNamespace["Windows.Test"]
	generator.prepareNamespaceClaims(meta)

	imports := typemap.ImportSet{}
	interfaceModels := generator.buildInterfaceModels(meta, imports)
	if len(interfaceModels) != 1 {
		t.Fatalf("interface models = %d, want 1", len(interfaceModels))
	}
	consumer := interfaceModels[0]
	byName := map[string]int{} // GoName → slot
	for _, method := range consumer.Methods {
		if method.SkipComment == "" {
			byName[method.GoName] = method.Slot
		}
	}
	if len(byName) != 5 {
		t.Fatalf("emitted consumer methods = %v, want all 5", byName)
	}

	models := generator.buildPinterfaceModels(meta, imports)
	var names []string
	for _, model := range models {
		names = append(names, model.TypeName)
	}
	want := []string{
		"IIterableOfString",    // Feed param
		"IIteratorOfString",    // transitive: IIterable<String>.First
		"IMapOfStringAndInt32", // GetCounts (arity 2)
		"IVectorOfString",      // GetEditable
		"IVectorViewOfString",  // get_Names (+ deduped get_MoreNames, IVector.GetView)
	}
	if strings.Join(names, ",") != strings.Join(want, ",") {
		t.Fatalf("instantiations = %v, want %v (sorted, deduped, transitively closed)", names, want)
	}

	modelByName := map[string]int{}
	for i, model := range models {
		modelByName[model.TypeName] = i
	}

	// Slot preservation on the instantiated IVectorView<String>: skipped
	// members (GetMany's conformant array) keep their slot as a comment.
	vectorView := models[modelByName["IVectorViewOfString"]]
	slots := map[string]int{}
	for _, method := range vectorView.Methods {
		if method.SkipComment == "" {
			slots[method.GoName] = method.Slot
		}
	}
	for goName, wantSlot := range map[string]int{"GetAt": 6, "Size": 7, "IndexOf": 8} {
		if slots[goName] != wantSlot {
			t.Errorf("IVectorViewOfString.%s slot = %d, want %d", goName, slots[goName], wantSlot)
		}
	}
	if len(vectorView.Methods) != 4 || !strings.HasPrefix(vectorView.Methods[3].SkipComment, "slot 9: GetMany skipped:") {
		t.Errorf("IVectorViewOfString methods = %+v, want GetMany slot comment at entry 3", vectorView.Methods)
	}
	if vectorView.FullName != "Windows.Foundation.Collections.IVectorView`1<String>" {
		t.Errorf("IVectorViewOfString FullName = %q", vectorView.FullName)
	}
	if len(vectorView.Requires) != 1 || vectorView.Requires[0] != "Windows.Foundation.Collections.IIterable`1<String>" {
		t.Errorf("IVectorViewOfString Requires = %v", vectorView.Requires)
	}

	// Arity-2 substitution: IMap<String, Int32>.Lookup(key string) (int32, _).
	mapModel := models[modelByName["IMapOfStringAndInt32"]]
	lookup := mapModel.Methods[0]
	if lookup.GoName != "Lookup" || lookup.Slot != 6 ||
		lookup.ParamStr != "key string" || lookup.ReturnSig != "(int32, error)" {
		t.Errorf("IMapOfStringAndInt32.Lookup = %+v", lookup)
	}

	// Every IID literal must be exactly what the pinterface engine derives.
	for mangled, inst := range generator.pinstByName {
		iid, err := pinterface.InstanceIID(inst, registry)
		if err != nil {
			t.Fatalf("InstanceIID(%s): %v", mangled, err)
		}
		literal, err := guidLiteral(iid)
		if err != nil {
			t.Fatalf("guidLiteral(%s): %v", iid, err)
		}
		model := models[modelByName[mangled]]
		if model.IIDVar != "IID_"+mangled || model.IIDLiteral != literal {
			t.Errorf("%s IID = %s / %s, want IID_%s / %s", mangled, model.IIDVar, model.IIDLiteral, mangled, literal)
		}
	}

	// The known published IID for IVector<String> pins the whole chain.
	wantVectorIID, err := guidLiteral("98b9acc1-4b56-532e-ac73-03d5291cca90")
	if err != nil {
		t.Fatal(err)
	}
	if got := models[modelByName["IVectorOfString"]].IIDLiteral; got != wantVectorIID {
		t.Errorf("IID(IVector<String>) literal = %s, want %s", got, wantVectorIID)
	}
}
