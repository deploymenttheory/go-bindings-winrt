package emitwinrt

import (
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/deploymenttheory/go-bindings-winrt/internal/codegen/emit/winrt/view"
	"github.com/deploymenttheory/go-bindings-winrt/internal/codegen/naming"
	"github.com/deploymenttheory/go-bindings-winrt/internal/codegen/typemap"
	"github.com/deploymenttheory/go-bindings-winrt/internal/winrtmeta"
)

// buildClassModels converts a namespace's non-composable runtime classes
// into render models: a struct embedding the default interface by value,
// a NewFoo constructor when the class is directly activatable, an
// As<Interface> query method per other non-generic instance interface,
// package-level statics accessors for the [Static] interfaces, and
// package-level factory constructors for the [Activatable] factory
// interfaces. Statics accessors are independent of the class type: a
// statics-only class emits them with no class type at all.
func (g *Generator) buildClassModels(meta *winrtmeta.NamespaceMeta, imports typemap.ImportSet) []view.ClassModel {
	models := make([]view.ClassModel, 0, len(meta.Classes))
	for _, name := range sortedKeys(meta.Classes) {
		class := meta.Classes[name]
		fullName := meta.Namespace + "." + name
		if class.Composable {
			g.diag("composable-class-skipped", "%s", fullName)
			continue
		}
		model, typeEmitted := g.buildClassType(meta, name, fullName, &class, imports)
		model.Statics = g.buildStaticsAccessors(meta, fullName, &class, imports)
		if typeEmitted {
			model.Factories = g.buildFactoryFuncs(meta, fullName, &class, &model, imports)
		} else {
			for _, factory := range class.ActivatableFactories {
				g.diag("factory-skipped", "%s factory %s (class type not emitted)", fullName, factory)
			}
		}
		if typeEmitted || len(model.Statics) > 0 {
			models = append(models, model)
		}
	}
	return models
}

// buildClassType builds the class-type part of the model (struct, direct
// constructor, query methods). false means the class type is not emitted —
// silently for statics-only classes (their accessors still render), with a
// diagnostic otherwise.
func (g *Generator) buildClassType(meta *winrtmeta.NamespaceMeta, name, fullName string, class *winrtmeta.Class, imports typemap.ImportSet) (view.ClassModel, bool) {
	if class.DefaultInterface == nil {
		// A statics-only class has nothing to instantiate: no type, no
		// diagnostic — the statics accessors are its whole projection.
		if !(len(class.StaticInterfaces) > 0 && len(class.Interfaces) == 0) {
			g.diag("class-default-missing-skipped", "%s", fullName)
		}
		return view.ClassModel{FullName: fullName}, false
	}
	if class.DefaultInterface.Kind != "ApiRef" {
		g.diag("class-default-generic-skipped", "%s (default %s)", fullName, refDisplay(class.DefaultInterface))
		return view.ClassModel{FullName: fullName}, false
	}

	context := g.resolveContext(meta.Namespace)
	scratch := typemap.ImportSet{}
	resolvedDefault := g.mapper.GoType(class.DefaultInterface, context, scratch)
	if resolvedDefault.Kind != typemap.KindInterfacePtr {
		g.diag("class-default-generic-skipped", "%s (default %s not emittable)", fullName, refDisplay(class.DefaultInterface))
		return view.ClassModel{FullName: fullName}, false
	}
	defaultIIDRef, ok := g.iidRef(class.DefaultInterface, meta.Namespace)
	if !ok {
		g.diag("class-default-missing-skipped", "%s (default interface has no IID)", fullName)
		return view.ClassModel{FullName: fullName}, false
	}

	goName := naming.Export(name)
	if !g.claimTypeName(goName) {
		g.diag("name-collision-skipped", "class %s", fullName)
		return view.ClassModel{FullName: fullName}, false
	}
	model := view.ClassModel{
		TypeName:         goName,
		FullName:         fullName,
		DefaultInterface: strings.TrimPrefix(resolvedDefault.GoType, "*"),
		DefaultIIDRef:    defaultIIDRef,
	}

	// Direct activation (RoActivateInstance).
	if class.ActivatableDirect {
		if ctor := "New" + goName; g.claimName(ctor) {
			model.CtorName = ctor
		} else {
			g.diag("name-collision-skipped", "constructor New%s for %s", goName, fullName)
		}
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
		resolved := g.mapper.GoType(target, context, scratch)
		if resolved.Kind != typemap.KindInterfacePtr {
			s := splitReason(resolved.Reason)
			if resolved.Kind != typemap.KindUnsupported {
				s = skip{key: "class-interface-skipped", detail: refDisplay(target) + " is not an emittable interface"}
			}
			g.diag(s.key, "%s (%s)", memberPath, s.detail)
			continue
		}
		if target.Kind == "GenericInst" {
			// The instantiation is package-local under its mangled name; the
			// query method follows it (AsVectorViewOfString), not the
			// backtick-arity metadata name.
			asName = naming.InterfaceAsName(strings.TrimPrefix(resolved.GoType, "*"))
			memberPath = fullName + "." + asName
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

// buildStaticsAccessors projects a class's [Static] interfaces as
// package-level accessor functions returning the statics interface fetched
// through the class's activation factory. GetActivationFactory queries the
// statics IID itself, so the returned pointer IS the statics interface
// (every generated interface struct is a single vtable-pointer word).
func (g *Generator) buildStaticsAccessors(meta *winrtmeta.NamespaceMeta, fullName string, class *winrtmeta.Class, imports typemap.ImportSet) []view.StaticsAccessorModel {
	if len(class.StaticInterfaces) == 0 {
		return nil
	}
	context := g.resolveContext(meta.Namespace)
	statics := slices.Clone(class.StaticInterfaces)
	sort.Strings(statics)
	var models []view.StaticsAccessorModel
	for _, staticFullName := range statics {
		ref, ok := interfaceRef(staticFullName)
		if !ok {
			g.diag("statics-skipped", "%s (%s is not a namespace-qualified name)", fullName, staticFullName)
			continue
		}
		scratch := typemap.ImportSet{}
		resolved := g.mapper.GoType(&ref, context, scratch)
		if resolved.Kind != typemap.KindInterfacePtr {
			g.diag("statics-skipped", "%s (%s: %s)", fullName, staticFullName, splitReason(resolved.Reason).detail)
			continue
		}
		iidRef, ok := g.iidRef(&ref, meta.Namespace)
		if !ok {
			g.diag("statics-skipped", "%s (%s has no IID)", fullName, staticFullName)
			continue
		}
		funcName := naming.StaticsAccessorName(ref.Name)
		if !g.claimName(funcName) {
			g.diag("name-collision-skipped", "statics accessor %s for %s", funcName, fullName)
			continue
		}
		models = append(models, view.StaticsAccessorModel{
			FuncName:          funcName,
			InterfaceType:     strings.TrimPrefix(resolved.GoType, "*"),
			InterfaceFullName: staticFullName,
			IIDRef:            iidRef,
			ClassFullName:     fullName,
		})
		imports.Merge(scratch)
	}
	return models
}

// buildFactoryFuncs projects a class's [Activatable] factory interfaces as
// package-level constructor functions: each emitted factory method becomes a
// func that fetches the factory, delegates to the generated interface method
// (so the parameter lowering is exactly the method's), and wraps the
// returned default-interface pointer as the class type. The factory is
// fetched per call — a factory cache is a future optimization. Adopting a
// method's signature adopts its import edges: the recorded per-method
// imports merge into the classes file's set.
func (g *Generator) buildFactoryFuncs(meta *winrtmeta.NamespaceMeta, fullName string, class *winrtmeta.Class, model *view.ClassModel, imports typemap.ImportSet) []view.FactoryFuncModel {
	var models []view.FactoryFuncModel
	for ordinal, factoryFullName := range class.ActivatableFactories {
		ref, ok := interfaceRef(factoryFullName)
		if !ok {
			g.diag("factory-skipped", "%s factory %s (not a namespace-qualified name)", fullName, factoryFullName)
			continue
		}
		definition := g.mapper.Registry.Interface(ref.Namespace, ref.Name)
		if definition == nil {
			g.diag("factory-skipped", "%s factory %s (unresolved)", fullName, factoryFullName)
			continue
		}
		if definition.Arity > 0 {
			g.diag("factory-generic-skipped", "%s factory %s", fullName, factoryFullName)
			continue
		}
		if ref.Namespace != meta.Namespace {
			// Factory interfaces are [ExclusiveTo] their class in practice; a
			// cross-namespace factory has no method surface recorded here.
			g.diag("factory-skipped", "%s factory %s (outside the class namespace)", fullName, factoryFullName)
			continue
		}
		iidRef, ok := g.iidRef(&ref, meta.Namespace)
		if !ok {
			g.diag("factory-skipped", "%s factory %s (no IID)", fullName, factoryFullName)
			continue
		}
		records := g.ifaceMethods[factoryFullName]
		for i := range definition.Methods {
			method := &definition.Methods[i]
			memberPath := fullName + " factory " + factoryFullName
			var record emittedMethod
			if i < len(records) {
				record = records[i]
			}
			if !record.emitted {
				g.diag("factory-skipped", "%s (method %s not emitted on the factory interface)", memberPath, method.Name)
				continue
			}
			// The wrapper's unsafe re-type is only sound when the factory
			// method hands back the class's default interface.
			if record.returnType != "*"+model.DefaultInterface {
				g.diag("factory-skipped", "%s (method %s does not return the class default interface)", memberPath, method.Name)
				continue
			}
			projected := method.Name
			if method.Overload != "" {
				projected = method.Overload
			}
			funcName := naming.Export(projected)
			if !g.claimName(funcName) {
				// Deterministic fallbacks. Bare factory names like Create
				// recur across classes in dense packages, so a loser first
				// gains its class name (Create → CreateSystemTrigger) —
				// unless it already carries it (IWidgetFactory2.CreateWidget
				// on Widget), where within-class disambiguation is what's
				// needed — then the factory's 1-based [Activatable] ordinal.
				candidate := funcName
				if !strings.HasSuffix(candidate, model.TypeName) {
					candidate += model.TypeName
				}
				switch suffixed := candidate + strconv.Itoa(ordinal+1); {
				case candidate != funcName && g.claimName(candidate):
					funcName = candidate
				case g.claimName(suffixed):
					funcName = suffixed
				default:
					g.diag("name-collision-skipped", "factory constructor %s for %s", funcName, fullName)
					continue
				}
			}
			imports.Merge(record.imports)
			models = append(models, view.FactoryFuncModel{
				FuncName:        funcName,
				FactoryType:     naming.Export(ref.Name),
				FactoryFullName: factoryFullName,
				FactoryIIDRef:   iidRef,
				MethodName:      record.goName,
				ParamStr:        record.paramStr,
				ArgNames:        record.paramNames,
			})
		}
	}
	return models
}

// interfaceRef builds the ApiRef for a full interface metadata name
// ("Windows.Globalization.ILanguageStatics"); false when the name has no
// namespace segment.
func interfaceRef(fullName string) (winrtmeta.TypeRef, bool) {
	dot := strings.LastIndex(fullName, ".")
	if dot < 0 {
		return winrtmeta.TypeRef{}, false
	}
	return winrtmeta.TypeRef{Kind: "ApiRef", Namespace: fullName[:dot], Name: fullName[dot+1:], TargetKind: "Interface"}, true
}

// iidRef builds the address expression of an interface's IID variable
// ("&IID_ICalendar", "&foundation.IID_IStringable"); false when the
// interface carries no GUID. Generic instantiations resolve to the derived
// IID var emitted alongside the instantiation in the consuming package.
func (g *Generator) iidRef(ref *winrtmeta.TypeRef, fromNamespace string) (string, bool) {
	if ref.Kind == "GenericInst" {
		mangled, err := instantiationName(ref)
		if err != nil {
			return "", false
		}
		return "&IID_" + mangled, true
	}
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
