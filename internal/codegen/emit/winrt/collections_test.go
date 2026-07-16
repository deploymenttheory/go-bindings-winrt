package emitwinrt

import (
	"strings"
	"testing"

	"github.com/deploymenttheory/go-bindings-winrt/internal/codegen/emit/winrt/view"
	"github.com/deploymenttheory/go-bindings-winrt/internal/codegen/typemap"
	"github.com/deploymenttheory/go-bindings-winrt/internal/winrtmeta"
)

// collectionCtorRegistry extends the shared collections fixture with a
// consumer whose members cover every constructor decision: string, scalar,
// GUID, interface-pointer, and codec-less (Bool) elements across all three
// constructible shapes.
func collectionCtorRegistry() *winrtmeta.NamespaceMeta {
	return &winrtmeta.NamespaceMeta{
		Namespace: "Windows.Test",
		Interfaces: map[string]winrtmeta.Interface{
			"IWidget": {
				GUID: "0a0b0c0d-1111-2222-3333-444455556666",
				Methods: []winrtmeta.Method{
					{Name: "Poke"}, // slot 6
				},
			},
			"IConsumer": {
				GUID: "ca30221d-86d9-40fb-a26b-d44eb7cf08ea",
				Methods: []winrtmeta.Method{
					{Name: "get_Names", Return: refPtr(instRef("IVectorView`1", nativeRef("HString")))},
					{Name: "GetEditable", Return: refPtr(instRef("IVector`1", nativeRef("HString")))},
					{Name: "Feed", Params: []winrtmeta.Param{
						{Name: "source", Type: instRef("IIterable`1", nativeRef("HString"))}}},
					{Name: "GetFlags", Return: refPtr(instRef("IIterable`1", nativeRef("Bool")))},
					{Name: "GetIds", Return: refPtr(instRef("IIterable`1", nativeRef("Guid")))},
					{Name: "GetCounts", Return: refPtr(instRef("IVector`1", nativeRef("I4")))},
					{Name: "GetWidgets", Return: refPtr(instRef("IIterable`1", winrtmeta.TypeRef{
						Kind: "ApiRef", Namespace: "Windows.Test", Name: "IWidget", TargetKind: "Interface"}))},
				},
			},
		},
	}
}

// TestCollectionCtorEmission drives the constructor gather end to end over
// the fixture registry: codec selection and boxing per element kind, sibling
// IID wiring, the transitive sibling requests, and the codec-less skip.
func TestCollectionCtorEmission(t *testing.T) {
	registry := collectionsRegistry()
	test := collectionCtorRegistry()
	registry.Namespaces[1] = test
	registry.ByNamespace["Windows.Test"] = test
	for name := range test.Interfaces {
		definition := test.Interfaces[name]
		registry.InterfaceIndex["Windows.Test."+name] = &definition
	}

	generator := New(registry, "example.com/mod", t.TempDir())
	meta := registry.ByNamespace["Windows.Test"]
	generator.prepareNamespaceClaims(meta)

	imports := typemap.ImportSet{}
	generator.buildInterfaceModels(meta, imports)
	models := generator.buildPinterfaceModels(meta, imports)
	byName := map[string]*view.InterfaceModel{}
	for i := range models {
		byName[models[i].TypeName] = &models[i]
	}

	// The vector ctor's sibling requests must have grounded the full String
	// family in-package (IIterable/IIterator arrive via Feed's transitive
	// closure anyway; IVectorView via get_Names — dedup keeps one of each).
	for _, expected := range []string{
		"IIterableOfString", "IIteratorOfString", "IVectorViewOfString", "IVectorOfString",
		"IIterableOfBool", "IIterableOfGuid", "IIteratorOfGuid",
		"IVectorOfInt32", "IIterableOfInt32", "IIteratorOfInt32", "IVectorViewOfInt32",
		"IIterableOfIWidget", "IIteratorOfIWidget",
	} {
		if byName[expected] == nil {
			t.Errorf("instantiation %s missing (have %v)", expected, mapKeys(byName))
		}
	}

	assertCtor := func(typeName string, want view.CollectionCtorModel) {
		t.Helper()
		model := byName[typeName]
		if model == nil {
			t.Fatalf("%s not emitted", typeName)
		}
		ctor := model.CollectionCtor
		if ctor == nil {
			t.Fatalf("%s has no collection ctor", typeName)
		}
		if ctor.CtorName != want.CtorName || ctor.ElemType != want.ElemType ||
			ctor.BoxExpr != want.BoxExpr || ctor.RuntimeCtor != want.RuntimeCtor ||
			ctor.Codec != want.Codec || ctor.IIDs != want.IIDs || ctor.Class != want.Class {
			t.Errorf("%s ctor = %+v, want %+v", typeName, *ctor, want)
		}
	}

	assertCtor("IIterableOfString", view.CollectionCtorModel{
		CtorName: "NewIIterableOfString", ElemType: "string", BoxExpr: "item",
		RuntimeCtor: "NewIterableObject", Codec: "winrt.CodecString",
		Class: "Windows.Foundation.Collections.IIterable`1<String>",
		IIDs:  "winrt.CollectionIIDs{Iterable: IID_IIterableOfString, Iterator: IID_IIteratorOfString}",
	})
	assertCtor("IVectorViewOfString", view.CollectionCtorModel{
		CtorName: "NewIVectorViewOfString", ElemType: "string", BoxExpr: "item",
		RuntimeCtor: "NewVectorViewObject", Codec: "winrt.CodecString",
		Class: "Windows.Foundation.Collections.IVectorView`1<String>",
		IIDs:  "winrt.CollectionIIDs{Iterable: IID_IIterableOfString, Iterator: IID_IIteratorOfString, VectorView: IID_IVectorViewOfString}",
	})
	assertCtor("IVectorOfString", view.CollectionCtorModel{
		CtorName: "NewIVectorOfString", ElemType: "string", BoxExpr: "item",
		RuntimeCtor: "NewVectorObject", Codec: "winrt.CodecString",
		Class: "Windows.Foundation.Collections.IVector`1<String>",
		IIDs:  "winrt.CollectionIIDs{Iterable: IID_IIterableOfString, Iterator: IID_IIteratorOfString, VectorView: IID_IVectorViewOfString, Vector: IID_IVectorOfString}",
	})
	assertCtor("IVectorOfInt32", view.CollectionCtorModel{
		CtorName: "NewIVectorOfInt32", ElemType: "int32", BoxExpr: "uint64(item)",
		RuntimeCtor: "NewVectorObject", Codec: "winrt.CodecScalar(4)",
		Class: "Windows.Foundation.Collections.IVector`1<Int32>",
		IIDs:  "winrt.CollectionIIDs{Iterable: IID_IIterableOfInt32, Iterator: IID_IIteratorOfInt32, VectorView: IID_IVectorViewOfInt32, Vector: IID_IVectorOfInt32}",
	})
	assertCtor("IIterableOfGuid", view.CollectionCtorModel{
		CtorName: "NewIIterableOfGuid", ElemType: "win32.GUID", BoxExpr: "item",
		RuntimeCtor: "NewIterableObject", Codec: "winrt.CodecGuid",
		Class: "Windows.Foundation.Collections.IIterable`1<Guid>",
		IIDs:  "winrt.CollectionIIDs{Iterable: IID_IIterableOfGuid, Iterator: IID_IIteratorOfGuid}",
	})
	assertCtor("IIterableOfIWidget", view.CollectionCtorModel{
		CtorName: "NewIIterableOfIWidget", ElemType: "*IWidget", BoxExpr: "uintptr(unsafe.Pointer(item))",
		RuntimeCtor: "NewIterableObject", Codec: "winrt.CodecInterface",
		Class: "Windows.Foundation.Collections.IIterable`1<Windows.Test.IWidget>",
		IIDs:  "winrt.CollectionIIDs{Iterable: IID_IIterableOfIWidget, Iterator: IID_IIteratorOfIWidget}",
	})

	// Bool has no codec: the type emits, the ctor does not, and the skip is
	// recorded under the soft diagnostic key.
	if bool_ := byName["IIterableOfBool"]; bool_ == nil || bool_.CollectionCtor != nil {
		t.Errorf("IIterableOfBool = %+v, want emitted WITHOUT a ctor", bool_)
	}
	var sawSkip bool
	for _, diagnostic := range generator.Diagnostics {
		if strings.HasPrefix(diagnostic, "collection-ctor-skipped: ") &&
			strings.Contains(diagnostic, "IIterable`1<Bool>") {
			sawSkip = true
		}
	}
	if !sawSkip {
		t.Errorf("no collection-ctor-skipped diagnostic for Bool element; diagnostics = %v", generator.Diagnostics)
	}

	// The interface-element ctor comment must carry the identity-equality
	// caveat; the vector ctor comment the snapshot semantics.
	widgetComment := strings.Join(byName["IIterableOfIWidget"].CollectionCtor.CommentLines, "\n")
	if !strings.Contains(widgetComment, "identity WORDS") {
		t.Errorf("interface-element ctor comment lacks the identity caveat:\n%s", widgetComment)
	}
	vectorComment := strings.Join(byName["IVectorOfString"].CollectionCtor.CommentLines, "\n")
	if !strings.Contains(vectorComment, "SNAPSHOT") {
		t.Errorf("vector ctor comment lacks the snapshot note:\n%s", vectorComment)
	}
}

func mapKeys[V any](m map[string]*V) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	return keys
}
