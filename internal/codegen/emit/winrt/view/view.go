// Package view is the pure-data IR consumed by the WinRT render templates.
// It imports no metadata or typemap packages — every field is a fully
// resolved fragment, so templates only branch and interpolate, never decide.
// (The render firewall.)
package view

// EnumModel is one named enum type with its members.
type EnumModel struct {
	TypeName string
	// FullName is the metadata name ("Windows.Globalization.DayOfWeek") for
	// the doc comment.
	FullName string
	BaseType string
	IsFlags  bool
	Members  []EnumMemberModel
	// UniqueMembers is Members deduped by value (first name wins) — the
	// switch cases of String(); duplicate case values would not compile.
	UniqueMembers []EnumMemberModel
}

// EnumMemberModel is one enum constant.
type EnumMemberModel struct {
	Name  string
	Value string
}

// StructModel is one WinRT value struct.
type StructModel struct {
	TypeName string
	FullName string
	Fields   []StructFieldModel
}

// StructFieldModel is one struct field, fully resolved.
type StructFieldModel struct {
	Name   string
	GoType string
}

// InterfaceModel is one WinRT interface: a single vtable-pointer struct
// rooted at IInspectable, dispatching through absolute slots.
type InterfaceModel struct {
	TypeName string
	FullName string
	// GUID is the IID string for the doc comment ("" when absent).
	GUID string
	// ExclusiveTo names the owning runtime class ("" for shared interfaces).
	ExclusiveTo string
	// Requires lists required interfaces as display names (doc comment only;
	// WinRT interfaces never embed at the ABI).
	Requires []string
	// IIDVar/IIDLiteral declare the IID variable (skipped when GUID is "").
	IIDVar     string
	IIDLiteral string
	// Methods holds emitted methods and skipped-slot comments, interleaved
	// in absolute slot order so the vtable layout stays auditable.
	Methods []MethodModel
	// Await, when non-nil, renders the synthesized blocking Await method on
	// an async interface (a monomorphized IAsyncOperation<T> or the plain
	// IAsyncAction).
	Await *AwaitModel
	// CollectionCtor, when non-nil, renders the synthesized Go-implemented
	// collection constructor on a monomorphized IIterable`1 / IVectorView`1 /
	// IVector`1 whose element grounds to a runtime element codec.
	CollectionCtor *CollectionCtorModel
}

// CollectionCtorModel renders the package-level constructor that builds a
// Go-implemented collection object (backed by the runtime's generic
// collection core) and returns it as the package-local consumer type. Every
// decision — the codec selection, the element boxing expression, the sibling
// IID wiring — is precomputed by the gather layer; the template only
// interpolates.
type CollectionCtorModel struct {
	CommentLines []string
	// CtorName is the constructor ("NewIIterableOfUri").
	CtorName string
	// ElemType is the element's Go type in the constructor signature
	// ("string", "*foundation.IUriRuntimeClass", "int32", "win32.GUID").
	ElemType string
	// BoxExpr converts one `item` of ElemType into the runtime payload
	// representation ("item", "uintptr(unsafe.Pointer(item))",
	// "uint64(item)").
	BoxExpr string
	// RuntimeCtor is the runtime core constructor ("NewIterableObject",
	// "NewVectorViewObject", "NewVectorObject").
	RuntimeCtor string
	// Class is the WinRT runtime class display name the object reports
	// ("Windows.Foundation.Collections.IIterable`1<Windows.Foundation.Uri>").
	Class string
	// IIDs is the complete winrt.CollectionIIDs composite literal wiring the
	// package-local derived IID vars.
	IIDs string
	// Codec is the runtime codec expression ("winrt.CodecString",
	// "winrt.CodecInterface", "winrt.CodecScalar(4)", "winrt.CodecGuid").
	Codec string
}

// AwaitModel renders the synthesized Await method: register a Completed
// handler, block on its completion signal, surface a non-Completed terminal
// status through winrt.AsyncError (with the IAsyncInfo error code), and
// return GetResults(). Present only when SetCompleted and GetResults both
// emitted, the Completed handler grounded, and the IAsyncInfo/AsyncStatus
// references resolved.
type AwaitModel struct {
	CommentLines []string
	// ReturnSig mirrors GetResults's return signature
	// ("(*IStorageFile, error)"; "error" for IAsyncAction).
	ReturnSig string
	// StatusType is the AsyncStatus Go type, package-qualified as needed
	// ("foundation.AsyncStatus").
	StatusType string
	// StatusCompleted is the Completed constant expression
	// ("foundation.AsyncStatusCompleted").
	StatusCompleted string
	// HandlerCtor is the grounded Completed-handler constructor
	// ("NewAsyncOperationCompletedHandlerOfStorageFile").
	HandlerCtor string
	// InfoType is the IAsyncInfo Go type without the leading *
	// ("foundation.IAsyncInfo").
	InfoType string
	// InfoIIDRef is the address expression of IAsyncInfo's IID variable
	// ("&foundation.IID_IAsyncInfo").
	InfoIIDRef string
	// ErrPrefix precedes err/AsyncError in return statements: "" when Await
	// returns only an error, otherwise the zero value plus ", "
	// ("nil, ", `"", `).
	ErrPrefix string
}

// Return-shape discriminants for MethodModel.ReturnKind. The template
// branches on these and nothing else.
const (
	// RetError: no logical return — `return win32.ErrIfFailed(int32(r1))`.
	RetError = 0
	// RetValue: `return <ResultExpr>, win32.ErrIfFailed(int32(r1))`.
	RetValue = 1
	// RetString: HSTRING retval — ErrIfFailed short-circuits before
	// TakeHString consumes the handle.
	RetString = 2
)

// MethodModel is one vtable method (or, when SkipComment is set, a
// skipped-slot marker keeping the vtable layout auditable).
type MethodModel struct {
	// SkipComment renders a standalone `// slot N: name skipped: reason`
	// comment; every other field is unused when it is non-empty.
	SkipComment string

	CommentLines []string
	GoName       string
	ParamStr     string
	// ReturnSig is the complete return signature ("error",
	// "(string, error)", "(foundation.DateTime, error)").
	ReturnSig string
	// Slot is the absolute vtable index (6 + MethodDef index).
	Slot int
	// Preamble holds statements that convert idiomatic params into raw
	// syscall words (HSTRING inputs, bool→0/1) before dispatch.
	Preamble []string
	// ArgExprs are the SyscallN argument words after the self word,
	// including the trailing retval out-pointer when the method has one.
	ArgExprs []string
	// ReturnKind selects the body shape (Ret* constants).
	ReturnKind int
	// ResultDecl declares the retval local ("result := new(int32)") —
	// heap-allocated, because the native side writes through it and a stack
	// address can go stale mid-call (the out-param invariant; see buildMethod
	// in the gather layer). Empty for RetError.
	ResultDecl string
	// ResultExpr converts the retval local to the Go return value
	// ("*result", "*result != 0", "winrt.TakeHString(*result)").
	ResultExpr string
	// ZeroReturn is the zero value returned alongside a non-nil error in
	// preamble/RetString short-circuits (`""`, "0", "nil").
	ZeroReturn string
}

// DelegateModel is one event-delegate handler type emitted into the
// consuming package's <pkg>_delegates.go: a typed wrapper over the runtime's
// Go-implemented Delegate, with a constructor whose adapter converts the raw
// Invoke ABI words into typed callback arguments.
type DelegateModel struct {
	TypeName string
	// FullName is the display name for the doc comment
	// ("Windows.Foundation.TypedEventHandler`2<Windows.Foundation.IMemoryBufferReference, Object>").
	FullName string
	// GUID is the delegate IID string for the doc comment (declared, or
	// pinterface-derived for a generic instantiation).
	GUID string
	// IIDVar/IIDLiteral declare the IID variable.
	IIDVar     string
	IIDLiteral string
	// CtorName is the typed constructor ("New" + TypeName).
	CtorName string
	// CtorCommentLines document the constructor, including the
	// borrowed-reference contract when pointer or string arguments apply.
	CtorCommentLines []string
	// FnParams is the callback signature's parameter list
	// ("sender *IMemoryBufferReference, args *syswinrt.IInspectable").
	FnParams string
	// ParamCount is the number of raw ABI words Invoke receives (the
	// delegate runtime's NewDelegate paramCount).
	ParamCount int
	// ArgExprs convert the adapter's raw words to the typed callback
	// arguments, in parameter order ("(*IMemoryBufferReference)(unsafe.Pointer(raw[0]))").
	ArgExprs []string
}

// ClassModel is one runtime class (composable or not): a struct embedding
// its default interface by value, plus the package-level statics accessors
// and constructors surfaced from the class's activation factory. A
// statics-only class emits with TypeName "" — no class type, only Statics.
type ClassModel struct {
	// TypeName is the emitted class type; "" when the class type itself is
	// not emitted (statics-only classes) and only Statics render.
	TypeName string
	FullName string
	// DefaultInterface is the embedded default interface's Go type name
	// (possibly package-qualified).
	DefaultInterface string
	// DefaultIIDRef is the address expression of the default interface's
	// IID variable ("&IID_ICalendar").
	DefaultIIDRef string
	// CtorName is the direct-activation constructor ("NewCalendar"); ""
	// when the class is not directly activatable.
	CtorName string
	// QueryMethods project the class's other non-generic instance
	// interfaces through QueryInterface.
	QueryMethods []QueryMethodModel
	// Statics are the package-level accessors for the class's [Static]
	// interfaces.
	Statics []StaticsAccessorModel
	// Factories are the package-level factory constructors projected from
	// the class's [Activatable] factory interfaces.
	Factories []FactoryFuncModel
	// ComposableCtors are the package-level null-outer composable
	// constructors projected from the class's [Composable] factory
	// interfaces (instantiate-only composition).
	ComposableCtors []ComposableCtorModel
}

// StaticsAccessorModel is one package-level statics accessor: a func
// returning the class's statics interface fetched through its activation
// factory (GetActivationFactory already queries the statics IID, so the
// returned pointer IS the statics interface).
type StaticsAccessorModel struct {
	// FuncName is the accessor ("CalendarIdentifiersStatics" — the statics
	// interface name with its I prefix stripped).
	FuncName string
	// InterfaceType is the statics interface's Go type name (possibly
	// package-qualified).
	InterfaceType string
	// InterfaceFullName is the statics interface's full metadata name for
	// the doc comment.
	InterfaceFullName string
	// IIDRef is the address expression of the statics interface's IID
	// variable.
	IIDRef string
	// ClassFullName is the runtime class's activation name
	// ("Windows.Globalization.CalendarIdentifiers").
	ClassFullName string
}

// FactoryFuncModel is one package-level factory constructor: a func fetching
// the class's activation factory, delegating to the generated factory
// interface method, and wrapping the returned default-interface pointer as
// the class type.
type FactoryFuncModel struct {
	// FuncName is the constructor (the factory method's projected name,
	// e.g. "CreateCalendarWithTimeZone").
	FuncName string
	// FactoryType is the factory interface's Go type name ("ICalendarFactory").
	FactoryType string
	// FactoryFullName is the factory interface's full metadata name for the
	// doc comment.
	FactoryFullName string
	// FactoryIIDRef is the address expression of the factory interface's
	// IID variable.
	FactoryIIDRef string
	// MethodName is the generated factory-interface method the constructor
	// delegates to.
	MethodName string
	// ParamStr is the parameter list, identical to the factory method's
	// (already lowered by the interface emission).
	ParamStr string
	// ArgNames pass the parameters through to the factory method, in order.
	ArgNames []string
}

// ComposableCtorModel is one package-level composable constructor: a func
// fetching the class's activation factory, calling the generated composable
// factory method with a NULL outer (the class is created as itself — Go-side
// derivation is out of scope), releasing the returned inner (a second
// reference to the same object under null-outer composition), and wrapping
// the returned default-interface pointer as the class type.
type ComposableCtorModel struct {
	// FuncName is the constructor ("NewButton" for CreateInstance,
	// "NewFooWithBar" for CreateInstanceWithBar).
	FuncName string
	// FactoryType is the composable factory interface's Go type name
	// ("IButtonFactory").
	FactoryType string
	// FactoryFullName is the factory interface's full metadata name for the
	// doc comment.
	FactoryFullName string
	// FactoryIIDRef is the address expression of the factory interface's
	// IID variable.
	FactoryIIDRef string
	// MethodName is the generated factory-interface method the constructor
	// delegates to.
	MethodName string
	// ParamStr is the LEADING parameter list — the factory method's
	// parameters minus the trailing (baseInterface, innerInterface) pair the
	// constructor supplies itself.
	ParamStr string
	// InnerName is the local holding the inner out-pointer
	// ("inner"; freshened against the leading parameter names).
	InnerName string
	// ArgNames pass the leading parameters through to the factory method,
	// followed by the composition pair ("nil", InnerName).
	ArgNames []string
}

// QueryMethodModel is one As<Interface> query method on a runtime class.
type QueryMethodModel struct {
	// GoName is "As" + the interface name with its I prefix stripped
	// (AsTimeZoneOnCalendar).
	GoName string
	// InterfaceType is the target interface's Go type name (possibly
	// package-qualified).
	InterfaceType string
	// IIDRef is the address expression of the interface's IID variable.
	IIDRef string
}
