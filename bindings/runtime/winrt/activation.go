//go:build windows && (amd64 || arm64)

package winrt

import (
	"fmt"

	win32 "github.com/deploymenttheory/go-bindings-win32/bindings/runtime/win32"
	syswinrt "github.com/deploymenttheory/go-bindings-win32/bindings/win32/system/winrt"
)

// ActivateInstance activates a runtime class through its default
// constructor and returns the new instance's IInspectable. The caller owns
// the reference (Release when done, typically via the interface the
// instance is queried to).
func ActivateInstance(className string) (*syswinrt.IInspectable, error) {
	if err := Initialize(); err != nil {
		return nil, err
	}
	h, err := NewHString(className)
	if err != nil {
		return nil, err
	}
	defer h.Close()
	var instance *syswinrt.IInspectable
	if err := syswinrt.RoActivateInstance(h.Raw(), &instance); err != nil {
		return nil, fmt.Errorf("winrt: activating %s: %w", className, err)
	}
	if instance == nil {
		return nil, fmt.Errorf("winrt: activating %s: null instance", className)
	}
	return instance, nil
}

// GetActivationFactory returns the activation factory for a runtime class,
// queried to the given factory interface IID. The caller owns the reference.
func GetActivationFactory(className string, iid *win32.GUID) (*win32.IUnknown, error) {
	if err := Initialize(); err != nil {
		return nil, err
	}
	h, err := NewHString(className)
	if err != nil {
		return nil, err
	}
	defer h.Close()
	var factory *win32.IUnknown
	if err := syswinrt.RoGetActivationFactory(h.Raw(), iid, &factory); err != nil {
		return nil, fmt.Errorf("winrt: getting activation factory for %s: %w", className, err)
	}
	if factory == nil {
		return nil, fmt.Errorf("winrt: getting activation factory for %s: null factory", className)
	}
	return factory, nil
}
