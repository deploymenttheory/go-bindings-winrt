package emitwinrt

// Event emission. An interface's add_/remove_ accessors occupy vtable slots
// like any other method; the Event IR entry pairs them by MethodDef name and
// carries the delegate type. An emittable event becomes Add<Event> (takes a
// typed handler, returns the EventRegistrationToken) and Remove<Event>
// (takes the token) — and the event's delegate type is grounded into a
// package-local handler type in <pkg>_delegates.go: a typed wrapper over the
// runtime's Go-implemented Delegate whose constructor adapts the raw Invoke
// ABI words to typed callback arguments.
//
// Delegates are grounded ONLY when an event requests them this milestone:
// generic delegate instantiations in method parameters keep degrading with
// today's diagnostics (delegate-param-skipped / generic-member-skipped), and
// delegate TypeDefs are still not emitted into their home namespaces. Like
// pinterfaces, two packages consuming the same event delegate each get their
// own handler copy: distinct Go types, identical ABI (same IID).
//
// An event whose delegate cannot be adapted (Invoke with a return value,
// [out] or float/struct/array parameters, or an arity outside the delegate
// runtime's 1–3 raw words) is skipped with an event-delegate-unloweable
// diagnostic; both accessors keep their slot comments.

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/deploymenttheory/go-bindings-winrt/internal/codegen/emit/winrt/view"
	"github.com/deploymenttheory/go-bindings-winrt/internal/codegen/naming"
	"github.com/deploymenttheory/go-bindings-winrt/internal/codegen/pinterface"
	"github.com/deploymenttheory/go-bindings-winrt/internal/codegen/typemap"
	"github.com/deploymenttheory/go-bindings-winrt/internal/winrtmeta"
)

// eventUnloweable builds the degradation for an event whose delegate cannot
// be represented.
func eventUnloweable(format string, args ...any) *skip {
	return &skip{key: "event-delegate-unloweable", detail: fmt.Sprintf(format, args...)}
}

// buildAddAccessor lowers an add_<Event> accessor to
// Add<Event>(handler *<Handler>) (syswinrt.EventRegistrationToken, error),
// grounding the event's delegate type on demand. event is nil when no Event
// IR entry pairs the accessor.
func (g *Generator) buildAddAccessor(meta *winrtmeta.NamespaceMeta, interfaceGoName string, method *winrtmeta.Method, slot int, event *winrtmeta.Event) (view.MethodModel, *skip) {
	if event == nil {
		return view.MethodModel{}, &skip{key: "event-skipped", detail: "no event metadata pairs this accessor"}
	}
	if len(method.Params) != 1 || method.Return == nil {
		return view.MethodModel{}, &skip{key: "event-skipped", detail: fmt.Sprintf("accessor %s has an unexpected shape", method.Name)}
	}
	handlerType, skipped := g.requestEventDelegate(meta, &event.Type)
	if skipped != nil {
		return view.MethodModel{}, skipped
	}
	goName := "Add" + naming.Export(event.Name)
	return view.MethodModel{
		GoName:     goName,
		Slot:       slot,
		ParamStr:   "handler *" + handlerType,
		ReturnSig:  "(syswinrt.EventRegistrationToken, error)",
		ReturnKind: view.RetValue,
		ResultDecl: "var result syswinrt.EventRegistrationToken",
		ResultExpr: "result",
		ZeroReturn: "syswinrt.EventRegistrationToken{}",
		ArgExprs:   []string{"handler.Ptr()", "uintptr(unsafe.Pointer(&result))"},
		CommentLines: []string{
			fmt.Sprintf("%s (event add %s) dispatches through %s's vtable slot %d.", goName, method.Name, interfaceGoName, slot),
			"The handler stays registered (and referenced by the runtime) until the",
			fmt.Sprintf("returned token is passed to Remove%s.", naming.Export(event.Name)),
		},
	}, nil
}

// buildRemoveAccessor lowers a remove_<Event> accessor to
// Remove<Event>(token syswinrt.EventRegistrationToken) error. The event's
// delegate is requested here too, so both accessors of an unloweable event
// skip in lockstep.
func (g *Generator) buildRemoveAccessor(meta *winrtmeta.NamespaceMeta, interfaceGoName string, method *winrtmeta.Method, slot int, event *winrtmeta.Event) (view.MethodModel, *skip) {
	if event == nil {
		return view.MethodModel{}, &skip{key: "event-skipped", detail: "no event metadata pairs this accessor"}
	}
	if len(method.Params) != 1 || method.Return != nil {
		return view.MethodModel{}, &skip{key: "event-skipped", detail: fmt.Sprintf("accessor %s has an unexpected shape", method.Name)}
	}
	if _, skipped := g.requestEventDelegate(meta, &event.Type); skipped != nil {
		return view.MethodModel{}, skipped
	}
	goName := "Remove" + naming.Export(event.Name)
	return view.MethodModel{
		GoName:     goName,
		Slot:       slot,
		ParamStr:   "token syswinrt.EventRegistrationToken",
		ReturnSig:  "error",
		ReturnKind: view.RetError,
		ArgExprs:   []string{"uintptr(token.Value)"},
		CommentLines: []string{
			fmt.Sprintf("%s (event remove %s) dispatches through %s's vtable slot %d,", goName, method.Name, interfaceGoName, slot),
			fmt.Sprintf("unregistering the %s handler the token was returned for.", event.Name),
		},
	}, nil
}

// requestEventDelegate grounds an event's delegate type — a closed generic
// instantiation (TypedEventHandler`2<A, B>) or a non-generic delegate
// reference — into a package-local handler type, deduped by name, and
// returns the handler's Go type name. A non-nil skip degrades the accessor.
func (g *Generator) requestEventDelegate(meta *winrtmeta.NamespaceMeta, ref *winrtmeta.TypeRef) (string, *skip) {
	var handlerName, iid string
	var invoke winrtmeta.Method
	switch ref.Kind {
	case "GenericInst":
		open := g.registry.Delegate(ref.Namespace, ref.Name)
		if open == nil {
			return "", eventUnloweable("%s does not resolve to an open delegate", refDisplay(ref))
		}
		name, err := instantiationName(ref)
		if err != nil {
			return "", eventUnloweable("delegate instantiation %s cannot be named", refDisplay(ref))
		}
		derived, err := pinterface.InstanceIID(ref, g.registry)
		if err != nil {
			return "", eventUnloweable("delegate instantiation %s cannot be grounded: %v", refDisplay(ref), err)
		}
		handlerName, iid = name, derived
		invoke = substituteMethod(&open.Invoke, ref.Args)
	case "ApiRef":
		open := g.registry.Delegate(ref.Namespace, ref.Name)
		if open == nil || open.GUID == "" {
			return "", eventUnloweable("delegate %s unresolved or missing [Guid]", refDisplay(ref))
		}
		if open.Arity > 0 {
			return "", eventUnloweable("delegate %s is an open generic", refDisplay(ref))
		}
		handlerName = naming.Export(ref.Name)
		iid = open.GUID
		invoke = substituteMethod(&open.Invoke, nil)
	default:
		return "", eventUnloweable("event delegate reference kind %q", ref.Kind)
	}

	if existing, seen := g.pdelByName[handlerName]; seen {
		// The same handler name must mean the same delegate: mangled names
		// drop namespaces, so two same-named refs could otherwise silently
		// alias distinct IIDs.
		clone := cloneRef(ref)
		if !reflect.DeepEqual(*existing, clone) {
			return "", eventUnloweable("handler name %s is already bound to a different delegate", handlerName)
		}
		return handlerName, nil
	}
	// The handler's names must all be free in the package.
	if g.claimedNames[handlerName] || g.claimedNames["IID_"+handlerName] || g.claimedNames["New"+handlerName] {
		return "", eventUnloweable("handler name %s collides with an existing declaration", handlerName)
	}

	model, skipped := g.buildDelegateModel(meta, refDisplay(ref), handlerName, iid, &invoke)
	if skipped != nil {
		return "", skipped
	}
	g.claimedNames[handlerName] = true
	g.claimedNames["IID_"+handlerName] = true
	g.claimedNames["New"+handlerName] = true
	clone := cloneRef(ref)
	g.pdelByName[handlerName] = &clone
	g.pdelModels = append(g.pdelModels, model)
	return handlerName, nil
}

// buildDelegateModel lowers a grounded delegate Invoke signature into the
// handler render model. The adapter conversion rules (raw ABI word → typed
// callback argument):
//
//   - interface / class / Object pointers  → typed pointer via unsafe cast
//     (a BORROWED reference, valid only for the callback's duration)
//   - HString                              → winrt.HStringToString (read
//     without consuming the source handle)
//   - Bool                                 → raw != 0
//   - integer scalars and enums            → direct conversion
//
// Anything else — floats (which never crossed the raw word intact), structs,
// arrays, unresolved or unemittable types — makes the event unloweable, as
// does an Invoke with a return value, an [out] parameter, or a parameter
// count outside the delegate runtime's 1–3 raw words.
func (g *Generator) buildDelegateModel(meta *winrtmeta.NamespaceMeta, fullName, goName, iid string, invoke *winrtmeta.Method) (view.DelegateModel, *skip) {
	if invoke.Return != nil {
		return view.DelegateModel{}, eventUnloweable("%s Invoke returns a value", fullName)
	}
	if len(invoke.Params) < 1 || len(invoke.Params) > 3 {
		return view.DelegateModel{}, eventUnloweable("%s Invoke has %d parameters (1-3 supported)", fullName, len(invoke.Params))
	}
	literal, err := guidLiteral(iid)
	if err != nil {
		return view.DelegateModel{}, eventUnloweable("%s IID: %v", fullName, err)
	}

	context := g.resolveContext(meta.Namespace)
	scratch := typemap.ImportSet{}
	model := view.DelegateModel{
		TypeName:   goName,
		FullName:   fullName,
		GUID:       iid,
		IIDVar:     "IID_" + goName,
		IIDLiteral: literal,
		CtorName:   "New" + goName,
		ParamCount: len(invoke.Params),
	}

	var decls, noteLines []string
	taken := map[string]bool{}
	var hasPointer, hasString bool
	for i := range invoke.Params {
		param := &invoke.Params[i]
		if param.Out {
			return view.DelegateModel{}, eventUnloweable("%s Invoke parameter %s is [out]", fullName, param.Name)
		}
		paramName := freshLocal(naming.ParamName(param.Name), taken)
		taken[paramName] = true
		resolved := g.mapper.GoType(&param.Type, context, scratch)
		word := fmt.Sprintf("raw[%d]", i)
		switch resolved.Kind {
		case typemap.KindInterfacePtr, typemap.KindObjectPtr:
			model.ArgExprs = append(model.ArgExprs, "("+resolved.GoType+")(unsafe.Pointer("+word+"))")
			hasPointer = true
		case typemap.KindString:
			model.ArgExprs = append(model.ArgExprs, "winrt.HStringToString(syswinrt.HSTRING("+word+"))")
			hasString = true
		case typemap.KindBool:
			model.ArgExprs = append(model.ArgExprs, word+" != 0")
		case typemap.KindScalar, typemap.KindEnum:
			model.ArgExprs = append(model.ArgExprs, resolved.GoType+"("+word+")")
		case typemap.KindUnsupported:
			return view.DelegateModel{}, eventUnloweable("%s Invoke parameter %s: %s", fullName, param.Name, splitReason(resolved.Reason).detail)
		default:
			return view.DelegateModel{}, eventUnloweable("%s Invoke parameter %s (%s) has no adapter conversion", fullName, param.Name, resolved.GoType)
		}
		if resolved.Note != "" {
			noteLines = append(noteLines, "Parameter "+paramName+"'s "+resolved.Note+".")
		}
		decls = append(decls, paramName+" "+resolved.GoType)
	}
	model.FnParams = strings.Join(decls, ", ")

	model.CtorCommentLines = []string{
		fmt.Sprintf("%s wraps fn as a COM-callable %s.", model.CtorName, fullName),
		"The handler starts with one Go-held reference; Close it once no native",
		"code can still invoke it.",
	}
	if hasPointer {
		model.CtorCommentLines = append(model.CtorCommentLines,
			"Pointer-typed callback arguments are BORROWED references owned by the",
			"event source for the duration of the callback: do not Release them or",
			"retain them past its return.")
	}
	if hasString {
		model.CtorCommentLines = append(model.CtorCommentLines,
			"String arguments are read without consuming the source HSTRING.")
	}
	model.CtorCommentLines = append(model.CtorCommentLines, noteLines...)

	g.pdelImports.Merge(scratch)
	return model, nil
}
