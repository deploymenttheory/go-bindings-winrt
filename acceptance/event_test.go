//go:build windows && (amd64 || arm64)

package acceptance

import (
	"sync/atomic"
	"testing"
	"time"
	"unsafe"

	syswinrt "github.com/deploymenttheory/go-bindings-win32/bindings/win32/system/winrt"

	"github.com/deploymenttheory/go-bindings-winrt/bindings/runtime/winrt"
	"github.com/deploymenttheory/go-bindings-winrt/bindings/winrt/foundation"
)

// The generated event surface's live proof, end to end on generated code:
// IMemoryBufferReference.Closed uses a generic delegate instantiation
// (TypedEventHandler`2<IMemoryBufferReference, Object>), so this exercises
// the emitted Add<Event>/Remove<Event> accessors, the monomorphized handler
// type with its pinterface-derived IID (the OS only QIs our delegate for an
// IID it derived by the same algorithm), and the raw-word adapter converting
// the ABI arguments to typed Go values.

// newMemoryBufferReference builds a real IMemoryBufferReference. Factory
// constructors are not yet projected onto runtime classes, so the MemoryBuffer
// activation factory is fetched by hand — but Create and everything after it
// is generated code.
func newMemoryBufferReference(t *testing.T) (*foundation.IMemoryBuffer, *foundation.IMemoryBufferReference) {
	t.Helper()
	factoryUnknown, err := winrt.GetActivationFactory("Windows.Foundation.MemoryBuffer", &foundation.IID_IMemoryBufferFactory)
	if err != nil {
		t.Fatalf("GetActivationFactory(MemoryBuffer): %v", err)
	}
	// GetActivationFactory already queried for IID_IMemoryBufferFactory, so
	// the returned pointer IS the factory interface (every generated
	// interface struct is a single vtable-pointer word).
	factory := (*foundation.IMemoryBufferFactory)(unsafe.Pointer(factoryUnknown))
	defer factory.Release()

	buffer, err := factory.Create(64)
	if err != nil {
		t.Fatalf("IMemoryBufferFactory.Create: %v", err)
	}
	reference, err := buffer.CreateReference()
	if err != nil {
		buffer.Release()
		t.Fatalf("IMemoryBuffer.CreateReference: %v", err)
	}
	return buffer, reference
}

// closeReference queries the reference's IClosable and closes it, which per
// the WinRT contract raises Closed synchronously.
func closeReference(t *testing.T, reference *foundation.IMemoryBufferReference) {
	t.Helper()
	closable, err := winrt.QueryInterface[foundation.IClosable](unsafe.Pointer(reference), &foundation.IID_IClosable)
	if err != nil {
		t.Fatalf("QueryInterface(IClosable): %v", err)
	}
	defer closable.Release()
	if err := closable.Close(); err != nil {
		t.Fatalf("IClosable.Close: %v", err)
	}
}

func TestEventClosedFires(t *testing.T) {
	buffer, reference := newMemoryBufferReference(t)
	defer buffer.Release()
	defer reference.Release()

	fired := make(chan bool, 1)
	handler, err := foundation.NewTypedEventHandlerOfIMemoryBufferReferenceAndObject(
		func(sender *foundation.IMemoryBufferReference, args *syswinrt.IInspectable) {
			// sender/args are borrowed; only their nilness is inspected.
			fired <- sender != nil
		})
	if err != nil {
		t.Fatalf("NewTypedEventHandlerOfIMemoryBufferReferenceAndObject: %v", err)
	}
	defer handler.Close()

	token, err := reference.AddClosed(handler)
	if err != nil {
		t.Fatalf("AddClosed: %v", err)
	}
	if token.Value == 0 {
		t.Error("registration token is zero")
	}

	closeReference(t, reference)

	// Closed is raised synchronously during Close per the WinRT contract;
	// the timeout only guards against a contract violation hanging the test.
	select {
	case senderOK := <-fired:
		if !senderOK {
			t.Error("Closed handler invoked with nil sender")
		}
	case <-time.After(30 * time.Second):
		t.Fatal("Closed event did not fire")
	}
}

func TestEventRemovedHandlerDoesNotFire(t *testing.T) {
	buffer, reference := newMemoryBufferReference(t)
	defer buffer.Release()
	defer reference.Release()

	var invoked atomic.Bool
	handler, err := foundation.NewTypedEventHandlerOfIMemoryBufferReferenceAndObject(
		func(sender *foundation.IMemoryBufferReference, args *syswinrt.IInspectable) {
			invoked.Store(true)
		})
	if err != nil {
		t.Fatalf("NewTypedEventHandlerOfIMemoryBufferReferenceAndObject: %v", err)
	}
	defer handler.Close()

	token, err := reference.AddClosed(handler)
	if err != nil {
		t.Fatalf("AddClosed: %v", err)
	}
	if err := reference.RemoveClosed(token); err != nil {
		t.Fatalf("RemoveClosed: %v", err)
	}

	closeReference(t, reference)

	// Closed fires synchronously during Close, so by now a still-registered
	// handler would have run.
	if invoked.Load() {
		t.Error("removed handler was invoked by Close")
	}
}
