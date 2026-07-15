package emitwinrt

import (
	"fmt"
	"strings"

	"github.com/deploymenttheory/go-bindings-winrt/internal/codegen/emit/winrt/view"
	"github.com/deploymenttheory/go-bindings-winrt/internal/codegen/naming"
	"github.com/deploymenttheory/go-bindings-winrt/internal/codegen/typemap"
	"github.com/deploymenttheory/go-bindings-winrt/internal/winrtmeta"
)

// skip describes why a member cannot be emitted: the diagnostic key plus a
// human-readable detail (which also lands in the slot comment).
type skip struct {
	key    string
	detail string
}

// emittedMethod records the Go surface of one emitted interface method so
// package-level wrappers (factory constructors) can mirror the generated
// method exactly. The zero value marks a skipped (or accessor) slot.
type emittedMethod struct {
	emitted  bool
	goName   string
	paramStr string
	// paramNames are the declared Go parameter names in order — the
	// pass-through arguments of a wrapper.
	paramNames []string
	// returnType is the logical Go return type ("" for none).
	returnType string
	// imports are the import edges the method's signature and body need. A
	// wrapper that restates the signature in another file (a factory
	// constructor in <pkg>_classes.go) must merge them into that file's
	// import set.
	imports typemap.ImportSet
}

// splitReason converts a typemap degradation Reason ("key: detail") into a
// skip.
func splitReason(reason string) skip {
	key, detail, found := strings.Cut(reason, ": ")
	if !found {
		return skip{key: reason, detail: reason}
	}
	return skip{key: key, detail: detail}
}

// buildInterfaceModels converts a namespace's interfaces into vtable
// dispatch structs. Every non-generic interface is emitted — including
// [ExclusiveTo] ones, which runtime classes embed. All WinRT interfaces
// root at IInspectable (slots 0–5); there are no base chains.
func (g *Generator) buildInterfaceModels(meta *winrtmeta.NamespaceMeta, imports typemap.ImportSet) []view.InterfaceModel {
	models := make([]view.InterfaceModel, 0, len(meta.Interfaces))
	for _, name := range sortedKeys(meta.Interfaces) {
		definition := meta.Interfaces[name]
		if definition.Arity > 0 {
			g.diag("generic-type-skipped", "interface %s.%s (arity %d)", meta.Namespace, name, definition.Arity)
			continue
		}
		goName := naming.Export(name)
		if !g.claimTypeName(goName) {
			g.diag("name-collision-skipped", "interface %s.%s", meta.Namespace, name)
			continue
		}
		model := g.buildInterface(meta, meta.Namespace+"."+name, goName, &definition, imports)
		if meta.Namespace == "Windows.Foundation" && name == "IAsyncAction" {
			g.attachAwait(meta, &model, imports)
		}
		models = append(models, model)
	}
	return models
}

// buildInterface lowers one interface definition (declared or a grounded
// generic instantiation) into its render model. fullName is the display
// name for doc comments and diagnostics — "Windows.Globalization.ICalendar"
// for declared interfaces, the instantiation display form
// ("Windows.Foundation.Collections.IVectorView`1<String>") for pinterfaces.
func (g *Generator) buildInterface(meta *winrtmeta.NamespaceMeta, fullName, goName string, definition *winrtmeta.Interface, imports typemap.ImportSet) view.InterfaceModel {
	model := view.InterfaceModel{
		TypeName:    goName,
		FullName:    fullName,
		GUID:        definition.GUID,
		ExclusiveTo: definition.ExclusiveTo,
	}
	for i := range definition.Requires {
		model.Requires = append(model.Requires, refDisplay(&definition.Requires[i]))
	}

	if definition.GUID != "" {
		literal, err := guidLiteral(definition.GUID)
		if err != nil {
			g.diag("malformed-guid", "interface %s: %v", fullName, err)
		} else if iidVar := "IID_" + goName; g.claimName(iidVar) {
			model.IIDVar = iidVar
			model.IIDLiteral = literal
		} else {
			g.diag("name-collision-skipped", "IID var for %s", fullName)
		}
	} else {
		g.diag("interface-missing-guid", "%s", fullName)
	}

	// Events reference their add_/remove_ accessors by MethodDef name; the
	// accessors themselves sit in Methods at their vtable slots.
	addEvents := map[string]*winrtmeta.Event{}
	removeEvents := map[string]*winrtmeta.Event{}
	for i := range definition.Events {
		event := &definition.Events[i]
		if event.AddMethod != "" {
			addEvents[event.AddMethod] = event
		}
		if event.RemoveMethod != "" {
			removeEvents[event.RemoveMethod] = event
		}
	}

	// Vtable methods in MethodDef order: slot = 6 + index. Skipped members
	// NEVER renumber — they leave an audit comment at their slot instead.
	methodNames := map[string]bool{}
	records := make([]emittedMethod, len(definition.Methods))
	for i := range definition.Methods {
		method := &definition.Methods[i]
		slot := 6 + i
		memberPath := model.FullName + "." + method.Name
		var methodModel view.MethodModel
		var skipped *skip
		var methodImports typemap.ImportSet
		plain := false
		switch {
		case strings.HasPrefix(method.Name, "add_"):
			methodModel, skipped = g.buildAddAccessor(meta, goName, method, slot, addEvents[method.Name])
		case strings.HasPrefix(method.Name, "remove_"):
			methodModel, skipped = g.buildRemoveAccessor(meta, goName, method, slot, removeEvents[method.Name])
		default:
			// Per-method import set: merged into the file's set here, and
			// recorded on the method so signature-restating wrappers (factory
			// constructors) can merge it into THEIR file's set too.
			methodImports = typemap.ImportSet{}
			methodModel, skipped = g.buildMethod(meta, goName, method, slot, methodImports)
			imports.Merge(methodImports)
			plain = true
		}
		if skipped != nil {
			g.diag(skipped.key, "%s (%s)", memberPath, skipped.detail)
			model.Methods = append(model.Methods, view.MethodModel{
				SkipComment: fmt.Sprintf("slot %d: %s skipped: %s", slot, method.Name, skipped.detail),
			})
			continue
		}
		for methodNames[methodModel.GoName] {
			methodModel.GoName += "_"
		}
		methodNames[methodModel.GoName] = true
		model.Methods = append(model.Methods, methodModel)
		if plain {
			records[i] = emittedMethod{
				emitted:    true,
				goName:     methodModel.GoName,
				paramStr:   methodModel.ParamStr,
				paramNames: goParamNames(method),
				returnType: logicalReturnType(methodModel.ReturnSig),
				imports:    methodImports,
			}
		}
	}
	// Recorded under the display full name: exact metadata names for
	// declared interfaces (what the factory gather looks up); instantiation
	// display forms never collide with them.
	g.ifaceMethods[fullName] = records
	return model
}

// goParamNames lists a method's Go parameter names in declaration order —
// exactly the names buildMethod declares, so wrappers can pass them through.
func goParamNames(method *winrtmeta.Method) []string {
	names := make([]string, len(method.Params))
	for i := range method.Params {
		names[i] = naming.ParamName(method.Params[i].Name)
	}
	return names
}

// logicalReturnType extracts the logical Go return type from a ReturnSig
// buildMethod produced ("(*ICalendar, error)" → "*ICalendar"; "error" → "").
func logicalReturnType(returnSig string) string {
	if returnSig == "error" {
		return ""
	}
	return strings.TrimSuffix(strings.TrimPrefix(returnSig, "("), ", error)")
}

// buildMethod lowers one logical method to its ABI dispatch shape:
// SyscallN(LpVtbl[slot], self, lowered params..., &retval) →
// win32.ErrIfFailed. A nil skip means the method is emitted.
func (g *Generator) buildMethod(meta *winrtmeta.NamespaceMeta, interfaceGoName string, method *winrtmeta.Method, slot int, imports typemap.ImportSet) (view.MethodModel, *skip) {
	metadataName := method.Name
	var goName, accessorNote string
	switch {
	case strings.HasPrefix(metadataName, "get_"):
		goName = naming.Export(strings.TrimPrefix(metadataName, "get_"))
		accessorNote = fmt.Sprintf(" (propget %s)", metadataName)
	case strings.HasPrefix(metadataName, "put_"):
		goName = "Set" + naming.Export(strings.TrimPrefix(metadataName, "put_"))
		accessorNote = fmt.Sprintf(" (propput %s)", metadataName)
	case method.Overload != "":
		goName = naming.Export(method.Overload)
	default:
		goName = naming.Export(metadataName)
	}

	context := g.resolveContext(meta.Namespace)
	// Parameters may ground delegate references into package-local handler
	// types; returns resolve WITHOUT the seam, so methods returning a
	// delegate (get_Completed) keep degrading — returning a native delegate
	// to Go is meaningless this wave.
	paramContext := context
	paramContext.RequestDelegate = g.delegateRequester(meta)
	scratch := typemap.ImportSet{}
	var noteLines []string

	model := view.MethodModel{
		GoName: goName,
		Slot:   slot,
	}

	// Return shape first: the parameter preambles need the zero-value error
	// return.
	errReturn := "return err"
	if method.Return != nil {
		resolved := g.mapper.GoType(method.Return, context, scratch)
		switch resolved.Kind {
		case typemap.KindUnsupported:
			s := splitReason(resolved.Reason)
			return view.MethodModel{}, &s
		case typemap.KindFloat:
			return view.MethodModel{}, &skip{key: "float-abi-skipped", detail: fmt.Sprintf("%s return cannot cross SyscallN", resolved.GoType)}
		case typemap.KindString:
			model.ReturnKind = view.RetString
			model.ReturnSig = "(string, error)"
			model.ResultDecl = "var result syswinrt.HSTRING"
			model.ResultExpr = "winrt.TakeHString(result)"
			model.ZeroReturn = `""`
		case typemap.KindBool:
			model.ReturnKind = view.RetValue
			model.ReturnSig = "(bool, error)"
			model.ResultDecl = "var result byte"
			model.ResultExpr = "result != 0"
			model.ZeroReturn = "false"
		case typemap.KindScalar, typemap.KindEnum:
			model.ReturnKind = view.RetValue
			model.ReturnSig = "(" + resolved.GoType + ", error)"
			model.ResultDecl = "var result " + resolved.GoType
			model.ResultExpr = "result"
			model.ZeroReturn = "0"
		case typemap.KindGUID, typemap.KindStruct:
			model.ReturnKind = view.RetValue
			model.ReturnSig = "(" + resolved.GoType + ", error)"
			model.ResultDecl = "var result " + resolved.GoType
			model.ResultExpr = "result"
			model.ZeroReturn = resolved.GoType + "{}"
		case typemap.KindInterfacePtr, typemap.KindObjectPtr:
			model.ReturnKind = view.RetValue
			model.ReturnSig = "(" + resolved.GoType + ", error)"
			model.ResultDecl = "var result " + resolved.GoType
			model.ResultExpr = "result"
			model.ZeroReturn = "nil"
		default:
			return view.MethodModel{}, &skip{key: "unsupported-return", detail: resolved.GoType}
		}
		if resolved.Note != "" {
			noteLines = append(noteLines, "The return value's "+resolved.Note+".")
		}
		errReturn = "return " + model.ZeroReturn + ", err"
	} else {
		model.ReturnKind = view.RetError
		model.ReturnSig = "error"
	}

	// Parameters, in metadata order.
	paramNames := make(map[string]bool, len(method.Params))
	for i := range method.Params {
		paramNames[naming.ParamName(method.Params[i].Name)] = true
	}
	var decls []string
	for i := range method.Params {
		param := &method.Params[i]
		paramName := naming.ParamName(param.Name)
		resolved := g.mapper.GoType(&param.Type, paramContext, scratch)
		if resolved.Kind == typemap.KindUnsupported {
			s := splitReason(resolved.Reason)
			return view.MethodModel{}, &s
		}
		if resolved.Kind == typemap.KindFloat {
			return view.MethodModel{}, &skip{key: "float-abi-skipped", detail: fmt.Sprintf("%s parameter %s cannot cross SyscallN", resolved.GoType, param.Name)}
		}
		if resolved.Kind == typemap.KindDelegatePtr {
			noteLines = append(noteLines, fmt.Sprintf("A nil %s passes NULL at the ABI (WinRT accepts it where a handler may be cleared).", paramName))
		}
		if resolved.Note != "" {
			noteLines = append(noteLines, "Parameter "+paramName+"'s "+resolved.Note+".")
		}
		if param.Out {
			decl, arg, skipped := lowerOutParam(paramName, resolved)
			if skipped != nil {
				return view.MethodModel{}, skipped
			}
			decls = append(decls, decl)
			model.ArgExprs = append(model.ArgExprs, arg)
			continue
		}
		decl, preamble, arg, skipped := g.lowerInParam(paramName, param, resolved, paramNames, errReturn)
		if skipped != nil {
			return view.MethodModel{}, skipped
		}
		decls = append(decls, decl)
		model.Preamble = append(model.Preamble, preamble...)
		model.ArgExprs = append(model.ArgExprs, arg)
	}
	if method.Return != nil {
		model.ArgExprs = append(model.ArgExprs, "uintptr(unsafe.Pointer(&result))")
	}
	model.ParamStr = strings.Join(decls, ", ")

	model.CommentLines = append(model.CommentLines,
		fmt.Sprintf("%s%s dispatches through %s's vtable slot %d.", goName, accessorNote, interfaceGoName, slot))
	model.CommentLines = append(model.CommentLines, noteLines...)

	imports.Merge(scratch)
	return model, nil
}

// lowerOutParam lowers a non-retval [out] parameter to a Go pointer
// parameter passed straight through — only shapes whose Go representation
// is ABI-identical qualify.
func lowerOutParam(paramName string, resolved typemap.Resolved) (decl, arg string, skipped *skip) {
	switch resolved.Kind {
	case typemap.KindScalar, typemap.KindEnum, typemap.KindStruct, typemap.KindGUID,
		typemap.KindInterfacePtr, typemap.KindObjectPtr:
		return paramName + " *" + resolved.GoType, "uintptr(unsafe.Pointer(" + paramName + "))", nil
	case typemap.KindString, typemap.KindBool:
		return "", "", &skip{key: "out-param-skipped", detail: fmt.Sprintf("%s out parameter %s is not lowered this wave", resolved.GoType, paramName)}
	}
	return "", "", &skip{key: "out-param-skipped", detail: fmt.Sprintf("out parameter %s not representable", paramName)}
}

// lowerInParam lowers one input parameter to its SyscallN argument word,
// with any conversion preamble (HSTRING inputs, bool → 0/1 words).
func (g *Generator) lowerInParam(paramName string, param *winrtmeta.Param, resolved typemap.Resolved, taken map[string]bool, errReturn string) (decl string, preamble []string, arg string, skipped *skip) {
	switch resolved.Kind {
	case typemap.KindScalar, typemap.KindEnum:
		return paramName + " " + resolved.GoType, nil, "uintptr(" + paramName + ")", nil
	case typemap.KindBool:
		local := freshLocal("_"+paramName, taken)
		preamble = []string{
			local + " := uintptr(0)",
			"if " + paramName + " {",
			local + " = 1",
			"}",
		}
		return paramName + " bool", preamble, local, nil
	case typemap.KindString:
		local := freshLocal("h"+naming.Export(paramName), taken)
		preamble = []string{
			local + ", err := winrt.NewHString(" + paramName + ")",
			"if err != nil {",
			errReturn,
			"}",
			"defer " + local + ".Close()",
		}
		return paramName + " string", preamble, "uintptr(" + local + ".Raw())", nil
	case typemap.KindStruct:
		// By-value structs cross SyscallN only when they flatten to a
		// single integer word (DateTime, TimeSpan).
		field, ok := g.mapper.SingleIntegerField(resolved.StructNamespace, resolved.StructName)
		if !ok {
			return "", nil, "", &skip{key: "byval-struct-param-skipped",
				detail: fmt.Sprintf("by-value %s.%s parameter %s does not flatten to one integer word", resolved.StructNamespace, resolved.StructName, paramName)}
		}
		return paramName + " " + resolved.GoType, nil, "uintptr(" + paramName + "." + field + ")", nil
	case typemap.KindGUID:
		// 16 bytes by value: two registers on arm64, hidden reference on
		// amd64 — no single SyscallN lowering covers both.
		return "", nil, "", &skip{key: "byval-struct-param-skipped",
			detail: fmt.Sprintf("by-value GUID parameter %s has divergent amd64/arm64 ABIs", paramName)}
	case typemap.KindInterfacePtr, typemap.KindObjectPtr:
		return paramName + " " + resolved.GoType, nil, "uintptr(unsafe.Pointer(" + paramName + "))", nil
	case typemap.KindDelegatePtr:
		// A Go-implemented handler crosses as its COM object pointer; nil
		// passes NULL (WinRT accepts it where a handler may be cleared).
		local := freshLocal("_"+paramName, taken)
		preamble = []string{
			local + " := uintptr(0)",
			"if " + paramName + " != nil {",
			local + " = " + paramName + ".Ptr()",
			"}",
		}
		return paramName + " " + resolved.GoType, preamble, local, nil
	}
	return "", nil, "", &skip{key: "unsupported-param", detail: fmt.Sprintf("parameter %s (%s)", param.Name, resolved.GoType)}
}

// freshLocal returns a local identifier not colliding with any parameter.
func freshLocal(candidate string, taken map[string]bool) string {
	for taken[candidate] {
		candidate += "_"
	}
	return candidate
}

// refDisplay renders a TypeRef for doc comments (full metadata names,
// generic arguments in angle brackets, primitives under their projected
// WinRT names: IVectorView`1<String>).
func refDisplay(ref *winrtmeta.TypeRef) string {
	switch ref.Kind {
	case "Native":
		if projected, ok := nativeMangles[ref.Name]; ok {
			return projected
		}
		return ref.Name
	case "GenericParamRef":
		return fmt.Sprintf("T%d", ref.Index)
	case "Array":
		if ref.Elem != nil {
			return refDisplay(ref.Elem) + "[]"
		}
		return "[]"
	}
	name := ref.Name
	if ref.Namespace != "" {
		name = ref.Namespace + "." + name
	}
	if len(ref.Args) > 0 {
		args := make([]string, len(ref.Args))
		for i := range ref.Args {
			args[i] = refDisplay(&ref.Args[i])
		}
		name += "<" + strings.Join(args, ", ") + ">"
	}
	return name
}
