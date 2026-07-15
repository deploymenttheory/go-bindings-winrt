package emitwinrt

import (
	"sort"
	"strings"

	"github.com/deploymenttheory/go-bindings-winrt/internal/codegen/emit/winrt/view"
	"github.com/deploymenttheory/go-bindings-winrt/internal/codegen/naming"
	"github.com/deploymenttheory/go-bindings-winrt/internal/codegen/typemap"
	"github.com/deploymenttheory/go-bindings-winrt/internal/winrtmeta"
)

// buildClassModels converts a namespace's non-composable runtime classes
// into render models: a struct embedding the default interface by value,
// a NewFoo constructor when the class is directly activatable, and an
// As<Interface> query method per other non-generic instance interface.
// Factory and static projections are deliberately out of scope this wave.
func (g *Generator) buildClassModels(meta *winrtmeta.NamespaceMeta, imports typemap.ImportSet) []view.ClassModel {
	models := make([]view.ClassModel, 0, len(meta.Classes))
	for _, name := range sortedKeys(meta.Classes) {
		class := meta.Classes[name]
		fullName := meta.Namespace + "." + name
		if class.Composable {
			g.diag("composable-class-skipped", "%s", fullName)
			continue
		}
		if class.DefaultInterface == nil {
			if len(class.StaticInterfaces) > 0 && len(class.Interfaces) == 0 {
				g.diag("statics-only-class-skipped", "%s", fullName)
			} else {
				g.diag("class-default-missing-skipped", "%s", fullName)
			}
			continue
		}
		if class.DefaultInterface.Kind != "ApiRef" {
			g.diag("class-default-generic-skipped", "%s (default %s)", fullName, refDisplay(class.DefaultInterface))
			continue
		}
		model, ok := g.buildClass(meta, name, fullName, &class, imports)
		if !ok {
			continue
		}
		models = append(models, model)
	}
	return models
}

func (g *Generator) buildClass(meta *winrtmeta.NamespaceMeta, name, fullName string, class *winrtmeta.Class, imports typemap.ImportSet) (view.ClassModel, bool) {
	context := typemap.Context{Namespace: meta.Namespace}
	scratch := typemap.ImportSet{}

	resolvedDefault := g.mapper.GoType(class.DefaultInterface, context, scratch)
	if resolvedDefault.Kind != typemap.KindInterfacePtr {
		g.diag("class-default-generic-skipped", "%s (default %s not emittable)", fullName, refDisplay(class.DefaultInterface))
		return view.ClassModel{}, false
	}
	defaultIIDRef, ok := g.iidRef(class.DefaultInterface, meta.Namespace)
	if !ok {
		g.diag("class-default-missing-skipped", "%s (default interface has no IID)", fullName)
		return view.ClassModel{}, false
	}

	goName := naming.Export(name)
	if !g.claimTypeName(goName) {
		g.diag("name-collision-skipped", "class %s", fullName)
		return view.ClassModel{}, false
	}
	model := view.ClassModel{
		TypeName:         goName,
		FullName:         fullName,
		DefaultInterface: strings.TrimPrefix(resolvedDefault.GoType, "*"),
		DefaultIIDRef:    defaultIIDRef,
	}

	// Direct activation (RoActivateInstance) only; factory constructors are
	// next wave's work whether or not the factory is generic.
	if class.ActivatableDirect {
		if ctor := "New" + goName; g.claimName(ctor) {
			model.CtorName = ctor
		} else {
			g.diag("name-collision-skipped", "constructor New%s for %s", goName, fullName)
		}
	}
	for _, factory := range class.ActivatableFactories {
		if g.factoryIsGeneric(factory) {
			g.diag("factory-generic-skipped", "%s factory %s", fullName, factory)
		} else {
			g.diag("factory-skipped", "%s factory %s", fullName, factory)
		}
	}
	if len(class.StaticInterfaces) > 0 {
		statics := append([]string(nil), class.StaticInterfaces...)
		sort.Strings(statics)
		g.diag("statics-skipped", "%s (%s)", fullName, strings.Join(statics, ", "))
	}

	// Query methods for the other instance interfaces, in InterfaceImpl
	// (metadata) order.
	methodNames := map[string]bool{}
	for i := range class.Interfaces {
		target := &class.Interfaces[i]
		if target.Namespace == class.DefaultInterface.Namespace && target.Name == class.DefaultInterface.Name {
			continue // the default is embedded, not queried
		}
		asName := naming.InterfaceAsName(target.Name)
		memberPath := fullName + "." + asName
		if target.Kind != "ApiRef" {
			g.diag("generic-member-skipped", "%s (instance interface %s)", memberPath, refDisplay(target))
			continue
		}
		resolved := g.mapper.GoType(target, context, scratch)
		if resolved.Kind != typemap.KindInterfacePtr {
			s := splitReason(resolved.Reason)
			if resolved.Kind != typemap.KindUnsupported {
				s = skip{key: "class-interface-skipped", detail: refDisplay(target) + " is not an emittable interface"}
			}
			g.diag(s.key, "%s (%s)", memberPath, s.detail)
			continue
		}
		iidRef, ok := g.iidRef(target, meta.Namespace)
		if !ok {
			g.diag("class-interface-skipped", "%s (%s has no IID)", memberPath, refDisplay(target))
			continue
		}
		if methodNames[asName] {
			g.diag("name-collision-skipped", "%s", memberPath)
			continue
		}
		methodNames[asName] = true
		model.QueryMethods = append(model.QueryMethods, view.QueryMethodModel{
			GoName:        asName,
			InterfaceType: strings.TrimPrefix(resolved.GoType, "*"),
			IIDRef:        iidRef,
		})
	}

	imports.Merge(scratch)
	return model, true
}

// iidRef builds the address expression of an interface's IID variable
// ("&IID_ICalendar", "&foundation.IID_IStringable"); false when the
// interface carries no GUID.
func (g *Generator) iidRef(ref *winrtmeta.TypeRef, fromNamespace string) (string, bool) {
	definition := g.mapper.Registry.Interface(ref.Namespace, ref.Name)
	if definition == nil || definition.GUID == "" {
		return "", false
	}
	iidVar := "IID_" + naming.Export(ref.Name)
	if ref.Namespace == fromNamespace {
		return "&" + iidVar, true
	}
	return "&" + naming.ImportAlias(ref.Namespace) + "." + iidVar, true
}

// factoryIsGeneric reports whether a factory interface (full metadata name)
// is generic or has any method touching generic types.
func (g *Generator) factoryIsGeneric(fullName string) bool {
	dot := strings.LastIndex(fullName, ".")
	if dot < 0 {
		return false
	}
	definition := g.mapper.Registry.Interface(fullName[:dot], fullName[dot+1:])
	if definition == nil {
		return false
	}
	if definition.Arity > 0 {
		return true
	}
	for i := range definition.Methods {
		if methodTouchesGenerics(&definition.Methods[i]) {
			return true
		}
	}
	return false
}

// methodTouchesGenerics reports whether any type in the method's signature
// involves a generic instantiation or parameter.
func methodTouchesGenerics(method *winrtmeta.Method) bool {
	var touches func(ref *winrtmeta.TypeRef) bool
	touches = func(ref *winrtmeta.TypeRef) bool {
		if ref == nil {
			return false
		}
		if ref.Kind == "GenericInst" || ref.Kind == "GenericParamRef" {
			return true
		}
		for i := range ref.Args {
			if touches(&ref.Args[i]) {
				return true
			}
		}
		return touches(ref.Elem)
	}
	for i := range method.Params {
		if touches(&method.Params[i].Type) {
			return true
		}
	}
	return touches(method.Return)
}
