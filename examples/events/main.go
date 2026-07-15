//go:build windows && (amd64 || arm64)

// Command events subscribes a Go function to a WinRT event and watches it
// fire: IMemoryBufferReference.Closed, whose delegate is the generic
// TypedEventHandler<IMemoryBufferReference, Object> monomorphized into the
// foundation package with a typed constructor.
//
// The flow every WinRT event follows here:
//
//  1. Build a typed handler with the generated New<Handler> constructor.
//  2. Register it: AddClosed(handler) returns an EventRegistrationToken.
//  3. When done, RemoveClosed(token), then handler.Close() once no native
//     code can still invoke it.
//
// Handler callbacks run on a fresh goroutine per invocation — never on the
// goroutine that registered them; the pointer arguments they receive are
// BORROWED for the callback's duration — never Release or retain them.
package main

import (
	"fmt"
	"log"
	"unsafe"

	syswinrt "github.com/deploymenttheory/go-bindings-win32/bindings/win32/system/winrt"

	"github.com/deploymenttheory/go-bindings-winrt/bindings/runtime/winrt"
	"github.com/deploymenttheory/go-bindings-winrt/bindings/winrt/foundation"
)

func main() {
	// A MemoryBuffer through its factory constructor, and a reference into
	// it — the object whose Closed event we subscribe to.
	buffer, err := foundation.Create(64)
	if err != nil {
		log.Fatalf("MemoryBuffer Create: %v", err)
	}
	defer buffer.Release()
	reference, err := buffer.CreateReference()
	if err != nil {
		log.Fatalf("CreateReference: %v", err)
	}
	defer reference.Release()

	// 1. The typed handler. The callback's arguments are typed by the
	// generated adapter (sender is the reference that closed); they are
	// borrowed — valid only until the callback returns.
	fired := make(chan struct{})
	handler, err := foundation.NewTypedEventHandlerOfIMemoryBufferReferenceAndObject(
		func(sender *foundation.IMemoryBufferReference, args *syswinrt.IInspectable) {
			fmt.Println("2. Closed handler invoked (on its own goroutine)")
			close(fired)
		})
	if err != nil {
		log.Fatalf("NewTypedEventHandlerOf...: %v", err)
	}
	// 3b. Close the handler when no native code can still invoke it —
	// after RemoveClosed below (deferred here, so it runs last).
	defer handler.Close()

	// 2. Register. The token is what you hand back to Remove<Event>.
	token, err := reference.AddClosed(handler)
	if err != nil {
		log.Fatalf("AddClosed: %v", err)
	}
	fmt.Printf("handler registered (token %d)\n", token.Value)

	// Trigger the event: closing the reference raises Closed synchronously
	// per the WinRT contract. IClosable is another interface of the same
	// object — query for it.
	closable, err := winrt.QueryInterface[foundation.IClosable](unsafe.Pointer(reference), &foundation.IID_IClosable)
	if err != nil {
		log.Fatalf("QueryInterface(IClosable): %v", err)
	}
	defer closable.Release()
	fmt.Println("1. closing the reference (raises Closed)")
	if err := closable.Close(); err != nil {
		log.Fatalf("IClosable.Close: %v", err)
	}

	// Closed is raised synchronously during Close: the native call above did
	// not return until the handler (on its own goroutine) had run, so this
	// receive is immediate. For asynchronously-raised events this channel
	// wait is how you observe the handler from your own goroutines.
	<-fired
	fmt.Println("3. event observed; unregistering")

	// 3a. Unregister with the token.
	if err := reference.RemoveClosed(token); err != nil {
		log.Fatalf("RemoveClosed: %v", err)
	}
	fmt.Println("done (handler.Close runs deferred)")
}
