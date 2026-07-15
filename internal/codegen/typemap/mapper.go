// Package typemap converts IR TypeRefs into Go types under the WinRT
// projection rules. It is the ONLY place type decisions live: the emit
// gather layer consumes Resolved values and never inspects TypeRefs itself.
// Cross-namespace references populate the caller's ImportSet as a side
// effect; anything the ABI lowering cannot represent comes back as
// KindUnsupported with the diagnostic key + detail in Reason, and the gather
// layer degrades the member.
package typemap

import (
	"fmt"
	"maps"

	"github.com/deploymenttheory/go-bindings-winrt/internal/codegen/naming"
	"github.com/deploymenttheory/go-bindings-winrt/internal/codegen/pipeline"
	"github.com/deploymenttheory/go-bindings-winrt/internal/winrtmeta"
)

// Win32ModulePath hosts the shared ABI foundation: the win32 runtime
// (HRESULT/GUID/IUnknown) and the generated system/winrt package (HSTRING,
// IInspectable, Ro* activation).
const Win32ModulePath = "github.com/deploymenttheory/go-bindings-win32"

// Win32RuntimeImport is the hand-written win32 runtime package (alias win32).
const Win32RuntimeImport = Win32ModulePath + "/bindings/runtime/win32"

// SysWinRTImport is the generated WinRT system package (alias syswinrt).
const SysWinRTImport = Win32ModulePath + "/bindings/win32/system/winrt"

// Import is one import edge: the Go import path plus the WinRT namespace it
// carries ("" for runtime/support imports). The generator computes the
// transitive emit closure from the Namespace fields of imports that survive
// pruning — so only namespaces referenced by EMITTED members are chased.
type Import struct {
	Path      string
	Namespace string
}

// ImportSet accumulates alias → Import pairs as resolution progresses.
type ImportSet map[string]Import

// Merge copies every entry of other into the set (used to commit a
// per-member scratch set once the member is known to be emitted).
func (s ImportSet) Merge(other ImportSet) {
	maps.Copy(s, other)
}

// Kind classifies a resolved Go type for ABI lowering decisions.
type Kind uint8

const (
	KindVoid         Kind = iota // no value (returns only)
	KindScalar                   // integer scalar (incl. Char16 → uint16)
	KindFloat                    // float32/float64 — cannot cross SyscallN by value
	KindBool                     // Go bool; one byte at the WinRT ABI
	KindString                   // Go string; syswinrt.HSTRING at the ABI
	KindGUID                     // win32.GUID by value
	KindEnum                     // named enum type (int32/uint32 backed)
	KindStruct                   // value struct
	KindObjectPtr                // *syswinrt.IInspectable
	KindInterfacePtr             // interface pointer (*IFoo)
	KindDelegatePtr              // grounded delegate handler pointer (*FooHandler)
	KindUnsupported              // member must degrade; see Reason
)

// Resolved is the pure-data result of one type resolution.
type Resolved struct {
	// GoType is the rendered Go type ("string", "*ICalendar",
	// "foundation.DateTime"). Empty for KindVoid and KindUnsupported.
	GoType string
	Kind   Kind
	// Reason carries the diagnostic "key: detail" when Kind is
	// KindUnsupported.
	Reason string
	// Note is a non-fatal remark the gather layer surfaces in the generated
	// doc comment (e.g. a class reference projected as IInspectable).
	Note string
	// StructNamespace/StructName identify a KindStruct target so the gather
	// layer can apply the by-value flattening rule.
	StructNamespace, StructName string
}

// Context carries per-resolution state.
type Context struct {
	// Namespace is the full namespace being emitted
	// ("Windows.Globalization"); references into it stay unqualified.
	// Blocked-edge decisions key off this namespace.
	Namespace string

	// RequestInstantiation is the gather layer's demand-driven pinterface
	// seam: when a member references a closed generic INTERFACE
	// instantiation, the callback registers the instantiation for emission
	// into the consuming package and returns its package-local Go type
	// name. A nil callback, or ok == false (the instantiation cannot be
	// grounded or named), degrades the member exactly as before.
	RequestInstantiation func(ref *winrtmeta.TypeRef) (string, bool)

	// RequestDelegate is the gather layer's delegate-grounding seam for
	// method PARAMETERS: when a parameter references a delegate (a closed
	// generic delegate instantiation or a non-generic delegate ApiRef), the
	// callback grounds it into a package-local handler type — under the same
	// adaptability rules as event delegates — and returns the handler's Go
	// type name. It is wired ONLY for parameter resolution: delegate
	// RETURNS (get_Completed) resolve without it and keep degrading. A nil
	// callback, or ok == false, degrades the member exactly as before.
	RequestDelegate func(ref *winrtmeta.TypeRef) (string, bool)
}

// Mapper resolves TypeRefs against the loaded Registry.
type Mapper struct {
	Registry *pipeline.Registry
	// ModulePath is this module's import path root.
	ModulePath string

	// Blocked marks severed cross-namespace edges (import-cycle breaks):
	// Blocked[src][dst] forces references from src to dst to degrade
	// instead of importing.
	Blocked map[string]map[string]bool

	// structEmittable memoizes the per-struct emittability decision.
	structEmittable map[string]bool
}

// externalTypes are Windows.Foundation types that are NEVER emitted: they
// already exist in the shared ABI foundation (or flatten to a primitive),
// and re-emitting them would fork the identity every signature depends on.
var externalTypes = map[string]Resolved{
	"Windows.Foundation.EventRegistrationToken": {
		GoType: "syswinrt.EventRegistrationToken", Kind: KindStruct,
		StructNamespace: "Windows.Foundation", StructName: "EventRegistrationToken",
	},
	"Windows.Foundation.HResult": {GoType: "int32", Kind: KindScalar},
}

// IsExternalType reports whether the full type name routes to the shared
// ABI foundation instead of being emitted here.
func IsExternalType(namespace, name string) bool {
	_, ok := externalTypes[namespace+"."+name]
	return ok
}

// nativeScalars maps IR Native names to Go scalar types.
var nativeScalars = map[string]string{
	"Char16": "uint16",
	"I1":     "int8",
	"U1":     "byte",
	"I2":     "int16",
	"U2":     "uint16",
	"I4":     "int32",
	"U4":     "uint32",
	"I8":     "int64",
	"U8":     "uint64",
}

// GoType resolves one TypeRef. Cross-namespace references are qualified with
// the owning package alias and recorded in imports.
func (m *Mapper) GoType(ref *winrtmeta.TypeRef, ctx Context, imports ImportSet) Resolved {
	switch ref.Kind {
	case "Native":
		return m.resolveNative(ref, imports)
	case "ApiRef":
		return m.resolveApiRef(ref, ctx, imports)
	case "GenericInst":
		return m.resolveGenericInst(ref, ctx)
	case "GenericParamRef":
		return unsupported("generic-member-skipped", "generic type parameter")
	case "Array":
		return unsupported("array-param-skipped", "conformant array")
	}
	return unsupported("unknown-typeref-kind", "%q", ref.Kind)
}

func (m *Mapper) resolveNative(ref *winrtmeta.TypeRef, imports ImportSet) Resolved {
	switch ref.Name {
	case "Void":
		return Resolved{Kind: KindVoid}
	case "Bool":
		return Resolved{GoType: "bool", Kind: KindBool}
	case "F32":
		return Resolved{GoType: "float32", Kind: KindFloat}
	case "F64":
		return Resolved{GoType: "float64", Kind: KindFloat}
	case "HString":
		return Resolved{GoType: "string", Kind: KindString}
	case "Guid":
		imports["win32"] = Import{Path: Win32RuntimeImport}
		return Resolved{GoType: "win32.GUID", Kind: KindGUID}
	case "Object":
		imports["syswinrt"] = Import{Path: SysWinRTImport}
		return Resolved{GoType: "*syswinrt.IInspectable", Kind: KindObjectPtr}
	}
	if goType, ok := nativeScalars[ref.Name]; ok {
		return Resolved{GoType: goType, Kind: KindScalar}
	}
	return unsupported("unknown-native-type", "%q", ref.Name)
}

// resolveGenericInst maps a closed generic INTERFACE instantiation to the
// concrete (monomorphized) type the gather layer emits into the consuming
// package — package-local, so no import is recorded. Closed DELEGATE
// instantiations (AsyncOperationCompletedHandler`1 et al.) ground the same
// way through the RequestDelegate seam when it is wired (method parameters);
// without it they keep degrading. The open type's namespace is never
// imported, so blocked import edges do not apply here; cross-namespace
// ARGUMENT references are resolved (and blocked-edge checked) when the
// instantiation's (or handler's) methods are lowered.
func (m *Mapper) resolveGenericInst(ref *winrtmeta.TypeRef, ctx Context) Resolved {
	if ctx.RequestInstantiation != nil && m.Registry.Interface(ref.Namespace, ref.Name) != nil {
		name, ok := ctx.RequestInstantiation(ref)
		if !ok {
			return unsupported("generic-member-skipped", "parameterized type %s.%s", ref.Namespace, ref.Name)
		}
		return Resolved{GoType: "*" + name, Kind: KindInterfacePtr}
	}
	if ctx.RequestDelegate != nil && m.Registry.Delegate(ref.Namespace, ref.Name) != nil {
		name, ok := ctx.RequestDelegate(ref)
		if !ok {
			return unsupported("generic-member-skipped", "parameterized type %s.%s", ref.Namespace, ref.Name)
		}
		return Resolved{GoType: "*" + name, Kind: KindDelegatePtr}
	}
	return unsupported("generic-member-skipped", "parameterized type %s.%s", ref.Namespace, ref.Name)
}

func (m *Mapper) resolveApiRef(ref *winrtmeta.TypeRef, ctx Context, imports ImportSet) Resolved {
	if external, ok := externalTypes[ref.Namespace+"."+ref.Name]; ok {
		if external.GoType == "syswinrt.EventRegistrationToken" {
			imports["syswinrt"] = Import{Path: SysWinRTImport}
		}
		return external
	}
	if ref.Namespace != ctx.Namespace && m.Blocked[ctx.Namespace][ref.Namespace] {
		return unsupported("import-cycle-skipped", "reference to %s.%s crosses a severed import edge", ref.Namespace, ref.Name)
	}
	switch ref.TargetKind {
	case "Enum":
		return Resolved{GoType: m.qualifiedName(ref.Namespace, ref.Name, ctx, imports), Kind: KindEnum}
	case "Struct":
		if !m.StructEmittable(ref.Namespace, ref.Name) {
			return unsupported("skipped-struct-ref", "reference to skipped struct %s.%s", ref.Namespace, ref.Name)
		}
		return Resolved{
			GoType:          m.qualifiedName(ref.Namespace, ref.Name, ctx, imports),
			Kind:            KindStruct,
			StructNamespace: ref.Namespace,
			StructName:      ref.Name,
		}
	case "Interface":
		definition := m.Registry.Interface(ref.Namespace, ref.Name)
		if definition == nil {
			return unsupported("unresolved-typeref", "%s.%s", ref.Namespace, ref.Name)
		}
		if definition.Arity > 0 {
			return unsupported("generic-member-skipped", "generic interface %s.%s", ref.Namespace, ref.Name)
		}
		return Resolved{GoType: "*" + m.qualifiedName(ref.Namespace, ref.Name, ctx, imports), Kind: KindInterfacePtr}
	case "Class":
		return m.resolveClassRef(ref, ctx, imports)
	case "Delegate":
		if ctx.RequestDelegate != nil {
			if name, ok := ctx.RequestDelegate(ref); ok {
				return Resolved{GoType: "*" + name, Kind: KindDelegatePtr}
			}
		}
		return unsupported("delegate-param-skipped", "delegate %s.%s", ref.Namespace, ref.Name)
	case "":
		return unsupported("unresolved-typeref", "%s.%s", ref.Namespace, ref.Name)
	}
	return unsupported("unknown-target-kind", "%s.%s (%q)", ref.Namespace, ref.Name, ref.TargetKind)
}

// resolveClassRef lowers a runtime-class reference: a class in a signature
// means its default interface at the ABI — composable classes included
// (instantiate-only composition: the reference is still just the default
// interface pointer). When no emittable default interface is reachable
// (statics-only class, generic default interface, a severed import edge) the
// reference degrades to the raw IInspectable with an explanatory note.
func (m *Mapper) resolveClassRef(ref *winrtmeta.TypeRef, ctx Context, imports ImportSet) Resolved {
	class := m.Registry.Class(ref.Namespace, ref.Name)
	if class == nil {
		return unsupported("unresolved-typeref", "%s.%s", ref.Namespace, ref.Name)
	}
	if class.DefaultInterface != nil && class.DefaultInterface.Kind == "ApiRef" {
		resolved := m.GoType(class.DefaultInterface, ctx, imports)
		if resolved.Kind == KindInterfacePtr {
			return resolved
		}
	}
	imports["syswinrt"] = Import{Path: SysWinRTImport}
	return Resolved{
		GoType: "*syswinrt.IInspectable",
		Kind:   KindObjectPtr,
		Note:   fmt.Sprintf("class %s.%s is projected as IInspectable (no emittable default interface is reachable here)", ref.Namespace, ref.Name),
	}
}

// StructEmittable reports whether a struct's definition can be emitted:
// every field must resolve to a representable value shape. References to
// unemittable structs must degrade rather than name an undefined type.
func (m *Mapper) StructEmittable(namespace, name string) bool {
	key := namespace + "." + name
	if IsExternalType(namespace, name) {
		return true // never emitted here, but always representable
	}
	if verdict, seen := m.structEmittable[key]; seen {
		return verdict
	}
	definition := m.Registry.Struct(namespace, name)
	if definition == nil {
		return false
	}
	if m.structEmittable == nil {
		m.structEmittable = map[string]bool{}
	}
	// Optimistic seed breaks (metadata-invalid) reference cycles.
	m.structEmittable[key] = true
	verdict := true
	for i := range definition.Fields {
		scratch := ImportSet{}
		resolved := m.GoType(&definition.Fields[i].Type, Context{Namespace: namespace}, scratch)
		switch resolved.Kind {
		case KindScalar, KindFloat, KindBool, KindGUID, KindEnum, KindStruct:
			// Value shapes embed fine; Bool fields are one byte at the ABI,
			// which Go bool matches.
		default:
			verdict = false
		}
	}
	m.structEmittable[key] = verdict
	return verdict
}

// SingleIntegerField returns the field selector for a struct that flattens
// to exactly one integer field of at most eight bytes — the only struct
// shape a by-value parameter can take through SyscallN (DateTime, TimeSpan).
func (m *Mapper) SingleIntegerField(namespace, name string) (string, bool) {
	if namespace+"."+name == "Windows.Foundation.EventRegistrationToken" {
		return "Value", true
	}
	definition := m.Registry.Struct(namespace, name)
	if definition == nil || len(definition.Fields) != 1 {
		return "", false
	}
	field := &definition.Fields[0]
	if field.Type.Kind != "Native" {
		return "", false
	}
	if _, ok := nativeScalars[field.Type.Name]; !ok {
		return "", false
	}
	return naming.Export(field.Name), true
}

// ImportPathFor returns the Go import path of the package that carries the
// given namespace.
func (m *Mapper) ImportPathFor(namespace string) string {
	return m.ModulePath + "/bindings/winrt/" + naming.PackagePath(namespace)
}

// RuntimeImportPath returns the import path of this module's hand-written
// runtime package (alias winrt).
func (m *Mapper) RuntimeImportPath() string {
	return m.ModulePath + "/bindings/runtime/winrt"
}

// qualifiedName renders Name qualified by the owning package (recording the
// import) unless it lives in the namespace being emitted.
func (m *Mapper) qualifiedName(namespace, name string, ctx Context, imports ImportSet) string {
	name = naming.Export(name)
	if namespace == "" || namespace == ctx.Namespace {
		return name
	}
	alias := naming.ImportAlias(namespace)
	imports[alias] = Import{Path: m.ImportPathFor(namespace), Namespace: namespace}
	return alias + "." + name
}

// unsupported builds the degradation result with its diagnostic reason.
func unsupported(key, format string, args ...any) Resolved {
	return Resolved{Kind: KindUnsupported, Reason: key + ": " + fmt.Sprintf(format, args...)}
}
