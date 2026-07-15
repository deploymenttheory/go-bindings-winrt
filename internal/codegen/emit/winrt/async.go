package emitwinrt

// Await synthesis. WinRT async objects (Windows.Foundation.IAsyncOperation`1
// instantiations and the plain IAsyncAction) surface completion through
// put_Completed — a delegate parameter, emitted as SetCompleted since the
// delegate-parameter wave. Every such interface whose SetCompleted and
// GetResults both emitted gains an idiomatic blocking helper:
//
//	func (self *IAsyncOperationOfX) Await() (<X>, error)
//	func (self *IAsyncAction) Await() error
//
// Await registers a Completed handler that sends the terminal AsyncStatus
// on a buffered channel, blocks on the receive, and returns GetResults()
// on success or a winrt.AsyncError (status + the IAsyncInfo error code)
// otherwise. The WinRT contract makes this race-free: a handler assigned
// after the operation already completed is invoked immediately. Everything
// the body needs is precomputed here into view.AwaitModel — the template
// stays decision-free.

import (
	"strings"

	"github.com/deploymenttheory/go-bindings-winrt/internal/codegen/emit/winrt/view"
	"github.com/deploymenttheory/go-bindings-winrt/internal/codegen/typemap"
	"github.com/deploymenttheory/go-bindings-winrt/internal/winrtmeta"
)

// asyncSupportRef builds the ApiRef for one of the Windows.Foundation
// support types the Await body leans on.
func asyncSupportRef(name, targetKind string) winrtmeta.TypeRef {
	return winrtmeta.TypeRef{Kind: "ApiRef", Namespace: "Windows.Foundation", Name: name, TargetKind: targetKind}
}

// attachAwait synthesizes the Await view model onto a just-built async
// interface model (an IAsyncOperation`1 instantiation or IAsyncAction).
// Missing prerequisites — SetCompleted or GetResults skipped, the handler
// not grounded, or the Windows.Foundation support types unreachable (a
// severed import edge) — leave the model untouched: the member skips
// already carry their own diagnostics, so no new key is recorded.
func (g *Generator) attachAwait(meta *winrtmeta.NamespaceMeta, model *view.InterfaceModel, imports typemap.ImportSet) {
	var setCompleted, getResults *view.MethodModel
	for i := range model.Methods {
		method := &model.Methods[i]
		if method.SkipComment != "" {
			continue
		}
		switch method.GoName {
		case "SetCompleted":
			setCompleted = method
		case "GetResults":
			getResults = method
		case "Await":
			return // a metadata method already owns the name
		}
	}
	if setCompleted == nil || getResults == nil {
		return
	}
	// The grounded handler is literally in SetCompleted's signature
	// ("handler *AsyncOperationCompletedHandlerOfStorageFile").
	handlerType, ok := strings.CutPrefix(setCompleted.ParamStr, "handler *")
	if !ok || g.pdelByName[handlerType] == nil {
		return
	}

	context := g.resolveContext(meta.Namespace)
	scratch := typemap.ImportSet{}
	statusRef := asyncSupportRef("AsyncStatus", "Enum")
	resolvedStatus := g.mapper.GoType(&statusRef, context, scratch)
	if resolvedStatus.Kind != typemap.KindEnum {
		return
	}
	infoRef := asyncSupportRef("IAsyncInfo", "Interface")
	resolvedInfo := g.mapper.GoType(&infoRef, context, scratch)
	if resolvedInfo.Kind != typemap.KindInterfacePtr {
		return
	}
	infoIIDRef, ok := g.iidRef(&infoRef, meta.Namespace)
	if !ok {
		return
	}

	errPrefix := ""
	if getResults.ReturnSig != "error" {
		errPrefix = getResults.ZeroReturn + ", "
	}
	imports.Merge(scratch)
	model.Await = &view.AwaitModel{
		ReturnSig:       getResults.ReturnSig,
		StatusType:      resolvedStatus.GoType,
		StatusCompleted: resolvedStatus.GoType + "Completed",
		HandlerCtor:     "New" + handlerType,
		InfoType:        strings.TrimPrefix(resolvedInfo.GoType, "*"),
		InfoIIDRef:      infoIIDRef,
		ErrPrefix:       errPrefix,
		CommentLines: []string{
			"Await registers a Completed handler and blocks until " + model.TypeName + " reaches",
			"a terminal state, then returns GetResults() — or, when the status is not",
			"Completed, an error carrying the status and the IAsyncInfo error code (see",
			"winrt.AsyncError). Safe on an operation that already completed: WinRT",
			"invokes a handler assigned after completion immediately. put_Completed",
			"accepts a single assignment per operation, so Await (or SetCompleted) can",
			"be used at most once per instance. Await blocks indefinitely by design; a",
			"context-aware variant is future work. The completion signal is sent from",
			"the handler's Invoke, which the delegate runtime runs on a fresh goroutine",
			"— it never contends with the runtime's callback worker, so a completed",
			"operation cannot deadlock Await.",
		},
	}
}
