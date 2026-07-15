//go:build windows && (amd64 || arm64)

package winrt

import (
	"testing"
	"unsafe"
)

// resultWord absorbs the SyscallN-argument words the escape tests produce so
// the compiler cannot elide the pattern under test.
var resultWord uintptr

// TestOutParamForcesHeapEscape pins the out-param invariant at the compiler
// level: a `new(T)` routed through OutParam MUST be heap-allocated (one
// allocation per run), because a stack-resident out-param goes stale when a
// reentrant native call parks this goroutine and the GC shrinks its stack
// (see outparam.go). If a compiler change ever lets this allocation stay on
// the stack, this test fails before the flake does.
func TestOutParamForcesHeapEscape(t *testing.T) {
	allocs := testing.AllocsPerRun(200, func() {
		result := new(int32)
		resultWord = uintptr(OutParam(unsafe.Pointer(result)))
		*result = 7
	})
	if allocs < 1 {
		t.Fatalf("new(int32) through OutParam allocated %.0f times per run, want >= 1 (out-param stayed on the stack)", allocs)
	}
}

// TestHeapNewForcesHeapEscape pins the same invariant for the hand-written
// runtime paths' helper.
func TestHeapNewForcesHeapEscape(t *testing.T) {
	allocs := testing.AllocsPerRun(200, func() {
		out := heapNew[*int32]()
		resultWord = uintptr(unsafe.Pointer(out))
		_ = *out
	})
	if allocs < 1 {
		t.Fatalf("heapNew allocated %.0f times per run, want >= 1 (out-param stayed on the stack)", allocs)
	}
}

// TestOutParamIsIdentity confirms OutParam is a pure pass-through.
func TestOutParamIsIdentity(t *testing.T) {
	result := new(uint64)
	if got := OutParam(unsafe.Pointer(result)); got != unsafe.Pointer(result) {
		t.Fatalf("OutParam(%p) = %p, want identity", result, got)
	}
}
