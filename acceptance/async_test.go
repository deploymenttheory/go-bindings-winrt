//go:build windows && (amd64 || arm64)

package acceptance

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
	"unsafe"

	win32 "github.com/deploymenttheory/go-bindings-win32/bindings/runtime/win32"

	"github.com/deploymenttheory/go-bindings-winrt/bindings/runtime/winrt"
	"github.com/deploymenttheory/go-bindings-winrt/bindings/winrt/foundation"
	"github.com/deploymenttheory/go-bindings-winrt/bindings/winrt/storage"
)

// The generated async surface's live proof: SetCompleted (the first
// delegate-typed method PARAMETER emitted) and the synthesized Await on a
// monomorphized IAsyncOperation — a real WinRT async call awaited from Go.
// GetFileFromPathAsync is hardware-free and in the committed closure, and
// the file's existence is fully under the test's control.

const hresultFileNotFound = int32(-2147024894) // 0x80070002 (WIN32 ERROR_FILE_NOT_FOUND)

// tempFilePath writes a real file for the async storage APIs to find.
func tempFilePath(t *testing.T) string {
	t.Helper()
	file, err := os.CreateTemp(t.TempDir(), "winrt-async-*.txt")
	if err != nil {
		t.Fatalf("os.CreateTemp: %v", err)
	}
	if _, err := file.WriteString("awaited from Go\n"); err != nil {
		file.Close()
		t.Fatalf("writing temp file: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("closing temp file: %v", err)
	}
	return file.Name()
}

// getFileOperation starts a GetFileFromPathAsync operation through the
// generated statics accessor.
func getFileOperation(t *testing.T, path string) *storage.IAsyncOperationOfStorageFile {
	t.Helper()
	statics, err := storage.StorageFileStatics()
	if err != nil {
		t.Fatalf("StorageFileStatics: %v", err)
	}
	defer statics.Release()
	operation, err := statics.GetFileFromPathAsync(path)
	if err != nil {
		t.Fatalf("GetFileFromPathAsync(%s): %v", path, err)
	}
	if operation == nil {
		t.Fatal("GetFileFromPathAsync returned a nil operation")
	}
	t.Cleanup(func() { operation.Release() })
	return operation
}

// fileName reads the awaited StorageFile's Name through IStorageItem.
func fileName(t *testing.T, file *storage.IStorageFile) string {
	t.Helper()
	item, err := winrt.QueryInterface[storage.IStorageItem](unsafe.Pointer(file), &storage.IID_IStorageItem)
	if err != nil {
		t.Fatalf("QueryInterface(IStorageItem): %v", err)
	}
	defer item.Release()
	name, err := item.Name()
	if err != nil {
		t.Fatalf("IStorageItem.Name: %v", err)
	}
	return name
}

// TestAsyncAwaitStorageFileLive awaits a fresh operation end to end: write a
// file from Go, GetFileFromPathAsync, Await, and verify the resulting
// StorageFile names the file we wrote.
func TestAsyncAwaitStorageFileLive(t *testing.T) {
	path := tempFilePath(t)
	operation := getFileOperation(t, path)

	file, err := operation.Await()
	if err != nil {
		t.Fatalf("Await: %v", err)
	}
	if file == nil {
		t.Fatal("Await returned a nil StorageFile")
	}
	defer file.Release()

	if name := fileName(t, file); name != filepath.Base(path) {
		t.Errorf("StorageFile name = %q, want %q", name, filepath.Base(path))
	}
}

// TestAsyncAwaitAlreadyCompletedLive proves the immediate-invoke path: the
// operation is driven to a terminal state (observed through the generated
// IAsyncInfo surface) BEFORE Await assigns the Completed handler — per the
// WinRT contract the handler must then be invoked immediately, so Await
// returns instead of hanging.
func TestAsyncAwaitAlreadyCompletedLive(t *testing.T) {
	path := tempFilePath(t)
	operation := getFileOperation(t, path)

	info, err := winrt.QueryInterface[foundation.IAsyncInfo](unsafe.Pointer(operation), &foundation.IID_IAsyncInfo)
	if err != nil {
		t.Fatalf("QueryInterface(IAsyncInfo): %v", err)
	}
	defer info.Release()
	deadline := time.Now().Add(10 * time.Second)
	for {
		status, err := info.Status()
		if err != nil {
			t.Fatalf("IAsyncInfo.Status: %v", err)
		}
		if status != foundation.AsyncStatusStarted {
			t.Logf("operation reached %v before Await", status)
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("operation did not reach a terminal state within 10s")
		}
		time.Sleep(10 * time.Millisecond)
	}

	file, err := operation.Await()
	if err != nil {
		t.Fatalf("Await after completion: %v", err)
	}
	defer file.Release()
	if name := fileName(t, file); name != filepath.Base(path) {
		t.Errorf("StorageFile name = %q, want %q", name, filepath.Base(path))
	}
}

// TestAsyncAwaitFailureLive proves the failure path: awaiting
// GetFileFromPathAsync on a nonexistent file returns (not hangs) an error
// that carries the operation's HRESULT — file-not-found — via errors.As.
func TestAsyncAwaitFailureLive(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "does-not-exist.txt")
	operation := getFileOperation(t, missing)

	file, err := operation.Await()
	if err == nil {
		if file != nil {
			file.Release()
		}
		t.Fatal("Await succeeded for a nonexistent path")
	}
	if file != nil {
		t.Errorf("Await returned a non-nil StorageFile alongside error %v", err)
	}
	var hr win32.HRESULT
	if !errors.As(err, &hr) {
		t.Fatalf("Await error does not carry an HRESULT: %v", err)
	}
	if int32(hr) != hresultFileNotFound {
		t.Errorf("Await error HRESULT = 0x%08x, want 0x80070002 (file not found); err = %v", uint32(hr), err)
	}
	t.Logf("Await failure: %v", err)
}
