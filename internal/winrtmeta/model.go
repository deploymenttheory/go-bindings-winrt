// Package winrtmeta defines the intermediate representation (IR) for the
// WinRT API surface, projected from the pinned contract winmds. One
// NamespaceMeta is serialized per namespace as
// metadata/winrt/<Namespace>.winrtmeta.json.
//
// The IR is the seam between the winmd reader (producer) and the codegen
// pipeline (consumer): everything downstream of this package is independent
// of ECMA-335. Methods carry the LOGICAL signature exactly as the metadata
// declares it — the HRESULT return and trailing [out, retval] lowering is
// the emit stage's job, not ingest's.
package winrtmeta

// CurrentSchemaVersion is bumped when the IR changes incompatibly; readers
// reject files with a different version so stale caches are re-ingested.
const CurrentSchemaVersion = 1

// NamespaceMeta is the serialized unit: the full API surface of one
// Windows.* namespace, merged across the pinned contract files.
type NamespaceMeta struct {
	// Namespace is the full CLR namespace, e.g. "Windows.Globalization".
	Namespace     string `json:"namespace"`
	SchemaVersion int    `json:"schema_version"`

	// WinmdVersion is the Contracts package version the namespace was
	// projected from (from PROVENANCE.json).
	WinmdVersion string `json:"winmd_version,omitempty"`

	Enums      map[string]Enum      `json:"enums,omitempty"`
	Structs    map[string]Struct    `json:"structs,omitempty"`
	Interfaces map[string]Interface `json:"interfaces,omitempty"`
	Classes    map[string]Class     `json:"classes,omitempty"`
	Delegates  map[string]Delegate  `json:"delegates,omitempty"`
}

// TypeRef is the recursive type reference grammar, mirroring the structured
// ECMA-335 signature under WinRT's projection rules.
type TypeRef struct {
	// Kind is one of "Native", "ApiRef", "GenericInst", "GenericParamRef",
	// "Array".
	Kind string `json:"kind"`

	// Name: for Native, the primitive ("Void", "Bool" [1 byte at the ABI],
	// "Char16", "I1", "U1", "I2", "U2", "I4", "U4", "I8", "U8", "F32",
	// "F64", "HString", "Guid", "Object" [IInspectable]); for ApiRef and
	// GenericInst, the referenced type's metadata name (arity backtick
	// included, e.g. "IVector`1").
	Name string `json:"name,omitempty"`

	// Namespace is the full owning namespace of an ApiRef/GenericInst
	// target (e.g. "Windows.Foundation.Collections"), used for
	// cross-package import resolution.
	Namespace string `json:"namespace,omitempty"`

	// TargetKind classifies an ApiRef/GenericInst target: "Enum",
	// "Struct", "Interface", "Class", "Delegate". Empty on an unresolved
	// reference (recorded by an unresolved-typeref diagnostic).
	TargetKind string `json:"target_kind,omitempty"`

	// Args holds the type arguments of a GenericInst (e.g. the T of
	// IVector`1<T>).
	Args []TypeRef `json:"args,omitempty"`

	// Elem is the element type of an Array (WinRT conformant array;
	// unsupported at emit, which skips the member with a diagnostic).
	Elem *TypeRef `json:"elem,omitempty"`

	// Index is the 0-based parameter position of a GenericParamRef.
	Index uint32 `json:"index,omitempty"`
}

// Param is one logical method parameter.
type Param struct {
	Name string  `json:"name"`
	Type TypeRef `json:"type"`

	// Out is set on [out] parameters (ParamAttrOut).
	Out bool `json:"out,omitempty"`
}

// Method is one interface method in MethodDef order — THE vtable order:
// slot = 6 (past IInspectable) + index. Members are never reordered or
// dropped, so skipped members keep their slot at emit.
type Method struct {
	// Name is the metadata MethodDef name; overloads share it.
	Name string `json:"name"`
	// Overload is the unique name from the [Overload] attribute (empty
	// when absent). The projected Go name is Overload if non-empty, else
	// Name.
	Overload string `json:"overload,omitempty"`
	// IsDefaultOverload marks the [DefaultOverload] member of an overload
	// group.
	IsDefaultOverload bool `json:"is_default_overload,omitempty"`

	Params []Param `json:"params,omitempty"`
	// Return is the logical return type; nil for void. Ingest does NOT
	// synthesize the ABI HRESULT/retval shape — emit lowers.
	Return *TypeRef `json:"return,omitempty"`
}

// Property references its accessors by metadata method name (get_X/put_X),
// paired via MethodSemantics; the methods themselves stay in Methods at
// their vtable slots.
type Property struct {
	Name string  `json:"name"`
	Type TypeRef `json:"type"`
	// Getter/Setter are the accessor MethodDef names (e.g. "get_Year",
	// "put_Year"); empty when the accessor is absent.
	Getter string `json:"getter,omitempty"`
	Setter string `json:"setter,omitempty"`
}

// Event references its add_/remove_ accessors by metadata method name,
// paired via MethodSemantics.
type Event struct {
	Name string `json:"name"`
	// Type is the event delegate type (often a GenericInst of
	// TypedEventHandler`2).
	Type         TypeRef `json:"type"`
	AddMethod    string  `json:"add_method,omitempty"`
	RemoveMethod string  `json:"remove_method,omitempty"`
}

// Interface is a WinRT interface: vtable methods in MethodDef order after
// the six IInspectable slots.
type Interface struct {
	// GUID is the IID from [Guid] in canonical lowercase form.
	GUID string `json:"guid,omitempty"`
	// ExclusiveTo is the full name of the runtime class this interface is
	// [ExclusiveTo] (empty for shared interfaces).
	ExclusiveTo string `json:"exclusive_to,omitempty"`
	// Arity is the generic parameter count (0 for non-generic). Generic
	// interfaces are kept in the IR and skipped at emit.
	Arity int `json:"arity,omitempty"`
	// Requires lists the interfaces this one requires (InterfaceImpl
	// targets; TypeSpec targets decode to GenericInst refs).
	Requires []TypeRef `json:"requires,omitempty"`

	Methods    []Method   `json:"methods,omitempty"`
	Properties []Property `json:"properties,omitempty"`
	Events     []Event    `json:"events,omitempty"`
}

// Class is a WinRT runtime class. The ABI surface lives on its interfaces;
// the class records activation/composition shape only.
type Class struct {
	// DefaultInterface is the InterfaceImpl target carrying [Default] —
	// the interface a new instance is typed as.
	DefaultInterface *TypeRef `json:"default_interface,omitempty"`
	// Interfaces lists every InterfaceImpl target (instance interfaces).
	Interfaces []TypeRef `json:"interfaces,omitempty"`

	// ActivatableDirect is set by an [Activatable] whose first fixed arg
	// is a version (not a factory type): RoActivateInstance works.
	ActivatableDirect bool `json:"activatable_direct,omitempty"`
	// ActivatableFactories are the factory interface full names from
	// [Activatable(Type, ...)] overloads.
	ActivatableFactories []string `json:"activatable_factories,omitempty"`
	// StaticInterfaces are the [Static(Type, ...)] interface full names.
	StaticInterfaces []string `json:"static_interfaces,omitempty"`
	// ComposableFactories are the [Composable(Type, ...)] factory full
	// names — emit projects each qualifying factory method as a null-outer
	// composable constructor (instantiate-only composition).
	ComposableFactories []string `json:"composable_factories,omitempty"`

	// Composable is set when the class extends another runtime class
	// (Extends != System.Object). Composable classes emit like any other
	// class (instantiate-only: Go-side derivation stays out of scope).
	Composable bool `json:"composable,omitempty"`
}

// Delegate is a WinRT delegate: a TypeDef extending
// System.MulticastDelegate whose Invoke method carries the signature.
type Delegate struct {
	// GUID is the delegate's IID from [Guid].
	GUID string `json:"guid,omitempty"`
	// Arity is the generic parameter count (0 for non-generic).
	Arity int `json:"arity,omitempty"`
	// Invoke is the logical Invoke signature.
	Invoke Method `json:"invoke"`
}

// Enum is a WinRT enum: Int32 or UInt32 backed only (validated at ingest).
type Enum struct {
	// BaseType is the Go-facing integral base: "int32" or "uint32".
	BaseType string `json:"base_type"`
	// IsFlags marks a [Flags] enum (UInt32-backed).
	IsFlags bool         `json:"is_flags,omitempty"`
	Members []EnumMember `json:"members,omitempty"`
}

// EnumMember is one enum value; Value is its decimal string representation.
type EnumMember struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// Struct is a WinRT value struct (blittable fields only by WinRT rules).
type Struct struct {
	Fields []StructField `json:"fields,omitempty"`
}

// StructField is one field of a Struct.
type StructField struct {
	Name string  `json:"name"`
	Type TypeRef `json:"type"`
}
