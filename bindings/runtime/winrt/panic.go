//go:build windows && amd64

package winrt

// Containing panics at the callback boundary.
//
// A Go body reached from native code — a delegate's Invoke, an implemented interface
// method, a collection's GetAt — cannot let a panic escape. There are Go frames beneath
// it and then NATIVE frames beneath those, and Go's unwinder cannot walk past the
// native ones. What happens instead is that the runtime declares the panic unrecovered
// and terminates the process, from inside a COM call, with the calling thread holding
// whatever locks the framework had taken.
//
// The observable result is not a crash report. It is a HANG: the panic trace may or may
// not reach the console before the apartment stops responding, and a caller waiting on
// the call simply waits. That is a bad trade for what is usually a small Go mistake —
// the case this was written for was a one-word type error in a collection's element
// payload, which presented as a 45-second timeout with no output at all.
//
// So every boundary recovers, converts the panic to a failing HRESULT, and reports it
// once on stderr. The call fails, the framework sees an error it is built to handle, and
// the process survives to say what happened.
//
// This is deliberately NOT a general error-handling mechanism. A body that wants to fail
// should return an HRESULT. Recovery here is for programming errors that would otherwise
// be untraceable, and the report is written so it reads as one.

import (
	"fmt"
	"os"
	"runtime/debug"
)

// eUnexpected is what a recovered body returns. E_UNEXPECTED rather than E_FAIL so a
// caller reading the HRESULT can tell "this implementation is broken" from "this
// implementation declined", which is what E_FAIL means everywhere else in this package.
const eUnexpected = uintptr(0x8000FFFF) // E_UNEXPECTED

// panicReporter receives the formatted report. Package-level so a test can capture it
// instead of writing to stderr; nil means stderr.
var panicReporter func(string)

// containPanic converts a panic in the body it is deferred from into eUnexpected.
//
// Used as:
//
//	func someCallback(args []uintptr) (result uintptr) {
//		defer containPanic("a delegate body (Invoke)", noSlot, &result)
//		...
//	}
//
// The result pointer is how the HRESULT is substituted: a deferred function cannot
// change a non-named return value, and naming it at each call site is clearer than
// wrapping every body in a closure.
//
// EVERY ARGUMENT MUST BE CHEAP TO EVALUATE, and that is not a style preference.
// Deferred arguments are evaluated when the defer STATEMENT runs — on every call, not
// only the panicking ones — and this sits on the path a native callback reenters Go
// through. Allocating there grows the callback goroutine's stack, morestack copies the
// stack, and a native frame beneath is left holding a pointer into the old one. That is
// the failure collections_test.go's callSlot documents at length, and the first version
// of this function caused it directly: it took a preformatted string, so every call ran
// a fmt.Sprintf whether it panicked or not.
//
// The slot therefore arrives as an int, and formatting happens inside — on the path
// that has already panicked, where there is nothing left to protect.
func containPanic(what string, slot int, result *uintptr) {
	recovered := recover()
	if recovered == nil {
		return
	}
	*result = eUnexpected

	where := what
	if slot != noSlot {
		where = fmt.Sprintf("%s at vtable slot %d", what, slot)
	}
	report(fmt.Sprintf(
		"winrt: PANIC in %s, recovered at the native callback boundary.\n"+
			"The call returns E_UNEXPECTED; the process continues. This is a bug in the Go\n"+
			"body, not in the caller — a panic here would otherwise terminate the process\n"+
			"from inside a COM call, which presents as a hang rather than a crash.\n"+
			"  panic: %v\n%s", where, recovered, debug.Stack()))
}

// noSlot is the slot argument for a boundary that is not a vtable slot.
const noSlot = -1

func report(message string) {
	if panicReporter != nil {
		panicReporter(message)
		return
	}
	fmt.Fprintln(os.Stderr, message)
}
