package emitwinrt

import (
	"strings"
	"testing"

	"github.com/deploymenttheory/go-bindings-winrt/internal/codegen/emit/winrt/view"
	"github.com/deploymenttheory/go-bindings-winrt/internal/codegen/pipeline"
	"github.com/deploymenttheory/go-bindings-winrt/internal/codegen/typemap"
	"github.com/deploymenttheory/go-bindings-winrt/internal/winrtmeta"
)

// asyncRegistry mirrors the real Windows.Foundation async IR shapes —
// IAsyncOperation`1/IAsyncAction with put_Completed/get_Completed/GetResults,
// their Completed delegates, IAsyncInfo, AsyncStatus — plus a Windows.Test
// consumer exercising the delegate-parameter lowering paths.
func asyncRegistry() *pipeline.Registry {
	statusRef := winrtmeta.TypeRef{Kind: "ApiRef", Namespace: "Windows.Foundation", Name: "AsyncStatus", TargetKind: "Enum"}
	hresultRef := winrtmeta.TypeRef{Kind: "ApiRef", Namespace: "Windows.Foundation", Name: "HResult", TargetKind: "Struct"}
	actionRef := winrtmeta.TypeRef{Kind: "ApiRef", Namespace: "Windows.Foundation", Name: "IAsyncAction", TargetKind: "Interface"}
	actionHandlerRef := delegateRef("Windows.Foundation", "AsyncActionCompletedHandler")
	operationOf := func(arg winrtmeta.TypeRef) winrtmeta.TypeRef {
		return winrtmeta.TypeRef{Kind: "GenericInst", Namespace: "Windows.Foundation", Name: "IAsyncOperation`1",
			TargetKind: "Interface", Args: []winrtmeta.TypeRef{arg}}
	}
	operationHandlerOf := func(arg winrtmeta.TypeRef) winrtmeta.TypeRef {
		return delegateRef("Windows.Foundation", "AsyncOperationCompletedHandler`1", arg)
	}

	foundation := &winrtmeta.NamespaceMeta{
		Namespace: "Windows.Foundation",
		Enums: map[string]winrtmeta.Enum{
			"AsyncStatus": {BaseType: "int32", Members: []winrtmeta.EnumMember{
				{Name: "Started", Value: "0"}, {Name: "Completed", Value: "1"},
				{Name: "Canceled", Value: "2"}, {Name: "Error", Value: "3"}}},
		},
		Interfaces: map[string]winrtmeta.Interface{
			"IAsyncInfo": {
				GUID: "00000036-0000-0000-c000-000000000046",
				Methods: []winrtmeta.Method{
					{Name: "get_Id", Return: refPtr(nativeRef("U4"))},
					{Name: "get_Status", Return: refPtr(statusRef)},
					{Name: "get_ErrorCode", Return: refPtr(hresultRef)},
					{Name: "Cancel"},
					{Name: "Close"},
				},
			},
			"IAsyncAction": {
				GUID: "5a648006-843a-4da9-865b-9d26e5dfad7b",
				Methods: []winrtmeta.Method{
					{Name: "put_Completed", Params: []winrtmeta.Param{{Name: "handler", Type: actionHandlerRef}}}, // slot 6
					{Name: "get_Completed", Return: refPtr(actionHandlerRef)},                                     // slot 7
					{Name: "GetResults"}, // slot 8
				},
			},
			"IAsyncOperation`1": {
				GUID: "9fc2b0bb-e446-44e2-aa61-9cab8f636af2", Arity: 1,
				Methods: []winrtmeta.Method{
					{Name: "put_Completed", Params: []winrtmeta.Param{{Name: "handler", Type: operationHandlerOf(paramRef(0))}}}, // slot 6
					{Name: "get_Completed", Return: refPtr(operationHandlerOf(paramRef(0)))},                                     // slot 7
					{Name: "GetResults", Return: refPtr(paramRef(0))},                                                            // slot 8
				},
			},
		},
		Delegates: map[string]winrtmeta.Delegate{
			"AsyncActionCompletedHandler": {
				GUID: "a4ed5c81-76c9-40bd-8be6-b1d90fb20ae7",
				Invoke: winrtmeta.Method{Name: "Invoke", Params: []winrtmeta.Param{
					{Name: "asyncInfo", Type: actionRef},
					{Name: "asyncStatus", Type: statusRef},
				}},
			},
			"AsyncOperationCompletedHandler`1": {
				GUID: "fcdcf02c-e5d8-4478-915a-4d90b74b83a5", Arity: 1,
				Invoke: winrtmeta.Method{Name: "Invoke", Params: []winrtmeta.Param{
					{Name: "asyncInfo", Type: operationOf(paramRef(0))},
					{Name: "asyncStatus", Type: statusRef},
				}},
			},
		},
	}
	test := &winrtmeta.NamespaceMeta{
		Namespace: "Windows.Test",
		Delegates: map[string]winrtmeta.Delegate{
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
		},
		Interfaces: map[string]winrtmeta.Interface{
			"IFetcher": {
				GUID: "ca30221d-86d9-40fb-a26b-d44eb7cf08ea",
				Methods: []winrtmeta.Method{
					{Name: "FetchAsync", Return: refPtr(operationOf(nativeRef("HString")))},                                                // slot 6
					{Name: "Register", Params: []winrtmeta.Param{{Name: "handler", Type: delegateRef("Windows.Test", "CountHandler")}}},    // slot 7
					{Name: "Handler", Return: refPtr(delegateRef("Windows.Test", "CountHandler"))},                                         // slot 8: delegate RETURN stays skipped
					{Name: "RegisterWide", Params: []winrtmeta.Param{{Name: "handler", Type: delegateRef("Windows.Test", "WideHandler")}}}, // slot 9: un-adaptable
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
		for name := range meta.Enums {
			definition := meta.Enums[name]
			registry.EnumIndex[meta.Namespace+"."+name] = &definition
		}
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

// TestDelegateParamLowering pins the method-parameter delegate seam: an
// adaptable delegate parameter grounds a package-local handler and lowers to
// handler *<Handler> with a nil-safe Ptr() preamble; delegate RETURNS and
// un-adaptable delegate parameters keep today's diagnostics.
func TestDelegateParamLowering(t *testing.T) {
	registry := asyncRegistry()
	generator := New(registry, "example.com/mod", t.TempDir())
	meta := registry.ByNamespace["Windows.Test"]
	generator.prepareNamespaceClaims(meta)

	var fetcher *view.InterfaceModel
	for _, model := range generator.buildInterfaceModels(meta, typemap.ImportSet{}) {
		if model.TypeName == "IFetcher" {
			copied := model
			fetcher = &copied
		}
	}
	if fetcher == nil {
		t.Fatal("IFetcher not emitted")
	}
	byName := map[string]view.MethodModel{}
	var skipComments []string
	for _, method := range fetcher.Methods {
		if method.SkipComment != "" {
			skipComments = append(skipComments, method.SkipComment)
			continue
		}
		byName[method.GoName] = method
	}

	register, ok := byName["Register"]
	if !ok {
		t.Fatal("Register not emitted")
	}
	if register.Slot != 7 || register.ParamStr != "handler *CountHandler" {
		t.Errorf("Register lowering = %+v", register)
	}
	preamble := strings.Join(register.Preamble, "\n")
	if !strings.Contains(preamble, "_handler := uintptr(0)") ||
		!strings.Contains(preamble, "if handler != nil {") ||
		!strings.Contains(preamble, "_handler = handler.Ptr()") {
		t.Errorf("Register preamble = %q", preamble)
	}
	if strings.Join(register.ArgExprs, ",") != "_handler" {
		t.Errorf("Register args = %v", register.ArgExprs)
	}
	comments := strings.Join(register.CommentLines, "\n")
	if !strings.Contains(comments, "A nil handler passes NULL") {
		t.Errorf("Register comments missing the nil note: %q", comments)
	}

	// Returns and un-adaptable parameters stay skipped under today's keys.
	for _, want := range []string{
		"slot 8: Handler skipped: delegate Windows.Test.CountHandler",
		"slot 9: RegisterWide skipped: delegate Windows.Test.WideHandler",
	} {
		found := false
		for _, comment := range skipComments {
			if strings.HasPrefix(comment, want) {
				found = true
			}
		}
		if !found {
			t.Errorf("missing skip comment %q in %v", want, skipComments)
		}
	}
	diagnostics := strings.Join(generator.Diagnostics, "\n")
	for _, want := range []string{
		"delegate-param-skipped: Windows.Test.IFetcher.Handler (delegate Windows.Test.CountHandler)",
		"delegate-param-skipped: Windows.Test.IFetcher.RegisterWide (delegate Windows.Test.WideHandler)",
	} {
		if !strings.Contains(diagnostics, want) {
			t.Errorf("diagnostics missing %q:\n%s", want, diagnostics)
		}
	}

	// Only the adaptable delegate grounded a handler.
	if len(generator.pdelModels) != 1 || generator.pdelModels[0].TypeName != "CountHandler" {
		t.Errorf("grounded handlers = %+v, want just CountHandler", generator.pdelModels)
	}
}

// TestAwaitOperationModel pins the synthesized Await on a monomorphized
// IAsyncOperation`1 in a consuming package: put_Completed emits as
// SetCompleted (grounding the Completed handler), get_Completed stays a
// skipped slot, and the Await view model is fully qualified against
// Windows.Foundation.
func TestAwaitOperationModel(t *testing.T) {
	registry := asyncRegistry()
	generator := New(registry, "example.com/mod", t.TempDir())
	meta := registry.ByNamespace["Windows.Test"]
	generator.prepareNamespaceClaims(meta)

	imports := typemap.ImportSet{}
	generator.buildInterfaceModels(meta, imports) // FetchAsync requests IAsyncOperation<String>
	var operation *view.InterfaceModel
	for _, model := range generator.buildPinterfaceModels(meta, imports) {
		if model.TypeName == "IAsyncOperationOfString" {
			copied := model
			operation = &copied
		}
	}
	if operation == nil {
		t.Fatal("IAsyncOperationOfString not instantiated")
	}

	byName := map[string]view.MethodModel{}
	var skipComments []string
	for _, method := range operation.Methods {
		if method.SkipComment != "" {
			skipComments = append(skipComments, method.SkipComment)
			continue
		}
		byName[method.GoName] = method
	}
	setCompleted, ok := byName["SetCompleted"]
	if !ok || setCompleted.Slot != 6 || setCompleted.ParamStr != "handler *AsyncOperationCompletedHandlerOfString" {
		t.Errorf("SetCompleted = %+v (ok=%v)", setCompleted, ok)
	}
	if len(skipComments) != 1 || !strings.HasPrefix(skipComments[0], "slot 7: get_Completed skipped:") {
		t.Errorf("get_Completed skip = %v", skipComments)
	}
	if _, ok := byName["GetResults"]; !ok {
		t.Error("GetResults not emitted")
	}

	await := operation.Await
	if await == nil {
		t.Fatal("Await model not attached")
	}
	if await.ReturnSig != "(string, error)" ||
		await.StatusType != "foundation.AsyncStatus" ||
		await.StatusCompleted != "foundation.AsyncStatusCompleted" ||
		await.HandlerCtor != "NewAsyncOperationCompletedHandlerOfString" ||
		await.InfoType != "foundation.IAsyncInfo" ||
		await.InfoIIDRef != "&foundation.IID_IAsyncInfo" ||
		await.ErrPrefix != `"", ` {
		t.Errorf("Await model = %+v", await)
	}
	if _, ok := imports["foundation"]; !ok {
		t.Errorf("foundation import not recorded: %v", imports)
	}

	// The grounded handler adapts (asyncInfo, asyncStatus) — the same shape
	// Await's closure assumes.
	var handler *view.DelegateModel
	for i := range generator.pdelModels {
		if generator.pdelModels[i].TypeName == "AsyncOperationCompletedHandlerOfString" {
			handler = &generator.pdelModels[i]
		}
	}
	if handler == nil {
		t.Fatal("AsyncOperationCompletedHandlerOfString not grounded")
	}
	if handler.FnParams != "asyncInfo *IAsyncOperationOfString, asyncStatus foundation.AsyncStatus" ||
		handler.ParamCount != 2 {
		t.Errorf("handler adapter = %+v", handler)
	}
}

// TestAwaitActionModel pins Await on the plain IAsyncAction inside
// Windows.Foundation itself: unqualified support types, no logical result.
func TestAwaitActionModel(t *testing.T) {
	registry := asyncRegistry()
	generator := New(registry, "example.com/mod", t.TempDir())
	meta := registry.ByNamespace["Windows.Foundation"]
	generator.prepareNamespaceClaims(meta)

	var action *view.InterfaceModel
	for _, model := range generator.buildInterfaceModels(meta, typemap.ImportSet{}) {
		if model.TypeName == "IAsyncAction" {
			copied := model
			action = &copied
		}
	}
	if action == nil {
		t.Fatal("IAsyncAction not emitted")
	}
	await := action.Await
	if await == nil {
		t.Fatal("Await model not attached")
	}
	if await.ReturnSig != "error" ||
		await.StatusType != "AsyncStatus" ||
		await.StatusCompleted != "AsyncStatusCompleted" ||
		await.HandlerCtor != "NewAsyncActionCompletedHandler" ||
		await.InfoType != "IAsyncInfo" ||
		await.InfoIIDRef != "&IID_IAsyncInfo" ||
		await.ErrPrefix != "" {
		t.Errorf("Await model = %+v", await)
	}
}
