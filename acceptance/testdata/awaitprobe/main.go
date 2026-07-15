//go:build windows && (amd64 || arm64)

// Command awaitprobe is the checkdead regression probe: a minimal program
// whose ONLY pending wake-up during Await is the WinRT completion callback.
// Without the runtime keepalive (bindings/runtime/winrt/inspectable.go,
// startInspectableWorker), Go's deadlock detector kills this process with
// "fatal error: all goroutines are asleep - deadlock!" — a false positive,
// since the native threadpool thread it cannot see is about to call back.
// Run by acceptance/awaitprobe_test.go as a standalone binary because
// `go test` itself registers a timeout timer that masks the detector.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"unsafe"

	"github.com/deploymenttheory/go-bindings-winrt/bindings/runtime/winrt"
	"github.com/deploymenttheory/go-bindings-winrt/bindings/winrt/storage"
)

func main() {
	dir, err := os.MkdirTemp("", "awaitprobe")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, "probe.txt")
	if err := os.WriteFile(path, []byte("probe"), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	statics, err := storage.StorageFileStatics()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer statics.Release()
	operation, err := statics.GetFileFromPathAsync(path)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer operation.Release()

	// The probe: nothing else is runnable while this parks.
	file, err := operation.Await()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer file.Release()
	item, err := winrt.QueryInterface[storage.IStorageItem](unsafe.Pointer(file), &storage.IID_IStorageItem)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer item.Release()
	name, err := item.Name()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println("awaited:", name)
}
