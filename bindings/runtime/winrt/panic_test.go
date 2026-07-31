//go:build windows && amd64

package winrt

import (
	"strings"
	"testing"
	"unsafe"
)

// unsafePointerOf keeps the test's own pointer conversion in one named place.
func unsafePointerOf[T any](p *T) unsafe.Pointer { return unsafe.Pointer(p) }

// capturePanicReports redirects the panic reporter for one test.
func capturePanicReports(t *testing.T) *[]string {
	t.Helper()
	var reports []string
	previous := panicReporter
	panicReporter = func(message string) { reports = append(reports, message) }
	t.Cleanup(func() { panicReporter = previous })
	return &reports
}

// TestContainPanicSubstitutesAnHresult is the core of the contract: a panicking body
// returns E_UNEXPECTED instead of unwinding.
//
// It matters because there is no other outcome available. A panic that escapes a body
// reached from native code cannot unwind past the native frames, so the runtime
// terminates the process — during a call the framework is waiting on, which makes it
// look like a hang rather than a crash.
func TestContainPanicSubstitutesAnHresult(t *testing.T) {
	reports := capturePanicReports(t)

	result := func() (result uintptr) {
		defer containPanic("a test body", noSlot, &result)
		panic("deliberate")
	}()

	if result != eUnexpected {
		t.Errorf("result = %#x, want E_UNEXPECTED (%#x)", result, eUnexpected)
	}
	if len(*reports) != 1 {
		t.Fatalf("got %d reports, want 1", len(*reports))
	}
	if !strings.Contains((*reports)[0], "deliberate") {
		t.Errorf("the report does not carry the panic value:\n%s", (*reports)[0])
	}
	if !strings.Contains((*reports)[0], "a test body") {
		t.Errorf("the report does not name the boundary:\n%s", (*reports)[0])
	}
}

// TestContainPanicLeavesSuccessAlone: the barrier must be invisible when nothing panics,
// including for a body that returns a failing HRESULT of its own. Returning an error is
// how a body declines; only a panic is a bug.
func TestContainPanicLeavesSuccessAlone(t *testing.T) {
	reports := capturePanicReports(t)

	for _, want := range []uintptr{0, eFail, eNotImplemented} {
		got := func() (result uintptr) {
			defer containPanic("a test body", noSlot, &result)
			return want
		}()
		if got != want {
			t.Errorf("result = %#x, want %#x", got, want)
		}
	}
	if len(*reports) != 0 {
		t.Errorf("got %d reports, want none", len(*reports))
	}
}

// TestImplementedMethodPanicIsContained drives it through the real path: an
// Implementation whose method panics, called through its own vtable.
//
// Calling through the vtable rather than the Go func is the point — that is the route
// native code takes, and it is the one where an escaping panic would be fatal.
func TestImplementedMethodPanicIsContained(t *testing.T) {
	reports := capturePanicReports(t)

	implementation, err := NewImplementation("Test.Panicking",
		Interface{IID: iidTestPrimary, Methods: []Method{
			func(args []uintptr) uintptr { panic("from an implemented method") },
		}})
	if err != nil {
		t.Fatalf("NewImplementation: %v", err)
	}
	defer implementation.Close()

	// Slot 6 is the first (and only) method. Call it exactly as native code would.
	result := callSlot(t, facetOf(implementation), 6)

	if result != eUnexpected {
		t.Errorf("result = %#x, want E_UNEXPECTED (%#x)", result, eUnexpected)
	}
	if len(*reports) != 1 {
		t.Fatalf("got %d reports, want 1", len(*reports))
	}
	if !strings.Contains((*reports)[0], "vtable slot 6") {
		t.Errorf("the report does not name the slot:\n%s", (*reports)[0])
	}
}

// TestWrongPayloadTypeFailsAtConstruction pins the second half of the same lesson.
//
// Every codec asserts its payload type inside MarshalOut, which runs when NATIVE code
// reads the collection. A wrong type there panics inside a COM callback, terminating
// the process during a call the caller is waiting on — it presents as a hang. This
// asserts the mistake is caught where it is made instead, on the caller's goroutine.
func TestWrongPayloadTypeFailsAtConstruction(t *testing.T) {
	defer func() {
		recovered := recover()
		if recovered == nil {
			t.Fatal("building a vector with a mistyped element did not panic")
		}
		message, ok := recovered.(string)
		if !ok {
			t.Fatalf("panic value is %T, want a string", recovered)
		}
		for _, want := range []string{"element 1", "uintptr"} {
			if !strings.Contains(message, want) {
				t.Errorf("the message does not mention %q:\n%s", want, message)
			}
		}
	}()

	// CodecInterface requires uintptr payloads. A string is the kind of mistake this
	// catches; unsafe.Pointer was the one that actually happened.
	NewVectorObject("Test.Mistyped", CollectionIIDs{
		Iterable: iidTestPrimary, Iterator: iidTestSecondary,
		VectorView: iidTestPrimary, Vector: iidTestSecondary,
	}, CodecInterface, []any{uintptr(0), "not a pointer"})
}

// TestCorrectPayloadTypeIsUnaffected: the check must be invisible to correct code.
func TestCorrectPayloadTypeIsUnaffected(t *testing.T) {
	vector := NewVectorObject("Test.WellTyped", CollectionIIDs{
		Iterable: iidTestPrimary, Iterator: iidTestSecondary,
		VectorView: iidTestPrimary, Vector: iidTestSecondary,
	}, CodecInterface, []any{uintptr(0), uintptr(0)})
	if vector == nil {
		t.Fatal("NewVectorObject returned nil for well-typed elements")
	}
	vector.Release()
}

// TestInParamReturnsAWritablePointer. InParam is a conversion and nothing else, so what
// is worth asserting is that a write through it lands where the address said.
func TestInParamReturnsAWritablePointer(t *testing.T) {
	// A native out-parameter is an address the caller owns. A heap variable stands in
	// for it here: the point is the conversion, not where the memory came from.
	destination := new(float64)
	address := uintptr(OutParam(unsafePointerOf(destination)))

	*(*float64)(InParam(address)) = 42.5

	if *destination != 42.5 {
		t.Errorf("wrote through InParam and the destination holds %v, want 42.5", *destination)
	}
}
