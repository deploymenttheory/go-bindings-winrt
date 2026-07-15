//go:build windows && (amd64 || arm64)

package acceptance

import (
	"path/filepath"
	"syscall"
	"testing"
	"unsafe"

	win32 "github.com/deploymenttheory/go-bindings-win32/bindings/runtime/win32"
	syswinrt "github.com/deploymenttheory/go-bindings-win32/bindings/win32/system/winrt"

	"github.com/deploymenttheory/go-bindings-winrt/bindings/runtime/winrt"
	"github.com/deploymenttheory/go-bindings-winrt/bindings/winrt/globalization"
	"github.com/deploymenttheory/go-bindings-winrt/internal/codegen/pinterface"
	"github.com/deploymenttheory/go-bindings-winrt/internal/codegen/pipeline"
	"github.com/deploymenttheory/go-bindings-winrt/internal/winrtmeta"
)

// The pinterface IID algorithm's live proof: derive the IIDs of
// IVectorView<String> and IIterable<String> from the committed IR alone,
// then QueryInterface a REAL parameterized-interface object for them. The
// OS only answers QI for IIDs it derived by the same algorithm, so success
// is end-to-end confirmation. The object comes from ICalendar.get_Languages
// (vtable slot 9 — the very member the generator skips today).
func TestPinterfaceIIDsLive(t *testing.T) {
	registry, err := pipeline.LoadAll(filepath.Join("..", "metadata", "winrt"))
	if err != nil {
		t.Fatalf("loading committed IR: %v", err)
	}
	hstringArg := winrtmeta.TypeRef{Kind: "Native", Name: "HString"}
	iidOf := func(name string) win32.GUID {
		t.Helper()
		inst := &winrtmeta.TypeRef{
			Kind: "GenericInst", Namespace: "Windows.Foundation.Collections",
			Name: name, Args: []winrtmeta.TypeRef{hstringArg},
		}
		iid, err := pinterface.InstanceIID(inst, registry)
		if err != nil {
			t.Fatalf("InstanceIID(%s<String>): %v", name, err)
		}
		guid, err := winrt.ParseGUID(iid)
		if err != nil {
			t.Fatalf("ParseGUID(%s): %v", iid, err)
		}
		return guid
	}
	iidVectorView := iidOf("IVectorView`1")
	iidIterable := iidOf("IIterable`1")

	calendar, err := globalization.NewCalendar()
	if err != nil {
		t.Fatalf("NewCalendar: %v", err)
	}
	defer calendar.Release()

	// get_Languages (ICalendar slot 9) returns IVectorView<String>.
	var languages *syswinrt.IInspectable
	r1, _, _ := syscall.SyscallN(calendar.LpVtbl[9], uintptr(unsafe.Pointer(calendar)), uintptr(unsafe.Pointer(&languages)))
	if err := win32.ErrIfFailed(int32(r1)); err != nil {
		t.Fatalf("get_Languages: %v", err)
	}
	defer languages.Release()

	// QI for the derived IVectorView<String> IID.
	vectorView, err := winrt.QueryInterface[syswinrt.IInspectable](unsafe.Pointer(languages), &iidVectorView)
	if err != nil {
		t.Fatalf("QI(IVectorView<String> %s): %v — pinterface IID algorithm wrong", iidVectorView, err)
	}
	defer vectorView.Release()

	// QI for the derived IIterable<String> IID.
	iterable, err := winrt.QueryInterface[syswinrt.IInspectable](unsafe.Pointer(languages), &iidIterable)
	if err != nil {
		t.Fatalf("QI(IIterable<String> %s): %v — pinterface IID algorithm wrong", iidIterable, err)
	}
	iterable.Release()

	// Drive the typed interface: IVectorView<T> slots are GetAt(6),
	// get_Size(7). The system always reports at least one language.
	var size uint32
	r1, _, _ = syscall.SyscallN(vectorView.LpVtbl[7], uintptr(unsafe.Pointer(vectorView)), uintptr(unsafe.Pointer(&size)))
	if err := win32.ErrIfFailed(int32(r1)); err != nil {
		t.Fatalf("IVectorView.get_Size: %v", err)
	}
	if size == 0 {
		t.Fatal("Languages vector is empty")
	}
	var first syswinrt.HSTRING
	r1, _, _ = syscall.SyscallN(vectorView.LpVtbl[6], uintptr(unsafe.Pointer(vectorView)), uintptr(0), uintptr(unsafe.Pointer(&first)))
	if err := win32.ErrIfFailed(int32(r1)); err != nil {
		t.Fatalf("IVectorView.GetAt(0): %v", err)
	}
	language := winrt.TakeHString(first)
	if language == "" {
		t.Fatal("first language is empty")
	}
	t.Logf("languages: %d entries, first = %s (IVectorView<String> = %s)", size, language, iidVectorView)
}
