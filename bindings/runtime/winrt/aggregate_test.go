//go:build windows && amd64

package winrt

import (
	"testing"
	"unsafe"

	win32 "github.com/deploymenttheory/go-bindings-win32/bindings/runtime/win32"
	syswinrt "github.com/deploymenttheory/go-bindings-win32/bindings/win32/system/winrt"
)

// A pair of made-up IIDs. Nothing native resolves these; the point is that the object
// answers for them and only them.
var (
	iidTestPrimary = win32.GUID{Data1: 0x11111111, Data2: 0x2222, Data3: 0x3333,
		Data4: [8]byte{0x44, 0x44, 0x55, 0x55, 0x66, 0x66, 0x77, 0x77}}
	iidTestSecondary = win32.GUID{Data1: 0x88888888, Data2: 0x9999, Data3: 0xaaaa,
		Data4: [8]byte{0xbb, 0xbb, 0xcc, 0xcc, 0xdd, 0xdd, 0xee, 0xee}}
	iidTestAbsent = win32.GUID{Data1: 0xdeadbeef}
)

// facetOf is the identity facet of an implementation, for the package's callSlot.
func facetOf(im *Implementation) *inspectable { return (*inspectable)(im.Ptr()) }

// queryInterface drives slot 0 the way native code would, returning the HRESULT and
// whatever landed in the out-pointer.
func queryInterface(t *testing.T, this *inspectable, iid win32.GUID) (uintptr, uintptr) {
	t.Helper()
	var out uintptr
	hr := callSlot(t, this, 0, uintptr(unsafe.Pointer(&iid)), uintptr(unsafe.Pointer(&out)))
	return hr, out
}

// TestImplementationAnswersItsInterfaces covers the basics an implemented object must
// get right before anything else is worth testing: it answers for the IIDs it declared,
// refuses one it did not, and keeps COM identity across facets.
func TestImplementationAnswersItsInterfaces(t *testing.T) {
	var calls []string
	object, err := NewImplementation("Test.Object",
		Interface{IID: iidTestPrimary, Methods: []Method{
			func(args []uintptr) uintptr { calls = append(calls, "primary0"); return 0 },
		}},
		Interface{IID: iidTestSecondary, Methods: []Method{
			func(args []uintptr) uintptr { calls = append(calls, "secondary0"); return 0 },
		}},
	)
	if err != nil {
		t.Fatalf("NewImplementation: %v", err)
	}
	defer object.Close()

	identity := facetOf(object)

	// Both declared interfaces resolve.
	for _, iid := range []win32.GUID{iidTestPrimary, iidTestSecondary} {
		hr, out := queryInterface(t, identity, iid)
		if hr != 0 || out == 0 {
			t.Errorf("QueryInterface(%v) = 0x%X / %#x, want success", iid, hr, out)
		}
		callSlot(t, identity, 2) // release the reference QI added
	}

	// An undeclared one does not, and must leave the out-pointer null rather than
	// stale — a caller that ignores the HRESULT would otherwise use a garbage pointer.
	hr, out := queryInterface(t, identity, iidTestAbsent)
	if hr == 0 {
		t.Error("QueryInterface succeeded for an interface the object does not implement")
	}
	if out != 0 {
		t.Errorf("a failed QueryInterface left %#x in the out-pointer, want 0", out)
	}

	// COM identity: IUnknown from ANY facet is the same pointer. Getting this wrong is
	// the classic aggregation bug, and it is invisible until something compares
	// pointers for equality.
	_, secondary := queryInterface(t, identity, iidTestSecondary)
	_, viaIdentity := queryInterface(t, identity, iidUnknown)
	_, viaSecondary := queryInterface(t, facetAt(t, secondary), iidUnknown)
	if viaIdentity != viaSecondary {
		t.Errorf("IUnknown differs by facet: %#x via identity, %#x via the tear-off",
			viaIdentity, viaSecondary)
	}
	callSlot(t, identity, 2)
	callSlot(t, identity, 2)
	callSlot(t, identity, 2)
}

// TestImplementedMethodsDispatchToTheRightSlot is the one the trampoline table exists
// for. Every slot shares a body that recovers the facet from `this` and indexes its own
// methods, so an off-by-one there would call a neighbouring method — which returns
// S_OK and does the wrong thing, rather than failing.
func TestImplementedMethodsDispatchToTheRightSlot(t *testing.T) {
	var called []int
	methods := make([]Method, 5)
	for index := range methods {
		methods[index] = func(args []uintptr) uintptr {
			called = append(called, index)
			return uintptr(index) // a distinguishable HRESULT per slot
		}
	}
	object, err := NewImplementation("Test.Slots", Interface{IID: iidTestPrimary, Methods: methods})
	if err != nil {
		t.Fatalf("NewImplementation: %v", err)
	}
	defer object.Close()

	for index := range methods {
		if got := callSlot(t, facetOf(object), 6+index); got != uintptr(index) {
			t.Errorf("slot %d returned %d, want %d — the trampoline table is misindexed",
				6+index, got, index)
		}
	}
	if len(called) != len(methods) {
		t.Fatalf("%d methods ran, want %d", len(called), len(methods))
	}
	for index := range called {
		if called[index] != index {
			t.Errorf("call %d dispatched to method %d", index, called[index])
		}
	}
}

// TestImplementedMethodsReceiveTheirArguments checks the raw words arrive in order.
func TestImplementedMethodsReceiveTheirArguments(t *testing.T) {
	var received []uintptr
	object, err := NewImplementation("Test.Args", Interface{IID: iidTestPrimary, Methods: []Method{
		func(args []uintptr) uintptr {
			received = append([]uintptr{}, args[:3]...)
			return 0
		},
	}})
	if err != nil {
		t.Fatalf("NewImplementation: %v", err)
	}
	defer object.Close()

	callSlot(t, facetOf(object), 6, 0x1111, 0x2222, 0x3333)
	want := []uintptr{0x1111, 0x2222, 0x3333}
	for index := range want {
		if received[index] != want[index] {
			t.Errorf("arg %d = %#x, want %#x", index, received[index], want[index])
		}
	}
}

// TestAggregationForwardsUnknownInterfaces is the aggregation contract itself, tested
// against a second Go object standing in for the runtime class.
//
// The outer must answer for what it implements AND for what the inner implements, and
// the caller cannot tell which half answered. That is exactly what WinUI needs from a
// derived Application: IXamlMetadataProvider from the Go side, every Application
// interface from the native side, one object.
func TestAggregationForwardsUnknownInterfaces(t *testing.T) {
	inner, err := NewImplementation("Test.Inner", Interface{IID: iidTestSecondary, Methods: []Method{
		func(args []uintptr) uintptr { return 0 },
	}})
	if err != nil {
		t.Fatalf("inner: %v", err)
	}

	outer, err := NewImplementation("Test.Outer", Interface{IID: iidTestPrimary, Methods: []Method{
		func(args []uintptr) uintptr { return 0 },
	}})
	if err != nil {
		t.Fatalf("outer: %v", err)
	}
	defer outer.Close()

	// Stand in for a composable factory: hand back the inner, as CreateInstance would.
	err = outer.Aggregate(func(o *syswinrt.IInspectable, i **syswinrt.IInspectable) error {
		*i = (*syswinrt.IInspectable)(inner.Ptr())
		return nil
	})
	if err != nil {
		t.Fatalf("Aggregate: %v", err)
	}

	// The outer's own interface still answers.
	if hr, out := queryInterface(t, facetOf(outer), iidTestPrimary); hr != 0 || out == 0 {
		t.Errorf("the outer no longer answers for its own interface: 0x%X", hr)
	} else {
		callSlot(t, facetOf(outer), 2)
	}

	// And the inner's now answers through the outer, which is the whole point.
	hr, out := queryInterface(t, facetOf(outer), iidTestSecondary)
	if hr != 0 || out == 0 {
		t.Fatalf("the outer does not forward to the inner: 0x%X", hr)
	}
	if out != uintptr(inner.Ptr()) {
		t.Errorf("forwarded QueryInterface returned %#x, want the inner's facet %#x",
			out, uintptr(inner.Ptr()))
	}
	callSlot(t, facetAt(t, out), 2)

	// Something neither implements is still refused.
	if hr, _ := queryInterface(t, facetOf(outer), iidTestAbsent); hr == 0 {
		t.Error("the aggregate answered for an interface neither half implements")
	}
}

// TestAggregationRefusesAnInnerlessFactory pins the failure mode that would otherwise
// be silent: a factory that returns success without writing the inner leaves an
// aggregate that answers nothing, and finding that out later is much harder.
func TestAggregationRefusesAnInnerlessFactory(t *testing.T) {
	outer, err := NewImplementation("Test.NoInner", Interface{IID: iidTestPrimary})
	if err != nil {
		t.Fatalf("NewImplementation: %v", err)
	}
	defer outer.Close()

	err = outer.Aggregate(func(o *syswinrt.IInspectable, i **syswinrt.IInspectable) error { return nil })
	if err == nil {
		t.Error("Aggregate accepted a factory that wrote no inner")
	}
}
