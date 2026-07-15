//go:build windows && (amd64 || arm64)

package acceptance

import (
	"syscall"
	"testing"
	"unsafe"

	win32 "github.com/deploymenttheory/go-bindings-win32/bindings/runtime/win32"
	syswinrt "github.com/deploymenttheory/go-bindings-win32/bindings/win32/system/winrt"

	"github.com/deploymenttheory/go-bindings-winrt/bindings/runtime/winrt"
)

// The delegate runtime's live proof: register a Go-implemented delegate
// with a real WinRT event source and unregister it. MediaProtectionManager
// is directly activatable with no hardware dependency, and its events use
// non-generic delegates. The hand-declared minimal binding below (IID +
// slots pinned from the committed winmd IR) stands in until event emission
// lands in the generator.
//
// Registration exercises the full delegate contract: the runtime QIs the
// handler (for the delegate IID / IAgileObject) and AddRefs it; removal
// releases it.

// iMediaProtectionManager: Windows.Media.Protection.IMediaProtectionManager.
type iMediaProtectionManager struct {
	syswinrt.IInspectable
}

var (
	iidMediaProtectionManager = winrt.MustGUID("45694947-c741-434b-a79e-474c12d93d2f")
	iidRebootNeededHandler    = winrt.MustGUID("64e12a45-973b-4a3a-b260-91898a49a82c")
)

// AddRebootNeeded dispatches through IMediaProtectionManager's vtable slot 8.
func (self *iMediaProtectionManager) AddRebootNeeded(handler uintptr) (syswinrt.EventRegistrationToken, error) {
	var token syswinrt.EventRegistrationToken
	r1, _, _ := syscall.SyscallN(self.LpVtbl[8], uintptr(unsafe.Pointer(self)), handler, uintptr(unsafe.Pointer(&token)))
	return token, win32.ErrIfFailed(int32(r1))
}

// RemoveRebootNeeded dispatches through IMediaProtectionManager's vtable
// slot 9 (EventRegistrationToken is one word by value).
func (self *iMediaProtectionManager) RemoveRebootNeeded(token syswinrt.EventRegistrationToken) error {
	r1, _, _ := syscall.SyscallN(self.LpVtbl[9], uintptr(unsafe.Pointer(self)), uintptr(token.Value))
	return win32.ErrIfFailed(int32(r1))
}

func TestDelegateEventRegistration(t *testing.T) {
	instance, err := winrt.ActivateInstance("Windows.Media.Protection.MediaProtectionManager")
	if err != nil {
		t.Fatalf("activating MediaProtectionManager: %v", err)
	}
	defer instance.Release()
	manager, err := winrt.QueryInterface[iMediaProtectionManager](unsafe.Pointer(instance), &iidMediaProtectionManager)
	if err != nil {
		t.Fatalf("QueryInterface: %v", err)
	}
	defer manager.Release()

	handler, err := winrt.NewDelegate(iidRebootNeededHandler, 1, func(args []uintptr) uintptr {
		return 0 // never fires in this test; registration is the proof
	})
	if err != nil {
		t.Fatalf("NewDelegate: %v", err)
	}

	token, err := manager.AddRebootNeeded(handler.Ptr())
	if err != nil {
		t.Fatalf("add_RebootNeeded: %v", err)
	}
	if token.Value == 0 {
		t.Error("registration token is zero")
	}

	if err := manager.RemoveRebootNeeded(token); err != nil {
		t.Fatalf("remove_RebootNeeded: %v", err)
	}
	// After removal the runtime has dropped its references; ours is last.
	if refs := handler.Release(); refs != 0 {
		t.Errorf("delegate refs after remove + release = %d, want 0 (runtime leaked a reference)", refs)
	}
}
