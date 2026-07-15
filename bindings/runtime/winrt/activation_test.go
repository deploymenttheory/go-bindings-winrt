//go:build windows && (amd64 || arm64)

package winrt

import (
	"testing"

	syswinrt "github.com/deploymenttheory/go-bindings-win32/bindings/win32/system/winrt"
)

// TestActivateInstance activates a real runtime class and reads its class
// name back through IInspectable — the live end-to-end proof for the whole
// runtime layer (Initialize, HSTRING in, activation, HSTRING out).
func TestActivateInstance(t *testing.T) {
	instance, err := ActivateInstance("Windows.Globalization.Calendar")
	if err != nil {
		t.Fatalf("ActivateInstance: %v", err)
	}
	defer instance.Release()

	var name syswinrt.HSTRING
	if err := instance.GetRuntimeClassName(&name); err != nil {
		t.Fatalf("GetRuntimeClassName: %v", err)
	}
	if got := TakeHString(name); got != "Windows.Globalization.Calendar" {
		t.Errorf("runtime class name = %q", got)
	}

	var trust syswinrt.TrustLevel
	if err := instance.GetTrustLevel(&trust); err != nil {
		t.Errorf("GetTrustLevel: %v", err)
	}
}

func TestActivateInstanceUnknownClass(t *testing.T) {
	if _, err := ActivateInstance("Does.Not.Exist"); err == nil {
		t.Fatal("activating an unknown class succeeded")
	}
}
