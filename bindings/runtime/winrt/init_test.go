//go:build windows && amd64

package winrt

import (
	"runtime"
	"testing"
)

func TestInitialize(t *testing.T) {
	if err := Initialize(); err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	// Idempotent on an already-initialized thread: the apartment probe
	// short-circuits, so no second reference is taken.
	if err := Initialize(); err != nil {
		t.Fatalf("second Initialize: %v", err)
	}
	if !threadHasApartment() {
		t.Fatal("thread reports no apartment after Initialize")
	}
}

// TestInitializeOtherThread is the regression test for the process-wide guard
// this replaced: apartment initialization is per-thread, so a sync.Once
// consumed by whichever thread activated first left every later thread
// uninitialized, and that thread's first activation failed with
// CO_E_NOTINITIALIZED.
func TestInitializeOtherThread(t *testing.T) {
	if err := Initialize(); err != nil {
		t.Fatalf("Initialize on the test thread: %v", err)
	}

	result := make(chan error, 1)
	go func() {
		// Lock so the goroutine cannot migrate onto a thread another test
		// already initialized — the point is a thread that has not been.
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		if err := Initialize(); err != nil {
			result <- err
			return
		}
		if !threadHasApartment() {
			t.Error("second thread reports no apartment after Initialize")
		}
		result <- nil
	}()
	if err := <-result; err != nil {
		t.Fatalf("Initialize on a second thread: %v", err)
	}
}
