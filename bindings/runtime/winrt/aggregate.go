//go:build windows && amd64

package winrt

// Go objects that implement arbitrary WinRT interfaces, and act as the controlling
// outer of an aggregated runtime class.
//
// The machinery underneath is the same facet engine the collections use
// (inspectable.go): one vtable-bearing word per interface inside a single allocation,
// a shared reference count on the identity facet, and QueryInterface answering
// IUnknown/IInspectable with the identity pointer from any facet. What is added here
// is a public way to supply the method bodies, and the aggregation contract.
//
// AGGREGATION is why this exists. WinUI resolves the XAML types named inside its own
// theme dictionaries by asking the application object for IXamlMetadataProvider — see
// MUXControlsFactory::VerifyInitialized in microsoft/microsoft-ui-xaml, which says so
// in its error text. A native Application cannot be made to answer that. The
// [Composable] pattern is the supported route: the factory's CreateInstance takes a
// controlling OUTER and returns a non-delegating INNER, and from then on the outer's
// IUnknown is the object's identity. QueryInterface for anything the outer does not
// implement is forwarded to the inner, so the aggregate answers for both halves.
//
// Without it, a third of the WinUI control set cannot render: TextBox, ProgressBar and
// their neighbours fail to load their default styles and take the process with them.

import (
	"fmt"
	"sync"
	"syscall"
	"unsafe"

	win32 "github.com/deploymenttheory/go-bindings-win32/bindings/runtime/win32"
	syswinrt "github.com/deploymenttheory/go-bindings-win32/bindings/win32/system/winrt"
)

// MaxImplementedMethods bounds how many methods one implemented interface may have.
//
// Each slot needs its own syscall.NewCallback trampoline, and those allocations are
// process-permanent — so the table is fixed and shared across every implementation
// rather than built per object. Twenty-four covers every interface a Go application is
// likely to implement; IXamlMetadataProvider, the one that motivated this, has three.
const MaxImplementedMethods = 24

// MaxImplementedArgs is how many raw ABI words past `this` a trampoline reads.
//
// The trampolines declare a fixed arity because syscall.NewCallback needs a concrete
// signature and the real arity varies per slot. Reading more words than the caller
// passed is safe on Windows x64 — the first four live in registers the caller owns and
// the rest in its frame, so the reads are of mapped memory — and the surplus is simply
// ignored by a Method that knows its own shape.
const MaxImplementedArgs = 8

// eNotImplemented is returned for a slot an Interface left nil, which is a caller
// error rather than a runtime condition — but returning it is better than calling
// through a nil func and taking the process down.
const eNotImplemented = uintptr(0x80004001) // E_NOTIMPL

// Method is one implemented vtable slot. It receives the raw ABI words after the
// implicit `this` and returns an HRESULT; return 0 for success.
//
// The arguments are raw: a pointer parameter arrives as the pointer's word, and an
// out-parameter as the address to write through. Interpreting them is the caller's
// job, exactly as it is for NewDelegate.
type Method func(args []uintptr) uintptr

// Interface is one WinRT interface a Go object implements: its IID and its methods in
// vtable order, starting at slot 6 (IInspectable occupies 0-5).
type Interface struct {
	IID     win32.GUID
	Methods []Method
}

// Implementation is a Go-implemented WinRT object. It owns one facet per interface,
// and optionally an aggregated inner it forwards unrecognised QueryInterface calls to.
type Implementation struct {
	facets []*inspectable
	// vtables are kept alive for as long as the object is: the facets' lpVtbl words
	// point into them, and native code dereferences those on every call.
	vtables [][]uintptr
}

// NewImplementation creates a Go object implementing the given interfaces, reported to
// GetRuntimeClassName as class. The object starts with one reference owned by the
// caller; Close it when no native code can still hold it.
//
// The first interface is the identity facet, so its pointer is the object's COM
// identity — pass the one the object is most naturally "a" instance of.
func NewImplementation(class string, interfaces ...Interface) (*Implementation, error) {
	if len(interfaces) == 0 {
		return nil, fmt.Errorf("winrt: an implementation needs at least one interface")
	}
	implementationTrampolinesOnce.Do(buildImplementationTrampolines)

	implementation := &Implementation{}
	for _, definition := range interfaces {
		if len(definition.Methods) > MaxImplementedMethods {
			return nil, fmt.Errorf("winrt: interface %v has %d methods (max %d)",
				definition.IID, len(definition.Methods), MaxImplementedMethods)
		}
		// Six IInspectable slots then one trampoline per method. The trampoline for
		// slot 6+n is the same function for every object; it recovers the facet from
		// the `this` word and indexes its own methods.
		vtable := make([]uintptr, 0, 6+len(definition.Methods))
		vtable = append(vtable,
			inspectableCallbackQI, inspectableCallbackAddRef, inspectableCallbackRelease,
			inspectableCallbackGetIids, inspectableCallbackGetRuntimeClassName, inspectableCallbackGetTrustLevel)
		for index := range definition.Methods {
			vtable = append(vtable, implementationTrampolines[index])
		}
		facet := &inspectable{
			lpVtbl:  &vtable[0],
			iids:    []win32.GUID{definition.IID},
			methods: definition.Methods,
		}
		implementation.vtables = append(implementation.vtables, vtable)
		implementation.facets = append(implementation.facets, facet)
	}

	initInspectable(class, implementation.facets...)
	implementation.facets[0].destroy = implementation.releaseInner
	return implementation, nil
}

// Ptr is the object's COM identity: the pointer to hand to native code, and the
// controlling outer to pass to a composable factory.
func (im *Implementation) Ptr() unsafe.Pointer {
	return unsafe.Pointer(im.facets[0])
}

// Outer is Ptr typed for the composable factories, whose CreateInstance takes the
// controlling outer as an IInspectable.
func (im *Implementation) Outer() *syswinrt.IInspectable {
	return (*syswinrt.IInspectable)(im.Ptr())
}

// Aggregate makes this object the controlling outer of a runtime class.
//
// create is the composable factory call — CreateInstance(outer, &inner) — which
// returns the aggregate's default interface and writes the non-delegating inner. From
// then on QueryInterface for anything this object does not implement is forwarded to
// that inner, so the aggregate answers for the Go interfaces AND the runtime class.
//
// The inner reference is owned here and released when the object is. The returned
// default interface DELEGATES to this object, so holding it holds a reference to the
// outer — which is the aggregation contract, not a leak, and is why a derived
// application is kept for the life of the process rather than released eagerly.
func (im *Implementation) Aggregate(create func(outer *syswinrt.IInspectable, inner **syswinrt.IInspectable) error) error {
	if im.facets[0].inner != nil {
		return fmt.Errorf("winrt: this object is already aggregating a runtime class")
	}
	var inner *syswinrt.IInspectable
	if err := create(im.Outer(), &inner); err != nil {
		return err
	}
	if inner == nil {
		return fmt.Errorf("winrt: the composable factory returned no inner object, so " +
			"aggregation would silently answer nothing")
	}
	im.facets[0].inner = inner
	return nil
}

// releaseInner drops the aggregated inner. Runs once, from the identity facet's
// destroy hook, after the reference count reaches zero.
func (im *Implementation) releaseInner() {
	identity := im.facets[0]
	if identity.inner == nil {
		return
	}
	inner := identity.inner
	identity.inner = nil
	inner.Release()
}

// Close drops the caller's reference. Native code holding its own keeps the object
// alive until it releases too.
func (im *Implementation) Close() uint32 {
	return uint32(inspectableRelease(im.facets[0]))
}

// The shared trampoline table: one function per method slot, each recovering the facet
// from `this` and invoking that facet's own method. Built once, on first use, because
// syscall.NewCallback allocations are process-permanent.
var (
	implementationTrampolinesOnce sync.Once
	implementationTrampolines     [MaxImplementedMethods]uintptr
)

func buildImplementationTrampolines() {
	for index := range implementationTrampolines {
		implementationTrampolines[index] = syscall.NewCallback(implementationSlotFunc(index))
	}
}

// implementationSlotFunc returns the trampoline body for one slot. The index is
// captured, which is exactly what syscall.NewCallback cannot do — so the closure is
// wrapped in a concrete signature and each index gets its own callback.
// The first parameter is typed, not a uintptr: syscall.NewCallback performs the
// conversion, which keeps the native word out of Go's hands as an integer and matches
// how every other callback in this package is declared.
func implementationSlotFunc(index int) func(*inspectable, uintptr, uintptr, uintptr, uintptr, uintptr, uintptr, uintptr, uintptr) uintptr {
	return func(this *inspectable, a0, a1, a2, a3, a4, a5, a6, a7 uintptr) uintptr {
		return invokeImplementedMethod(this, index, [MaxImplementedArgs]uintptr{a0, a1, a2, a3, a4, a5, a6, a7})
	}
}

// invokeImplementedMethod stages the call onto the shared worker, for the
// moving-stack reason documented at length in inspectable.go: the body may grow its
// stack, and a native frame beneath may hold a pointer into the reentered goroutine's.
func invokeImplementedMethod(facet *inspectable, index int, args [MaxImplementedArgs]uintptr) uintptr {
	if !registeredInspectable(facet) {
		return eFail
	}
	staged := args
	return dispatchInspectable(inspectableWork{
		fn:   implementedMethodBody,
		this: facet,
		p0:   unsafe.Pointer(&staged),
		// u0, not a pointer field: the slot index is an integer, and parking it in a
		// pointer word would leave the collector reading it as one.
		u0: uintptr(index),
	})
}

func implementedMethodBody(w *inspectableWork) uintptr {
	facet := w.this
	index := int(w.u0)
	if index >= len(facet.methods) || facet.methods[index] == nil {
		return eNotImplemented
	}
	args := (*[MaxImplementedArgs]uintptr)(w.p0)
	return facet.methods[index](args[:])
}
