//go:build windows && (amd64 || arm64)

// Command collections drives WinRT collections in both directions:
//
//   - CONSUME a collection the OS built: ApplicationLanguages.Languages
//     returns an IVectorView<String> (monomorphized as
//     globalization.IVectorViewOfString), read with Size/GetAt and iterated
//     through its IIterable facet with First/HasCurrent/Current/MoveNext.
//
//   - PRODUCE a collection for the OS to consume: winrt.NewStringIterable
//     wraps a Go slice as a real COM object implementing IIterable<String>,
//     passed into the Calendar factory constructor — the OS calls First and
//     MoveNext on OUR vtables and the data round-trips back out.
package main

import (
	"fmt"
	"log"
	"unsafe"

	"github.com/deploymenttheory/go-bindings-winrt/bindings/runtime/winrt"
	"github.com/deploymenttheory/go-bindings-winrt/bindings/winrt/globalization"
)

func main() {
	consumeOSVector()
	passGoIterable()
}

// consumeOSVector reads a WinRT vector view the OS built.
func consumeOSVector() {
	statics, err := globalization.ApplicationLanguagesStatics()
	if err != nil {
		log.Fatalf("ApplicationLanguagesStatics: %v", err)
	}
	defer statics.Release()

	languages, err := statics.Languages()
	if err != nil {
		log.Fatalf("Languages: %v", err)
	}
	defer languages.Release()

	// Indexed access: Size + GetAt.
	size, err := languages.Size()
	if err != nil {
		log.Fatalf("Size: %v", err)
	}
	fmt.Printf("application languages (%d):\n", size)
	for i := uint32(0); i < size; i++ {
		tag, err := languages.GetAt(i)
		if err != nil {
			log.Fatalf("GetAt(%d): %v", i, err)
		}
		fmt.Printf("  [%d] %s\n", i, tag)
	}

	// Iterator access: every IVectorView is also IIterable — query for the
	// monomorphized iterable type and walk it. This is how you read
	// collections that are only IIterable (no indexed view).
	iterable, err := winrt.QueryInterface[globalization.IIterableOfString](
		unsafe.Pointer(languages), &globalization.IID_IIterableOfString)
	if err != nil {
		log.Fatalf("QueryInterface(IIterableOfString): %v", err)
	}
	defer iterable.Release()
	iterator, err := iterable.First()
	if err != nil {
		log.Fatalf("First: %v", err)
	}
	defer iterator.Release()
	fmt.Println("same list via First/HasCurrent/Current/MoveNext:")
	for {
		has, err := iterator.HasCurrent()
		if err != nil {
			log.Fatalf("HasCurrent: %v", err)
		}
		if !has {
			break
		}
		tag, err := iterator.Current()
		if err != nil {
			log.Fatalf("Current: %v", err)
		}
		fmt.Printf("  %s\n", tag)
		if _, err := iterator.MoveNext(); err != nil {
			log.Fatalf("MoveNext: %v", err)
		}
	}
}

// passGoIterable hands a Go-implemented IIterable<String> to a WinRT factory.
func passGoIterable() {
	// NewStringIterable copies the slice and wraps it as a COM object. The
	// cast to the generated consumer type is sound: both are a single
	// vtable-pointer word.
	source := []string{"de-DE", "en-GB"}
	iterable := winrt.NewStringIterable(source)

	calendar, err := globalization.CreateCalendarDefaultCalendarAndClock(
		(*globalization.IIterableOfString)(unsafe.Pointer(iterable)))
	// The factory consumed the iterable during the call; release our
	// reference (returns the remaining count — 0 proves the OS did not leak).
	if refs := iterable.Release(); refs != 0 {
		log.Fatalf("iterable refs after release = %d, want 0", refs)
	}
	if err != nil {
		log.Fatalf("CreateCalendarDefaultCalendarAndClock: %v", err)
	}
	defer calendar.Release()

	// Read the languages back OUT of the OS object: our Go slice went in
	// through our vtables and comes back through the OS's vector view.
	list, err := calendar.Languages()
	if err != nil {
		log.Fatalf("Calendar.Languages: %v", err)
	}
	defer list.Release()
	size, err := list.Size()
	if err != nil {
		log.Fatalf("Size: %v", err)
	}
	fmt.Printf("calendar built from Go iterable %v — languages round-tripped (%d):\n", source, size)
	for i := uint32(0); i < size; i++ {
		tag, err := list.GetAt(i)
		if err != nil {
			log.Fatalf("GetAt(%d): %v", i, err)
		}
		fmt.Printf("  [%d] %s\n", i, tag)
	}
}
