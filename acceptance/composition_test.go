//go:build windows && (amd64 || arm64)

package acceptance

import (
	"errors"
	"testing"

	win32 "github.com/deploymenttheory/go-bindings-win32/bindings/runtime/win32"

	"github.com/deploymenttheory/go-bindings-winrt/bindings/winrt/ui/windowmanagement"
	controls "github.com/deploymenttheory/go-bindings-winrt/bindings/winrt/ui/xaml/controls"
)

// Composable classes' live proof (instantiate-only composition; Go-side
// derivation stays out of scope):
//
//   - the DIRECT path: composable classes marked activatable_direct
//     instantiate through RoActivateInstance, which composes null-outer
//     internally — FullScreenPresentationConfiguration is a data-only
//     composable with the best odds in a bare unpackaged process, and its
//     IsExclusive round-trip proves a real method call on the instance;
//   - the FACTORY path: the generated composable constructor (NewButton →
//     IButtonFactory.CreateInstance with a NULL outer and the inner
//     out-pointer) in a bare process without an initialized XAML runtime
//     MUST fail with a well-formed HRESULT and never crash — the factory
//     fetch, the CreateInstance ABI, and the inner handling are all
//     exercised on the error path in every environment, packaged or not.

// TestFullScreenPresentationConfigurationLive drives a direct-activated
// composable end to end: construct, set, read back. A platform that refuses
// the activation (stripped-down or headless hosts) must refuse with a
// well-formed HRESULT — that shape skips, never silently passes.
func TestFullScreenPresentationConfigurationLive(t *testing.T) {
	config, err := windowmanagement.NewFullScreenPresentationConfiguration()
	if err != nil {
		var hresult win32.HRESULT
		if !errors.As(err, &hresult) {
			t.Fatalf("NewFullScreenPresentationConfiguration error is not a well-formed HRESULT: %v", err)
		}
		t.Skipf("FullScreenPresentationConfiguration activation refused on this host: %v (platform restriction; verified passing on Windows 11 Enterprise)", err)
	}
	if config == nil {
		t.Fatal("NewFullScreenPresentationConfiguration returned a nil instance")
	}
	defer config.Release()

	initial, err := config.IsExclusive()
	if err != nil {
		t.Fatalf("IsExclusive: %v", err)
	}
	if initial {
		t.Error("IsExclusive defaults true, the platform documents false")
	}
	if err := config.SetIsExclusive(true); err != nil {
		t.Fatalf("SetIsExclusive(true): %v", err)
	}
	exclusive, err := config.IsExclusive()
	if err != nil {
		t.Fatalf("IsExclusive after set: %v", err)
	}
	if !exclusive {
		t.Error("IsExclusive round-trip: set true, read false")
	}
	if err := config.SetIsExclusive(false); err != nil {
		t.Fatalf("SetIsExclusive(false): %v", err)
	}
	back, err := config.IsExclusive()
	if err != nil {
		t.Fatalf("IsExclusive after reset: %v", err)
	}
	if back {
		t.Error("IsExclusive round-trip: reset false, read true")
	}
}

// TestButtonComposableCtorErrorShape always runs: a bare unpackaged process
// has no initialized XAML runtime, so NewButton must fail — with a
// well-formed HRESULT (XAML uninitialized / class not registered), never a
// crash. This live-proves the composable-factory path (activation-factory
// fetch, the null-outer CreateInstance ABI including the heap-escaped inner
// out-pointer, and the error-path inner handling). In the unlikely event a
// future host CAN create a Button here, success is tolerated and logged.
func TestButtonComposableCtorErrorShape(t *testing.T) {
	button, err := controls.NewButton()
	if err == nil {
		// Not expected in a bare process, but a hosting environment with an
		// initialized XAML core is not a failure of THIS contract.
		defer button.Release()
		t.Log("NewButton unexpectedly succeeded (XAML-initialized host); the error-shape branch was not exercised")
		return
	}
	var hresult win32.HRESULT
	if !errors.As(err, &hresult) {
		t.Fatalf("NewButton error is not a well-formed HRESULT: %v", err)
	}
	if button != nil {
		t.Errorf("NewButton returned a non-nil instance alongside error %v", err)
	}
	t.Logf("NewButton in a bare unpackaged process: %v (expected: XAML runtime uninitialized)", err)
}
