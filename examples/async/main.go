//go:build windows && (amd64 || arm64)

// Command async awaits a real WinRT async operation from Go:
// StorageFile.GetFileFromPathAsync returns an IAsyncOperation<StorageFile>,
// and the generated Await() blocks until it reaches a terminal state.
//
// It then demonstrates the failure path: awaiting the same call on a path
// that does not exist returns (not hangs) an error that carries the
// operation's HRESULT — reachable with errors.As — wrapped in a
// winrt.AsyncError naming the terminal AsyncStatus.
//
// Await is guarded with a timeout here (awaitFile below) — good practice
// because Await blocks indefinitely by design. A bare Await() is also safe,
// even in a minimal program: the winrt runtime keeps a keepalive timer
// registered so the WinRT completion arriving on a native thread (invisible
// to Go's deadlock detector) can never get the process falsely declared
// deadlocked. See docs/async.md.
package main

import (
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"
	"unsafe"

	win32 "github.com/deploymenttheory/go-bindings-win32/bindings/runtime/win32"

	"github.com/deploymenttheory/go-bindings-winrt/bindings/runtime/winrt"
	"github.com/deploymenttheory/go-bindings-winrt/bindings/winrt/storage"
)

// awaitFile runs Await on its own goroutine and bounds it with a timeout.
// On timeout the goroutine stays parked on the still-running operation —
// acceptable for a short-lived program; cancel via IAsyncInfo if you need
// to reclaim it.
func awaitFile(operation *storage.IAsyncOperationOfStorageFile) (*storage.IStorageFile, error) {
	type result struct {
		file *storage.IStorageFile
		err  error
	}
	done := make(chan result, 1)
	go func() {
		file, err := operation.Await()
		done <- result{file, err}
	}()
	select {
	case r := <-done:
		return r.file, r.err
	case <-time.After(30 * time.Second):
		return nil, errors.New("timed out awaiting the operation")
	}
}

func main() {
	// A real file for the success path, created with plain Go.
	dir, err := os.MkdirTemp("", "winrt-async-example")
	if err != nil {
		log.Fatalf("os.MkdirTemp: %v", err)
	}
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, "hello.txt")
	if err := os.WriteFile(path, []byte("awaited from Go\n"), 0o644); err != nil {
		log.Fatalf("os.WriteFile: %v", err)
	}

	statics, err := storage.StorageFileStatics()
	if err != nil {
		log.Fatalf("StorageFileStatics: %v", err)
	}
	defer statics.Release()

	// --- Success path ---------------------------------------------------
	// GetFileFromPathAsync starts the operation and returns immediately;
	// Await blocks until it completes. One Await (or SetCompleted) per
	// operation instance — WinRT accepts a single Completed assignment.
	operation, err := statics.GetFileFromPathAsync(path)
	if err != nil {
		log.Fatalf("GetFileFromPathAsync: %v", err)
	}
	defer operation.Release()

	file, err := awaitFile(operation)
	if err != nil {
		log.Fatalf("Await: %v", err)
	}
	defer file.Release()

	// Name lives on IStorageItem, another interface of the same object —
	// query for it.
	item, err := winrt.QueryInterface[storage.IStorageItem](unsafe.Pointer(file), &storage.IID_IStorageItem)
	if err != nil {
		log.Fatalf("QueryInterface(IStorageItem): %v", err)
	}
	defer item.Release()
	name, err := item.Name()
	if err != nil {
		log.Fatalf("IStorageItem.Name: %v", err)
	}
	fmt.Printf("awaited StorageFile: %s\n", name)

	// --- Failure path ---------------------------------------------------
	missing := filepath.Join(dir, "does-not-exist.txt")
	failingOperation, err := statics.GetFileFromPathAsync(missing)
	if err != nil {
		log.Fatalf("GetFileFromPathAsync(missing): %v", err)
	}
	defer failingOperation.Release()

	if _, err := awaitFile(failingOperation); err != nil {
		fmt.Printf("await on a missing file failed as expected:\n  %v\n", err)
		// The error wraps the IAsyncInfo error code as a win32.HRESULT.
		var hr win32.HRESULT
		if errors.As(err, &hr) {
			fmt.Printf("  HRESULT via errors.As: 0x%08X (0x80070002 = ERROR_FILE_NOT_FOUND)\n", uint32(hr))
		}
	} else {
		log.Fatal("await on a missing file unexpectedly succeeded")
	}
}
