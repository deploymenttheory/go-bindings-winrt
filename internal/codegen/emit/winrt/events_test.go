package emitwinrt

import (
	"strings"
	"testing"

	"github.com/deploymenttheory/go-bindings-winrt/internal/codegen/emit/winrt/view"
	"github.com/deploymenttheory/go-bindings-winrt/internal/codegen/pinterface"
	"github.com/deploymenttheory/go-bindings-winrt/internal/codegen/pipeline"
	"github.com/deploymenttheory/go-bindings-winrt/internal/codegen/typemap"
	"github.com/deploymenttheory/go-bindings-winrt/internal/winrtmeta"
)

func tokenRef() winrtmeta.TypeRef {
	return winrtmeta.TypeRef{Kind: "ApiRef", Namespace: "Windows.Foundation", Name: "EventRegistrationToken", TargetKind: "Struct"}
}

func delegateRef(namespace, name string, args ...winrtmeta.TypeRef) winrtmeta.TypeRef {
	kind := "ApiRef"
	if len(args) > 0 {
		kind = "GenericInst"
	}
	return winrtmeta.TypeRef{Kind: kind, Namespace: namespace, Name: name, TargetKind: "Delegate", Args: args}
}

// addAccessor/removeAccessor build the metadata shape of event accessors:
// add_ takes the handler and returns the token, remove_ takes the token.
func addAccessor(name string, handlerType winrtmeta.TypeRef) winrtmeta.Method {
	return winrtmeta.Method{Name: name,
		Params: []winrtmeta.Param{{Name: "handler", Type: handlerType}},
		Return: refPtr(tokenRef())}
}

func removeAccessor(name string) winrtmeta.Method {
	return winrtmeta.Method{Name: name,
		Params: []winrtmeta.Param{{Name: "cookie", Type: tokenRef()}}}
}

// eventsRegistry builds a registry exercising every event lowering path: a
// generic TypedEventHandler`2 instantiation, non-generic delegates covering
// each adapter conversion, the unloweable delegate shapes, an orphan
// accessor, and an open generic interface with an event (the pinterface
// path).
func eventsRegistry() *pipeline.Registry {
	thingRef := winrtmeta.TypeRef{Kind: "ApiRef", Namespace: "Windows.Test", Name: "IThing", TargetKind: "Interface"}
	typedClosed := delegateRef("Windows.Foundation", "TypedEventHandler`2", thingRef, nativeRef("Object"))

	foundation := &winrtmeta.NamespaceMeta{
		Namespace: "Windows.Foundation",
		Delegates: map[string]winrtmeta.Delegate{
			"TypedEventHandler`2": {
				GUID: "9de1c534-6ae1-11e0-84e1-18a905bcc53f", Arity: 2,
				Invoke: winrtmeta.Method{Name: "Invoke", Params: []winrtmeta.Param{
					{Name: "sender", Type: paramRef(0)},
					{Name: "args", Type: paramRef(1)},
				}},
			},
		},
	}
	test := &winrtmeta.NamespaceMeta{
		Namespace: "Windows.Test",
		Delegates: map[string]winrtmeta.Delegate{
			"ThingChangedHandler": {
				GUID: "11111111-2222-4333-8444-555555555555",
				Invoke: winrtmeta.Method{Name: "Invoke", Params: []winrtmeta.Param{
					{Name: "sender", Type: thingRef},
					{Name: "message", Type: nativeRef("HString")},
					{Name: "active", Type: nativeRef("Bool")},
				}},
			},
			"CountHandler": {
				GUID: "21111111-2222-4333-8444-555555555555",
				Invoke: winrtmeta.Method{Name: "Invoke", Params: []winrtmeta.Param{
					{Name: "count", Type: nativeRef("I4")},
				}},
			},
			"WideHandler": {
				GUID: "31111111-2222-4333-8444-555555555555",
				Invoke: winrtmeta.Method{Name: "Invoke", Params: []winrtmeta.Param{
					{Name: "a", Type: nativeRef("I4")}, {Name: "b", Type: nativeRef("I4")},
					{Name: "c", Type: nativeRef("I4")}, {Name: "d", Type: nativeRef("I4")},
				}},
			},
			"FloatHandler": {
				GUID: "41111111-2222-4333-8444-555555555555",
				Invoke: winrtmeta.Method{Name: "Invoke", Params: []winrtmeta.Param{
					{Name: "value", Type: nativeRef("F64")},
				}},
			},
			"ReturningHandler": {
				GUID: "51111111-2222-4333-8444-555555555555",
				Invoke: winrtmeta.Method{Name: "Invoke",
					Params: []winrtmeta.Param{{Name: "x", Type: nativeRef("I4")}},
					Return: refPtr(nativeRef("I4"))},
			},
		},
		Interfaces: map[string]winrtmeta.Interface{
			"IThing": {
				GUID: "ca30221d-86d9-40fb-a26b-d44eb7cf08ea",
				Methods: []winrtmeta.Method{
					{Name: "get_Name", Return: refPtr(nativeRef("HString"))},                       // slot 6
					addAccessor("add_Closed", typedClosed),                                         // slot 7
					removeAccessor("remove_Closed"),                                                // slot 8
					addAccessor("add_Changed", delegateRef("Windows.Test", "ThingChangedHandler")), // slot 9
					removeAccessor("remove_Changed"),                                               // slot 10
					addAccessor("add_Counted", delegateRef("Windows.Test", "CountHandler")),        // slot 11
					removeAccessor("remove_Counted"),                                               // slot 12
					addAccessor("add_Wide", delegateRef("Windows.Test", "WideHandler")),            // slot 13
					removeAccessor("remove_Wide"),                                                  // slot 14
					addAccessor("add_Scaled", delegateRef("Windows.Test", "FloatHandler")),         // slot 15
					removeAccessor("remove_Scaled"),                                                // slot 16
					addAccessor("add_Orphan", delegateRef("Windows.Test", "CountHandler")),         // slot 17: no Event entry
					addAccessor("add_Ret", delegateRef("Windows.Test", "ReturningHandler")),        // slot 18
					removeAccessor("remove_Ret"),                                                   // slot 19
					addAccessor("add_Closed2", typedClosed),                                        // slot 20: same delegate, dedupes
					removeAccessor("remove_Closed2"),                                               // slot 21
				},
				Events: []winrtmeta.Event{
					{Name: "Closed", Type: typedClosed, AddMethod: "add_Closed", RemoveMethod: "remove_Closed"},
					{Name: "Changed", Type: delegateRef("Windows.Test", "ThingChangedHandler"), AddMethod: "add_Changed", RemoveMethod: "remove_Changed"},
					{Name: "Counted", Type: delegateRef("Windows.Test", "CountHandler"), AddMethod: "add_Counted", RemoveMethod: "remove_Counted"},
					{Name: "Wide", Type: delegateRef("Windows.Test", "WideHandler"), AddMethod: "add_Wide", RemoveMethod: "remove_Wide"},
					{Name: "Scaled", Type: delegateRef("Windows.Test", "FloatHandler"), AddMethod: "add_Scaled", RemoveMethod: "remove_Scaled"},
					{Name: "Ret", Type: delegateRef("Windows.Test", "ReturningHandler"), AddMethod: "add_Ret", RemoveMethod: "remove_Ret"},
					{Name: "Closed2", Type: typedClosed, AddMethod: "add_Closed2", RemoveMethod: "remove_Closed2"},
				},
			},
			// The pinterface path: an open generic interface with an event
			// whose delegate arguments contain the interface's parameter.
			"IObservable`1": {
				GUID: "aaaa521a-5b71-46bf-a591-6de387601a11", Arity: 1,
				Methods: []winrtmeta.Method{
					{Name: "get_Value", Return: refPtr(paramRef(0))},                                                                       // slot 6
					addAccessor("add_Changed", delegateRef("Windows.Foundation", "TypedEventHandler`2", paramRef(0), nativeRef("Object"))), // slot 7
					removeAccessor("remove_Changed"),                                                                                       // slot 8
				},
				Events: []winrtmeta.Event{
					{Name: "Changed", Type: delegateRef("Windows.Foundation", "TypedEventHandler`2", paramRef(0), nativeRef("Object")),
						AddMethod: "add_Changed", RemoveMethod: "remove_Changed"},
				},
			},
			"IConsumer": {
				GUID: "ba30221d-86d9-40fb-a26b-d44eb7cf08ea",
				Methods: []winrtmeta.Method{
					{Name: "Observe", Return: refPtr(winrtmeta.TypeRef{
						Kind: "GenericInst", Namespace: "Windows.Test", Name: "IObservable`1",
						TargetKind: "Interface", Args: []winrtmeta.TypeRef{nativeRef("HString")},
					})}, // slot 6
				},
			},
		},
	}
	registry := &pipeline.Registry{
		Namespaces:     []*winrtmeta.NamespaceMeta{foundation, test},
		ByNamespace:    map[string]*winrtmeta.NamespaceMeta{foundation.Namespace: foundation, test.Namespace: test},
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
		for name := range meta.Delegates {
			definition := meta.Delegates[name]
			registry.DelegateIndex[meta.Namespace+"."+name] = &definition
		}
	}
	return registry
}

// TestEventAccessorEmission pins the accessor lowering: slot correlation
// through the Events IR, the Add/Remove ABI shapes, orphan accessors, and
// the skip rules for unloweable delegates.
func TestEventAccessorEmission(t *testing.T) {
	registry := eventsRegistry()
	generator := New(registry, "example.com/mod", t.TempDir())
	meta := registry.ByNamespace["Windows.Test"]
	generator.prepareNamespaceClaims(meta)

	imports := typemap.ImportSet{}
	var thing *view.InterfaceModel
	for _, model := range generator.buildInterfaceModels(meta, imports) {
		if model.TypeName == "IThing" {
			copied := model
			thing = &copied
		}
	}
	if thing == nil {
		t.Fatal("IThing not emitted")
	}

	byName := map[string]view.MethodModel{}
	for _, method := range thing.Methods {
		if method.SkipComment == "" {
			byName[method.GoName] = method
		}
	}

	// Emitted accessors correlate the Events IR to their vtable slots.
	for name, slot := range map[string]int{
		"AddClosed": 7, "RemoveClosed": 8, "AddChanged": 9, "RemoveChanged": 10,
		"AddCounted": 11, "RemoveCounted": 12, "AddClosed2": 20, "RemoveClosed2": 21,
	} {
		method, ok := byName[name]
		if !ok {
			t.Errorf("accessor %s not emitted", name)
			continue
		}
		if method.Slot != slot {
			t.Errorf("%s slot = %d, want %d", name, method.Slot, slot)
		}
	}

	// Add: typed handler in, token out through the trailing out-pointer.
	addClosed := byName["AddClosed"]
	if addClosed.ParamStr != "handler *TypedEventHandlerOfIThingAndObject" ||
		addClosed.ReturnSig != "(syswinrt.EventRegistrationToken, error)" ||
		addClosed.ResultDecl != "result := new(syswinrt.EventRegistrationToken)" ||
		strings.Join(addClosed.ArgExprs, ",") != "handler.Ptr(),uintptr(winrt.OutParam(unsafe.Pointer(result)))" {
		t.Errorf("AddClosed lowering = %+v", addClosed)
	}
	// Remove: the token's single word by value.
	removeClosed := byName["RemoveClosed"]
	if removeClosed.ParamStr != "token syswinrt.EventRegistrationToken" ||
		removeClosed.ReturnSig != "error" ||
		strings.Join(removeClosed.ArgExprs, ",") != "uintptr(token.Value)" {
		t.Errorf("RemoveClosed lowering = %+v", removeClosed)
	}
	if byName["AddChanged"].ParamStr != "handler *ThingChangedHandler" {
		t.Errorf("AddChanged params = %q", byName["AddChanged"].ParamStr)
	}

	// Unloweable events keep both accessor slots as comments.
	var skipComments []string
	for _, method := range thing.Methods {
		if method.SkipComment != "" {
			skipComments = append(skipComments, method.SkipComment)
		}
	}
	for _, want := range []string{
		"slot 13: add_Wide skipped:", "slot 14: remove_Wide skipped:",
		"slot 15: add_Scaled skipped:", "slot 16: remove_Scaled skipped:",
		"slot 17: add_Orphan skipped: no event metadata pairs this accessor",
		"slot 18: add_Ret skipped:", "slot 19: remove_Ret skipped:",
	} {
		found := false
		for _, comment := range skipComments {
			if strings.HasPrefix(comment, want) {
				found = true
			}
		}
		if !found {
			t.Errorf("missing skip comment with prefix %q in %v", want, skipComments)
		}
	}

	// Diagnostics: the unloweable reasons are keyed event-delegate-unloweable.
	diagnostics := strings.Join(generator.Diagnostics, "\n")
	for _, want := range []string{
		"event-delegate-unloweable: Windows.Test.IThing.add_Wide (Windows.Test.WideHandler Invoke has 4 parameters (1-3 supported))",
		"event-delegate-unloweable: Windows.Test.IThing.add_Scaled (Windows.Test.FloatHandler Invoke parameter value (float64) has no adapter conversion)",
		"event-delegate-unloweable: Windows.Test.IThing.add_Ret (Windows.Test.ReturningHandler Invoke returns a value)",
		"event-skipped: Windows.Test.IThing.add_Orphan (no event metadata pairs this accessor)",
	} {
		if !strings.Contains(diagnostics, want) {
			t.Errorf("diagnostics missing %q:\n%s", want, diagnostics)
		}
	}
}

// TestEventDelegateModels pins the handler models: mangling, IIDs (declared
// and pinterface-derived), adapter conversions per kind, and per-package
// dedup of a shared delegate instantiation.
func TestEventDelegateModels(t *testing.T) {
	registry := eventsRegistry()
	generator := New(registry, "example.com/mod", t.TempDir())
	meta := registry.ByNamespace["Windows.Test"]
	generator.prepareNamespaceClaims(meta)
	generator.buildInterfaceModels(meta, typemap.ImportSet{})

	models := map[string]view.DelegateModel{}
	for _, model := range generator.pdelModels {
		models[model.TypeName] = model
	}
	// Closed and Closed2 share one instantiation; Wide/Scaled/Ret failed.
	if len(models) != 3 {
		t.Fatalf("delegate models = %d (%v), want 3", len(models), generator.pdelModels)
	}

	typed, ok := models["TypedEventHandlerOfIThingAndObject"]
	if !ok {
		t.Fatal("TypedEventHandlerOfIThingAndObject not built")
	}
	if typed.ParamCount != 2 ||
		typed.FnParams != "sender *IThing, args *syswinrt.IInspectable" ||
		strings.Join(typed.ArgExprs, ",") != "(*IThing)(unsafe.Pointer(raw[0])),(*syswinrt.IInspectable)(unsafe.Pointer(raw[1]))" {
		t.Errorf("typed handler adapter = %+v", typed)
	}
	if typed.CtorName != "NewTypedEventHandlerOfIThingAndObject" ||
		typed.IIDVar != "IID_TypedEventHandlerOfIThingAndObject" {
		t.Errorf("typed handler names = %s / %s", typed.CtorName, typed.IIDVar)
	}
	// The instantiation IID must be exactly the pinterface derivation.
	thingRef := winrtmeta.TypeRef{Kind: "ApiRef", Namespace: "Windows.Test", Name: "IThing", TargetKind: "Interface"}
	inst := delegateRef("Windows.Foundation", "TypedEventHandler`2", thingRef, nativeRef("Object"))
	iid, err := pinterface.InstanceIID(&inst, registry)
	if err != nil {
		t.Fatalf("InstanceIID: %v", err)
	}
	literal, err := guidLiteral(iid)
	if err != nil {
		t.Fatalf("guidLiteral: %v", err)
	}
	if typed.IIDLiteral != literal || typed.GUID != iid {
		t.Errorf("typed handler IID = %s (%s), want %s", typed.GUID, typed.IIDLiteral, iid)
	}

	// Non-generic delegate: declared GUID, string/bool conversions.
	changed := models["ThingChangedHandler"]
	if changed.ParamCount != 3 ||
		changed.FnParams != "sender *IThing, message string, active bool" ||
		strings.Join(changed.ArgExprs, ",") != "(*IThing)(unsafe.Pointer(raw[0])),winrt.HStringToString(syswinrt.HSTRING(raw[1])),raw[2] != 0" {
		t.Errorf("ThingChangedHandler adapter = %+v", changed)
	}
	if changed.GUID != "11111111-2222-4333-8444-555555555555" {
		t.Errorf("ThingChangedHandler GUID = %s", changed.GUID)
	}

	// Scalar conversion.
	counted := models["CountHandler"]
	if counted.ParamCount != 1 || counted.FnParams != "count int32" ||
		strings.Join(counted.ArgExprs, ",") != "int32(raw[0])" {
		t.Errorf("CountHandler adapter = %+v", counted)
	}
}

// TestInstantiatedInterfaceEvents drives the pinterface path: an event on an
// open generic interface survives substitution and emits typed accessors,
// with the substituted delegate instantiation grounded into a handler.
func TestInstantiatedInterfaceEvents(t *testing.T) {
	registry := eventsRegistry()
	generator := New(registry, "example.com/mod", t.TempDir())
	meta := registry.ByNamespace["Windows.Test"]
	generator.prepareNamespaceClaims(meta)

	imports := typemap.ImportSet{}
	generator.buildInterfaceModels(meta, imports) // IConsumer.Observe requests IObservable<String>

	var observable *view.InterfaceModel
	for _, model := range generator.buildPinterfaceModels(meta, imports) {
		if model.TypeName == "IObservableOfString" {
			copied := model
			observable = &copied
		}
	}
	if observable == nil {
		t.Fatal("IObservableOfString not instantiated")
	}
	byName := map[string]view.MethodModel{}
	for _, method := range observable.Methods {
		if method.SkipComment == "" {
			byName[method.GoName] = method
		}
	}
	add, ok := byName["AddChanged"]
	if !ok || add.Slot != 7 || add.ParamStr != "handler *TypedEventHandlerOfStringAndObject" {
		t.Errorf("IObservableOfString.AddChanged = %+v (ok=%v)", add, ok)
	}
	remove, ok := byName["RemoveChanged"]
	if !ok || remove.Slot != 8 || remove.ParamStr != "token syswinrt.EventRegistrationToken" {
		t.Errorf("IObservableOfString.RemoveChanged = %+v (ok=%v)", remove, ok)
	}

	// The substituted delegate instantiation grounded: String sender reads
	// the HSTRING without consuming it.
	var handler *view.DelegateModel
	for i := range generator.pdelModels {
		if generator.pdelModels[i].TypeName == "TypedEventHandlerOfStringAndObject" {
			handler = &generator.pdelModels[i]
		}
	}
	if handler == nil {
		t.Fatal("TypedEventHandlerOfStringAndObject not built")
	}
	if handler.FnParams != "sender string, args *syswinrt.IInspectable" ||
		strings.Join(handler.ArgExprs, ",") != "winrt.HStringToString(syswinrt.HSTRING(raw[0])),(*syswinrt.IInspectable)(unsafe.Pointer(raw[1]))" {
		t.Errorf("substituted handler adapter = %+v", handler)
	}
}
