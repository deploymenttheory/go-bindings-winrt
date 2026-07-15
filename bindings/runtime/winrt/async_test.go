//go:build windows && (amd64 || arm64)

package winrt

import (
	"errors"
	"strings"
	"testing"

	win32 "github.com/deploymenttheory/go-bindings-win32/bindings/runtime/win32"
)

// TestAsyncErrorCarriesHRESULT pins the contract generated Await bodies
// depend on: the error names the status and wraps the IAsyncInfo error code
// so errors.As reaches the underlying HRESULT.
func TestAsyncErrorCarriesHRESULT(t *testing.T) {
	const fileNotFound = int32(-2147024894) // 0x80070002
	err := AsyncError(3, fileNotFound)
	if err == nil {
		t.Fatal("AsyncError returned nil")
	}
	var hr win32.HRESULT
	if !errors.As(err, &hr) {
		t.Fatalf("errors.As(win32.HRESULT) failed on %v", err)
	}
	if int32(hr) != fileNotFound {
		t.Errorf("wrapped HRESULT = 0x%08x, want 0x80070002", uint32(hr))
	}
	if !strings.Contains(err.Error(), "Error") {
		t.Errorf("error text does not name the status: %v", err)
	}
}

// TestAsyncErrorStatusNames covers the status naming, including values
// outside the enum.
func TestAsyncErrorStatusNames(t *testing.T) {
	for status, want := range map[int32]string{
		0: "Started", 1: "Completed", 2: "Canceled", 3: "Error", 7: "AsyncStatus(7)",
	} {
		if got := AsyncError(status, -1).Error(); !strings.Contains(got, want) {
			t.Errorf("AsyncError(%d, ...) = %q, want it to contain %q", status, got, want)
		}
	}
}
