//go:build windows && (amd64 || arm64)

package acceptance

import (
	"errors"
	"slices"
	"syscall"
	"testing"
	"unsafe"

	win32 "github.com/deploymenttheory/go-bindings-win32/bindings/runtime/win32"
	syswinrt "github.com/deploymenttheory/go-bindings-win32/bindings/win32/system/winrt"

	"github.com/deploymenttheory/go-bindings-winrt/bindings/runtime/winrt"
	"github.com/deploymenttheory/go-bindings-winrt/bindings/winrt/applicationmodel/datatransfer"
	"github.com/deploymenttheory/go-bindings-winrt/bindings/winrt/devices/printers"
	"github.com/deploymenttheory/go-bindings-winrt/bindings/winrt/foundation"
	"github.com/deploymenttheory/go-bindings-winrt/bindings/winrt/globalization"
	"github.com/deploymenttheory/go-bindings-winrt/bindings/winrt/storage"
)

// The element-generic collection runtime's live proofs: generated
// per-instantiation constructors (New<IIterableOfX> et al.) build
// Go-implemented collections whose elements are interface pointers, scalars,
// and strings, and REAL OS code consumes them — retaining elements,
// enumerating asynchronously, and driving the writable IVector surface.

const hresultEBounds = int32(-2147483637) // 0x8000000B E_BOUNDS

// TestIterableOfUriLive drives interface-pointer elements end to end: two
// OS-created Uri objects ride a Go-implemented IIterable<Uri> into
// IppAttributeValueStatics.CreateUriArray, and the OS's own vector hands
// them back with the AbsoluteUri values intact.
func TestIterableOfUriLive(t *testing.T) {
	source := []string{"https://example.com/first", "https://example.com/second"}
	var uris []*foundation.IUriRuntimeClass
	for _, s := range source {
		uri, err := foundation.CreateUri(s)
		if err != nil {
			t.Fatalf("CreateUri(%s): %v", s, err)
		}
		defer uri.Release()
		uris = append(uris, &uri.IUriRuntimeClass)
	}

	iterable := printers.NewIIterableOfUri(uris)
	statics, err := printers.IppAttributeValueStatics()
	if err != nil {
		t.Fatalf("IppAttributeValueStatics: %v", err)
	}
	defer statics.Release()
	value, err := statics.CreateUriArray(iterable)
	if err != nil {
		t.Fatalf("CreateUriArray: %v", err)
	}
	if value == nil {
		t.Fatal("CreateUriArray returned nil")
	}
	defer value.Release()
	// The OS has copied the elements out of our iterable; our reference is
	// the last (a leak here means the OS kept a collection reference).
	if refs := iterable.Release(); refs != 0 {
		t.Errorf("iterable refs after CreateUriArray + release = %d, want 0", refs)
	}

	got, err := value.GetUriArray()
	if err != nil {
		t.Fatalf("GetUriArray: %v", err)
	}
	defer got.Release()
	size, err := got.Size()
	if err != nil {
		t.Fatalf("Size: %v", err)
	}
	if int(size) != len(source) {
		t.Fatalf("OS vector size = %d, want %d", size, len(source))
	}
	var roundTripped []string
	for i := range size {
		uri, err := got.GetAt(i)
		if err != nil {
			t.Fatalf("GetAt(%d): %v", i, err)
		}
		absolute, err := uri.AbsoluteUri()
		uri.Release()
		if err != nil {
			t.Fatalf("AbsoluteUri(%d): %v", i, err)
		}
		roundTripped = append(roundTripped, absolute)
	}
	if !slices.Equal(roundTripped, source) {
		t.Errorf("round trip = %q, want %q", roundTripped, source)
	}
	t.Logf("OS round-tripped %d URIs through a Go-implemented IIterable<Uri>", size)
}

// TestIterableOfInt32Live proves the scalar codec live:
// CreateIntegerArray consumes a Go-implemented IIterable<Int32> (4-byte
// elements crossing get_Current/GetMany slots) and the OS hands the values
// back in order.
func TestIterableOfInt32Live(t *testing.T) {
	source := []int32{3, -1, 41, 0}
	iterable := printers.NewIIterableOfInt32(source)
	statics, err := printers.IppAttributeValueStatics()
	if err != nil {
		t.Fatalf("IppAttributeValueStatics: %v", err)
	}
	defer statics.Release()
	value, err := statics.CreateIntegerArray(iterable)
	if err != nil {
		t.Fatalf("CreateIntegerArray: %v", err)
	}
	defer value.Release()
	if refs := iterable.Release(); refs != 0 {
		t.Errorf("iterable refs after CreateIntegerArray + release = %d, want 0", refs)
	}

	got, err := value.GetIntegerArray()
	if err != nil {
		t.Fatalf("GetIntegerArray: %v", err)
	}
	defer got.Release()
	size, err := got.Size()
	if err != nil {
		t.Fatalf("Size: %v", err)
	}
	var roundTripped []int32
	for i := range size {
		n, err := got.GetAt(i)
		if err != nil {
			t.Fatalf("GetAt(%d): %v", i, err)
		}
		roundTripped = append(roundTripped, n)
	}
	if !slices.Equal(roundTripped, source) {
		t.Errorf("round trip = %v, want %v", roundTripped, source)
	}
}

// TestDataPackageStorageItemsLive is the flagship interface-element proof:
// two real StorageFiles ride a Go-implemented IIterable<IStorageItem> into
// DataPackage.SetStorageItems, and GetStorageItemsAsync().Await() — the OS
// retaining our collection and enumerating it ASYNCHRONOUSLY — returns a
// view whose item names match.
func TestDataPackageStorageItemsLive(t *testing.T) {
	statics, err := storage.StorageFileStatics()
	if err != nil {
		t.Fatalf("StorageFileStatics: %v", err)
	}
	defer statics.Release()
	var items []*storage.IStorageItem
	var wantNames []string
	for range 2 {
		path := tempFilePath(t)
		operation, err := statics.GetFileFromPathAsync(path)
		if err != nil {
			t.Fatalf("GetFileFromPathAsync(%s): %v", path, err)
		}
		file, err := operation.Await()
		operation.Release()
		if err != nil {
			t.Fatalf("Await(%s): %v", path, err)
		}
		item, err := winrt.QueryInterface[storage.IStorageItem](unsafe.Pointer(file), &storage.IID_IStorageItem)
		file.Release()
		if err != nil {
			t.Fatalf("QI(IStorageItem): %v", err)
		}
		defer item.Release()
		name, err := item.Name()
		if err != nil {
			t.Fatalf("Name: %v", err)
		}
		items = append(items, item)
		wantNames = append(wantNames, name)
	}

	pkg, err := datatransfer.NewDataPackage()
	if err != nil {
		t.Fatalf("NewDataPackage: %v", err)
	}
	defer pkg.Release()
	iterable := datatransfer.NewIIterableOfIStorageItem(items)
	if err := pkg.SetStorageItems(iterable, true); err != nil {
		t.Fatalf("SetStorageItems: %v", err)
	}
	// SetStorageItems copies the items into the package's own list; our
	// collection reference is the last.
	if refs := iterable.Release(); refs != 0 {
		t.Errorf("iterable refs after SetStorageItems + release = %d, want 0", refs)
	}

	view, err := pkg.GetView()
	if err != nil {
		t.Fatalf("DataPackage.GetView: %v", err)
	}
	defer view.Release()
	operation, err := view.GetStorageItemsAsync()
	if err != nil {
		t.Fatalf("GetStorageItemsAsync: %v", err)
	}
	defer operation.Release()
	returned, err := operation.Await()
	if err != nil {
		t.Fatalf("Await: %v", err)
	}
	defer returned.Release()

	size, err := returned.Size()
	if err != nil {
		t.Fatalf("Size: %v", err)
	}
	if int(size) != len(wantNames) {
		t.Fatalf("returned view size = %d, want %d", size, len(wantNames))
	}
	var gotNames []string
	for i := range size {
		item, err := returned.GetAt(i)
		if err != nil {
			t.Fatalf("GetAt(%d): %v", i, err)
		}
		name, err := item.Name()
		item.Release()
		if err != nil {
			t.Fatalf("Name(%d): %v", i, err)
		}
		gotNames = append(gotNames, name)
	}
	slices.Sort(gotNames)
	slices.Sort(wantNames)
	if !slices.Equal(gotNames, wantNames) {
		t.Errorf("returned names = %q, want %q", gotNames, wantNames)
	}
	t.Logf("DataPackage returned %d storage items from a Go-implemented IIterable<IStorageItem>", size)
}

// expectBounds asserts err unwraps to E_BOUNDS.
func expectBounds(t *testing.T, err error, context string) {
	t.Helper()
	var hr win32.HRESULT
	if !errors.As(err, &hr) || int32(hr) != hresultEBounds {
		t.Fatalf("%s = %v, want E_BOUNDS", context, err)
	}
}

// TestWritableVectorOfStringLive drives a generated Go-implemented
// IVector<String> through the GENERATED consumer type across all 12 vtable
// slots. GetMany (slot 16) and ReplaceAll (slot 17) take conformant arrays
// the generator does not lower on consumer types, so those two slots are
// driven raw — exactly the ABI shape native callers use.
func TestWritableVectorOfStringLive(t *testing.T) {
	vector := globalization.NewIVectorOfString([]string{"alpha", "beta", "gamma"})

	// GetAt (6) / Size (7) / bounds.
	if got, err := vector.GetAt(1); err != nil || got != "beta" {
		t.Fatalf("GetAt(1) = %q, %v", got, err)
	}
	_, err := vector.GetAt(3)
	expectBounds(t, err, "GetAt(3)")
	if size, err := vector.Size(); err != nil || size != 3 {
		t.Fatalf("Size = %d, %v", size, err)
	}

	// IndexOf (9): value equality, hit and miss.
	var index uint32
	if found, err := vector.IndexOf("gamma", &index); err != nil || !found || index != 2 {
		t.Fatalf("IndexOf(gamma) = %v at %d, %v", found, index, err)
	}
	if found, err := vector.IndexOf("delta", &index); err != nil || found {
		t.Fatalf("IndexOf(delta) = %v, %v; want not found", found, err)
	}

	// GetView (8) BEFORE mutating: the snapshot must not see what follows.
	view, err := vector.GetView()
	if err != nil {
		t.Fatalf("GetView: %v", err)
	}

	// SetAt (10) displaces; InsertAt (11) at the middle and the end;
	// bounds on both.
	if err := vector.SetAt(0, "ALPHA"); err != nil {
		t.Fatalf("SetAt: %v", err)
	}
	expectBounds(t, vector.SetAt(3, "x"), "SetAt(3)")
	if err := vector.InsertAt(1, "inserted"); err != nil {
		t.Fatalf("InsertAt(1): %v", err)
	}
	if err := vector.InsertAt(4, "atEnd"); err != nil {
		t.Fatalf("InsertAt(len): %v", err)
	}
	expectBounds(t, vector.InsertAt(6, "beyond"), "InsertAt(len+1)")
	// Now: ALPHA, inserted, beta, gamma, atEnd.

	// RemoveAt (12) + Append (13).
	if err := vector.RemoveAt(1); err != nil {
		t.Fatalf("RemoveAt(1): %v", err)
	}
	expectBounds(t, vector.RemoveAt(9), "RemoveAt(9)")
	if err := vector.Append("appended"); err != nil {
		t.Fatalf("Append: %v", err)
	}
	// Now: ALPHA, beta, gamma, atEnd, appended.
	if size, err := vector.Size(); err != nil || size != 5 {
		t.Fatalf("Size = %d, %v; want 5", size, err)
	}

	// GetMany (16, raw ABI): partial window from index 3.
	buffer := make([]syswinrt.HSTRING, 4)
	var actual uint32
	r1, _, _ := syscall.SyscallN(vector.LpVtbl[16], uintptr(unsafe.Pointer(vector)),
		3, 4, uintptr(unsafe.Pointer(&buffer[0])), uintptr(unsafe.Pointer(&actual)))
	if err := win32.ErrIfFailed(int32(r1)); err != nil {
		t.Fatalf("raw GetMany: %v", err)
	}
	if actual != 2 {
		t.Fatalf("GetMany actual = %d, want 2", actual)
	}
	if got := winrt.TakeHString(buffer[0]); got != "atEnd" {
		t.Errorf("GetMany[0] = %q, want atEnd", got)
	}
	if got := winrt.TakeHString(buffer[1]); got != "appended" {
		t.Errorf("GetMany[1] = %q, want appended", got)
	}

	// ReplaceAll (17, raw ABI): wholesale swap from a borrowed input array.
	replacement := []string{"one", "two"}
	handles := make([]syswinrt.HSTRING, len(replacement))
	for i, s := range replacement {
		h, err := winrt.NewHString(s)
		if err != nil {
			t.Fatal(err)
		}
		defer h.Close()
		handles[i] = h.Raw()
	}
	r1, _, _ = syscall.SyscallN(vector.LpVtbl[17], uintptr(unsafe.Pointer(vector)),
		uintptr(len(handles)), uintptr(unsafe.Pointer(&handles[0])))
	if err := win32.ErrIfFailed(int32(r1)); err != nil {
		t.Fatalf("raw ReplaceAll: %v", err)
	}
	if got, err := vector.GetAt(0); err != nil || got != "one" {
		t.Fatalf("GetAt(0) after ReplaceAll = %q, %v", got, err)
	}
	if size, err := vector.Size(); err != nil || size != 2 {
		t.Fatalf("Size after ReplaceAll = %d, %v; want 2", size, err)
	}

	// RemoveAtEnd (14) to empty, then E_BOUNDS; Clear (15) is legal on empty.
	if err := vector.RemoveAtEnd(); err != nil {
		t.Fatalf("RemoveAtEnd: %v", err)
	}
	if err := vector.RemoveAtEnd(); err != nil {
		t.Fatalf("RemoveAtEnd: %v", err)
	}
	expectBounds(t, vector.RemoveAtEnd(), "RemoveAtEnd(empty)")
	if err := vector.Clear(); err != nil {
		t.Fatalf("Clear: %v", err)
	}

	// The snapshot taken before ALL the mutation still reads the original
	// three elements — GetView is a snapshot, documented.
	if size, err := view.Size(); err != nil || size != 3 {
		t.Fatalf("snapshot Size = %d, %v; want the original 3", size, err)
	}
	if got, err := view.GetAt(0); err != nil || got != "alpha" {
		t.Fatalf("snapshot GetAt(0) = %q, %v; want the pre-mutation alpha", got, err)
	}
	// Iterate the snapshot through its IIterable tear-off facet.
	viewIterable, err := winrt.QueryInterface[globalization.IIterableOfString](
		unsafe.Pointer(view), &globalization.IID_IIterableOfString)
	if err != nil {
		t.Fatalf("QI(IIterable<String>) on the snapshot: %v", err)
	}
	iterator, err := viewIterable.First()
	if err != nil {
		t.Fatalf("snapshot First: %v", err)
	}
	var snapshot []string
	for {
		has, err := iterator.HasCurrent()
		if err != nil || !has {
			break
		}
		current, err := iterator.Current()
		if err != nil {
			t.Fatalf("snapshot Current: %v", err)
		}
		snapshot = append(snapshot, current)
		if _, err := iterator.MoveNext(); err != nil {
			t.Fatalf("snapshot MoveNext: %v", err)
		}
	}
	if !slices.Equal(snapshot, []string{"alpha", "beta", "gamma"}) {
		t.Errorf("snapshot iteration = %q, want the original contents", snapshot)
	}

	// Both objects drain to zero: the OS-side/native consumers hold nothing.
	if refs := iterator.Release(); refs != 0 {
		t.Errorf("iterator refs after release = %d, want 0", refs)
	}
	viewIterable.Release() // the QI reference (shared count with the view)
	if refs := view.Release(); refs != 0 {
		t.Errorf("view refs after release = %d, want 0", refs)
	}
	if refs := vector.Release(); refs != 0 {
		t.Errorf("vector refs after release = %d, want 0", refs)
	}
}
