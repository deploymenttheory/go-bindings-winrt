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
		models = append(models, g.buildInterface(meta, name, goName, &definition, imports))
	}
	return models
}

func (g *Generator) buildInterface(meta *winrtmeta.NamespaceMeta, name, goName string, definition *winrtmeta.Interface, imports typemap.ImportSet) view.InterfaceModel {
	model := view.InterfaceModel{
		TypeName:    goName,
		FullName:    meta.Namespace + "." + name,
		GUID:        definition.GUID,
		ExclusiveTo: definition.ExclusiveTo,
	}
	for i := range definition.Requires {
		model.Requires = append(model.Requires, refDisplay(&definition.Requires[i]))
	}

	if definition.GUID != "" {
		literal, err := guidLiteral(definition.GUID)
		if err != nil {
			g.diag("malformed-guid", "interface %s.%s: %v", meta.Namespace, name, err)
		} else if iidVar := "IID_" + goName; g.claimName(iidVar) {
			model.IIDVar = iidVar
			model.IIDLiteral = literal
		} else {
			g.diag("name-collision-skipped", "IID var for %s.%s", meta.Namespace, name)
		}
	} else {
		g.diag("interface-missing-guid", "%s.%s", meta.Namespace, name)
	}

	// Vtable methods in MethodDef order: slot = 6 + index. Skipped members
	// NEVER renumber — they leave an audit comment at their slot instead.
	methodNames := map[string]bool{}
	for i := range definition.Methods {
		method := &definition.Methods[i]
		slot := 6 + i
		memberPath := model.FullName + "." + method.Name
		methodModel, skipped := g.buildMethod(meta, goName, method, slot, imports)
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
	}
	return model
}

// buildMethod lowers one logical method to its ABI dispatch shape:
// SyscallN(LpVtbl[slot], self, lowered params..., &retval) →
// win32.ErrIfFailed. A nil skip means the method is emitted.
func (g *Generator) buildMethod(meta *winrtmeta.NamespaceMeta, interfaceGoName string, method *winrtmeta.Method, slot int, imports typemap.ImportSet) (view.MethodModel, *skip) {
	metadataName := method.Name
	var goName, accessorNote string
	switch {
	case strings.HasPrefix(metadataName, "add_"), strings.HasPrefix(metadataName, "remove_"):
		return view.MethodModel{}, &skip{key: "event-skipped", detail: "events are not emitted this wave"}
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

	context := typemap.Context{Namespace: meta.Namespace}
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
		resolved := g.mapper.GoType(&param.Type, context, scratch)
		if resolved.Kind == typemap.KindUnsupported {
			s := splitReason(resolved.Reason)
			return view.MethodModel{}, &s
		}
		if resolved.Kind == typemap.KindFloat {
			return view.MethodModel{}, &skip{key: "float-abi-skipped", detail: fmt.Sprintf("%s parameter %s cannot cross SyscallN", resolved.GoType, param.Name)}
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
// generic arguments in angle brackets).
func refDisplay(ref *winrtmeta.TypeRef) string {
	switch ref.Kind {
	case "Native":
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
