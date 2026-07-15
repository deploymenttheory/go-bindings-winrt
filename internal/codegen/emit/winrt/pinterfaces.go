package emitwinrt

// Generic-instantiation emission (pinterfaces). A closed generic INTERFACE
// instantiation referenced by an otherwise-emittable member (e.g.
// ICalendar.get_Languages returning IVectorView`1<String>) no longer
// degrades the member: the gather layer monomorphizes the open interface's
// IR under the instantiation's arguments and emits a concrete Go interface
// type — IID derived by the pinterface engine — into the CONSUMING package
// (<pkg>_pinterfaces.go), deduped per package by mangled name.
//
// Instantiations are deliberately NOT emitted into the open type's home
// namespace: member arguments flow from arbitrary consumer namespaces into
// the collections namespaces, so that direction manufactures import cycles.
// The cost is duplication — two packages using the same instantiation each
// get their own copy: distinct Go types, identical ABI (same vtable slots,
// same derived IID), interoperable at the pointer level via QueryInterface.
//
// Discovery is demand-driven and transitively closed: substituting the
// arguments into an open interface's methods may surface further
// instantiations (IIterable<T>.First → IIterator<T>; IVector<T>.GetView →
// IVectorView<T>), which are queued and emitted into the same package until
// a fixed point. Generic DELEGATE instantiations ground when an event or an
// adaptable method PARAMETER requests them (see events.go); in return
// position they still degrade. Monomorphized IAsyncOperation`1 instances
// additionally gain a synthesized blocking Await (see async.go).

import (
	"fmt"
	"reflect"
	"sort"
	"strings"

	"github.com/deploymenttheory/go-bindings-winrt/internal/codegen/emit/winrt/view"
	"github.com/deploymenttheory/go-bindings-winrt/internal/codegen/naming"
	"github.com/deploymenttheory/go-bindings-winrt/internal/codegen/pinterface"
	"github.com/deploymenttheory/go-bindings-winrt/internal/codegen/typemap"
	"github.com/deploymenttheory/go-bindings-winrt/internal/winrtmeta"
)

// nativeMangles maps the IR's Native kinds to their instantiation-name
// atoms (the WinRT projected primitive names).
var nativeMangles = map[string]string{
	"HString": "String",
	"Bool":    "Bool",
	"Char16":  "Char16",
	"U1":      "UInt8",
	"I2":      "Int16",
	"U2":      "UInt16",
	"I4":      "Int32",
	"U4":      "UInt32",
	"I8":      "Int64",
	"U8":      "UInt64",
	"F32":     "Single",
	"F64":     "Double",
	"Guid":    "Guid",
	"Object":  "Object",
}

// instantiationName mangles a closed generic instantiation into its
// package-local Go type name: the open type's name with the backtick arity
// stripped, then "Of" plus each argument's mangled name joined by "And"
// (IVectorView`1<String> → IVectorViewOfString; IMapView`2<String,
// IVectorView`1<String>> → IMapViewOfStringAndIVectorViewOfString).
func instantiationName(ref *winrtmeta.TypeRef) (string, error) {
	if ref.Kind != "GenericInst" {
		return "", fmt.Errorf("%s.%s is not a generic instantiation", ref.Namespace, ref.Name)
	}
	base := ref.Name
	if i := strings.IndexByte(base, '`'); i >= 0 {
		base = base[:i]
	}
	if len(ref.Args) == 0 {
		return "", fmt.Errorf("instantiation %s.%s has no type arguments", ref.Namespace, ref.Name)
	}
	argNames := make([]string, len(ref.Args))
	for i := range ref.Args {
		argName, err := argumentMangle(&ref.Args[i])
		if err != nil {
			return "", err
		}
		argNames[i] = argName
	}
	return naming.Export(base) + "Of" + strings.Join(argNames, "And"), nil
}

// argumentMangle names one instantiation argument.
func argumentMangle(ref *winrtmeta.TypeRef) (string, error) {
	switch ref.Kind {
	case "Native":
		if atom, ok := nativeMangles[ref.Name]; ok {
			return atom, nil
		}
		return "", fmt.Errorf("native kind %q has no instantiation-name form", ref.Name)
	case "ApiRef":
		return naming.Export(ref.Name), nil
	case "GenericInst":
		return instantiationName(ref)
	}
	return "", fmt.Errorf("generic argument kind %q has no instantiation-name form", ref.Kind)
}

// cloneRef deep-copies a TypeRef.
func cloneRef(ref *winrtmeta.TypeRef) winrtmeta.TypeRef {
	out := *ref
	if len(ref.Args) > 0 {
		out.Args = make([]winrtmeta.TypeRef, len(ref.Args))
		for i := range ref.Args {
			out.Args[i] = cloneRef(&ref.Args[i])
		}
	}
	if ref.Elem != nil {
		elem := cloneRef(ref.Elem)
		out.Elem = &elem
	}
	return out
}

// substituteRef deep-copies ref with every GenericParamRef of index i
// replaced by args[i]. Substitution recurses through nested instantiation
// arguments and array elements, so arity-2 and nested-generic shapes ground
// correctly.
func substituteRef(ref *winrtmeta.TypeRef, args []winrtmeta.TypeRef) winrtmeta.TypeRef {
	if ref.Kind == "GenericParamRef" && int(ref.Index) < len(args) {
		return cloneRef(&args[ref.Index])
	}
	out := *ref
	if len(ref.Args) > 0 {
		out.Args = make([]winrtmeta.TypeRef, len(ref.Args))
		for i := range ref.Args {
			out.Args[i] = substituteRef(&ref.Args[i], args)
		}
	}
	if ref.Elem != nil {
		elem := substituteRef(ref.Elem, args)
		out.Elem = &elem
	}
	return out
}

// substituteMethod deep-copies a method with every generic parameter
// replaced under args (nil args is a plain deep copy — any leftover
// GenericParamRef degrades downstream).
func substituteMethod(method *winrtmeta.Method, args []winrtmeta.TypeRef) winrtmeta.Method {
	out := winrtmeta.Method{
		Name:              method.Name,
		Overload:          method.Overload,
		IsDefaultOverload: method.IsDefaultOverload,
	}
	for i := range method.Params {
		param := method.Params[i]
		param.Type = substituteRef(&param.Type, args)
		out.Params = append(out.Params, param)
	}
	if method.Return != nil {
		returnType := substituteRef(method.Return, args)
		out.Return = &returnType
	}
	return out
}

// instantiateInterface grounds an open interface's IR under the given type
// arguments: methods (in MethodDef order, so vtable slots are preserved),
// properties, events, and requires (doc comments) are deep-copied with
// every generic parameter substituted. GUID is left empty — the caller
// assigns the derived pinterface IID.
func instantiateInterface(open *winrtmeta.Interface, args []winrtmeta.TypeRef) *winrtmeta.Interface {
	inst := &winrtmeta.Interface{ExclusiveTo: open.ExclusiveTo}
	for i := range open.Requires {
		inst.Requires = append(inst.Requires, substituteRef(&open.Requires[i], args))
	}
	for i := range open.Methods {
		inst.Methods = append(inst.Methods, substituteMethod(&open.Methods[i], args))
	}
	for i := range open.Properties {
		property := open.Properties[i]
		property.Type = substituteRef(&property.Type, args)
		inst.Properties = append(inst.Properties, property)
	}
	for i := range open.Events {
		event := open.Events[i]
		event.Type = substituteRef(&event.Type, args)
		inst.Events = append(inst.Events, event)
	}
	return inst
}

// requestInstantiation is the typemap callback (Context.RequestInstantiation)
// for the namespace being emitted: it validates that the instantiation can be
// named and grounded (IID derivable), registers it — deduped by mangled name —
// for emission into the current package, and returns the package-local type
// name. ok == false degrades the requesting member.
func (g *Generator) requestInstantiation(ref *winrtmeta.TypeRef) (string, bool) {
	mangled, err := instantiationName(ref)
	if err != nil {
		return "", false
	}
	if existing, seen := g.pinstByName[mangled]; seen {
		// Same mangled name must mean the same instantiation: argument
		// names drop namespaces, so two same-named args from different
		// namespaces would otherwise silently alias distinct IIDs.
		clone := cloneRef(ref)
		if !reflect.DeepEqual(*existing, clone) {
			return "", false
		}
		return mangled, true
	}
	// The name (and its IID var) must be free in the package: a metadata
	// type of the same name wins, and the member degrades.
	if g.claimedNames[mangled] || g.claimedNames["IID_"+mangled] {
		return "", false
	}
	iid, err := pinterface.InstanceIID(ref, g.registry)
	if err != nil {
		return "", false // ungroundable (unresolved arg, missing [Guid], ...)
	}
	g.claimedNames[mangled] = true
	clone := cloneRef(ref)
	g.pinstByName[mangled] = &clone
	g.pinstIID[mangled] = iid
	g.pinstQueue = append(g.pinstQueue, mangled)
	return mangled, true
}

// buildPinterfaceModels drains the instantiation worklist for the namespace
// being emitted. Building an instantiation's methods may request further
// instantiations (the typemap callback appends to the queue), so the loop
// runs to a fixed point; models are then sorted by mangled name for
// deterministic file content.
func (g *Generator) buildPinterfaceModels(meta *winrtmeta.NamespaceMeta, imports typemap.ImportSet) []view.InterfaceModel {
	var models []view.InterfaceModel
	for len(g.pinstQueue) > 0 {
		mangled := g.pinstQueue[0]
		g.pinstQueue = g.pinstQueue[1:]
		inst := g.pinstByName[mangled]
		open := g.registry.Interface(inst.Namespace, inst.Name)
		definition := instantiateInterface(open, inst.Args)
		definition.GUID = g.pinstIID[mangled]
		model := g.buildInterface(meta, refDisplay(inst), mangled, definition, imports)
		if inst.Namespace == "Windows.Foundation" &&
			(inst.Name == "IAsyncOperation`1" || inst.Name == "IAsyncOperationWithProgress`2" ||
				inst.Name == "IAsyncActionWithProgress`1") {
			g.attachAwait(meta, &model, imports)
		}
		models = append(models, model)
	}
	sort.Slice(models, func(i, j int) bool { return models[i].TypeName < models[j].TypeName })
	return models
}
