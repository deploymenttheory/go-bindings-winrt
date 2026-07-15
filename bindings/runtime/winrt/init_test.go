//go:build windows && (amd64 || arm64)

package winrt

import "testing"

func TestInitialize(t *testing.T) {
	if err := Initialize(); err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	// Idempotent: the once-guard returns the same (nil) result.
	if err := Initialize(); err != nil {
		t.Fatalf("second Initialize: %v", err)
	}
}
