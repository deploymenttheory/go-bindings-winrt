//go:build windows && (amd64 || arm64)

package acceptance

import (
	"slices"
	"testing"
	"unsafe"

	"github.com/deploymenttheory/go-bindings-winrt/bindings/runtime/winrt"
	"github.com/deploymenttheory/go-bindings-winrt/bindings/winrt/globalization"
)

// The Go-implemented collection runtime's live proof. These tests resolve
// the finding recorded in statics_factory_test.go: Calendar's factory
// constructors rejected a nil IIterable<String> with E_POINTER — with
// winrt.NewStringIterable the OS now consumes a Go-backed collection object
// (calling QueryInterface, First, get_HasCurrent, get_Current, and MoveNext
// on OUR vtables) and the factories are fully usable.

// asIterable casts a Go-implemented iterable to the generated consumer type;
// sound because both layouts are a single vtable-pointer word.
func asIterable(iterable *winrt.StringIterable) *globalization.IIterableOfString {
	return (*globalization.IIterableOfString)(unsafe.Pointer(iterable))
}

// TestStringIterableRoundTrip drives a Go-implemented iterable through the
// GENERATED IIterableOfString/IIteratorOfString dispatch — generated code
// consuming our object, no OS collection involved — and must read back the
// source slice exactly, non-ASCII included.
func TestStringIterableRoundTrip(t *testing.T) {
	source := []string{"en-US", "fr-FR", "日本語テスト", "emoji ✅ und Ümlaute"}
	iterable := winrt.NewStringIterable(source)

	iterator, err := asIterable(iterable).First()
	if err != nil {
		t.Fatalf("IIterableOfString.First: %v", err)
	}
	if iterator == nil {
		t.Fatal("First returned a nil iterator")
	}

	var got []string
	for {
		has, err := iterator.HasCurrent()
		if err != nil {
			t.Fatalf("IIteratorOfString.HasCurrent: %v", err)
		}
		if !has {
			break
		}
		current, err := iterator.Current()
		if err != nil {
			t.Fatalf("IIteratorOfString.Current: %v", err)
		}
		got = append(got, current)
		if _, err := iterator.MoveNext(); err != nil {
			t.Fatalf("IIteratorOfString.MoveNext: %v", err)
		}
	}
	if !slices.Equal(got, source) {
		t.Errorf("round trip = %q, want %q", got, source)
	}

	// Refcount sanity: the iterator reference is ours alone, and after it the
	// iterable's caller reference is the last — both drain to zero.
	if refs := iterator.Release(); refs != 0 {
		t.Errorf("iterator refs after release = %d, want 0", refs)
	}
	if refs := iterable.Release(); refs != 0 {
		t.Errorf("iterable refs after release = %d, want 0", refs)
	}
}

// TestFactoryCalendarLanguagesLive is the flagship: the OS's Calendar
// factory consumes a Go-implemented IIterable<String> to construct a
// Calendar whose language list round-trips our data back out of the OS.
func TestFactoryCalendarLanguagesLive(t *testing.T) {
	languages := winrt.NewStringIterable([]string{"en-US"})
	calendar, err := globalization.CreateCalendarDefaultCalendarAndClock(asIterable(languages))
	if err != nil {
		t.Fatalf("CreateCalendarDefaultCalendarAndClock: %v", err)
	}
	if calendar == nil {
		t.Fatal("factory returned a nil Calendar")
	}
	defer calendar.Release()

	// The OS has finished consuming the iterable; our reference is the last.
	if refs := languages.Release(); refs != 0 {
		t.Errorf("iterable refs after factory call + release = %d, want 0 (the OS leaked a reference)", refs)
	}

	list, err := calendar.Languages()
	if err != nil {
		t.Fatalf("ICalendar.Languages: %v", err)
	}
	defer list.Release()
	size, err := list.Size()
	if err != nil {
		t.Fatalf("IVectorViewOfString.Size: %v", err)
	}
	var index uint32
	found, err := list.IndexOf("en-US", &index)
	if err != nil {
		t.Fatalf("IVectorViewOfString.IndexOf: %v", err)
	}
	if !found {
		t.Errorf("calendar languages (%d entries) do not contain en-US", size)
	}
	t.Logf("Calendar built from Go iterable: %d language(s), en-US at %d", size, index)
}

// TestFactoryCalendarWithTimeZoneLive exercises the four-argument factory:
// Go iterable + three HSTRINGs in, and the time zone read back proves the
// constructed instance honored every argument.
func TestFactoryCalendarWithTimeZoneLive(t *testing.T) {
	languages := winrt.NewStringIterable([]string{"en-US"})
	calendar, err := globalization.CreateCalendarWithTimeZone(asIterable(languages), "GregorianCalendar", "24HourClock", "UTC")
	if err != nil {
		t.Fatalf("CreateCalendarWithTimeZone: %v", err)
	}
	if calendar == nil {
		t.Fatal("factory returned a nil Calendar")
	}
	defer calendar.Release()
	if refs := languages.Release(); refs != 0 {
		t.Errorf("iterable refs after factory call + release = %d, want 0", refs)
	}

	if system, err := calendar.GetCalendarSystem(); err != nil || system != "GregorianCalendar" {
		t.Errorf("GetCalendarSystem = %q, %v; want GregorianCalendar", system, err)
	}
	if clock, err := calendar.GetClock(); err != nil || clock != "24HourClock" {
		t.Errorf("GetClock = %q, %v; want 24HourClock", clock, err)
	}
	timeZone, err := calendar.AsTimeZoneOnCalendar()
	if err != nil {
		t.Fatalf("AsTimeZoneOnCalendar: %v", err)
	}
	defer timeZone.Release()
	// Newer Windows builds normalize the "UTC" ID to the IANA form "Etc/UTC".
	if zone, err := timeZone.GetTimeZone(); err != nil || (zone != "UTC" && zone != "Etc/UTC") {
		t.Errorf("GetTimeZone = %q, %v; want UTC (or Etc/UTC)", zone, err)
	}
}

// TestStringVectorViewAsIterableLive drives the vector view's IIterable
// tear-off through the generated consumer surface: QueryInterface for
// IIterable<String> lands on the tear-off facet, and the generated First/
// iteration reads the backing slice.
func TestStringVectorViewAsIterableLive(t *testing.T) {
	source := []string{"zh-Hans-CN", "de-DE"}
	view := winrt.NewStringVectorView(source)

	iterable, err := winrt.QueryInterface[globalization.IIterableOfString](
		unsafe.Pointer(view), &globalization.IID_IIterableOfString)
	if err != nil {
		t.Fatalf("QI(IIterable<String>) on the vector view: %v", err)
	}
	iterator, err := iterable.First()
	if err != nil {
		t.Fatalf("tear-off First: %v", err)
	}

	var got []string
	for {
		has, err := iterator.HasCurrent()
		if err != nil {
			t.Fatalf("HasCurrent: %v", err)
		}
		if !has {
			break
		}
		current, err := iterator.Current()
		if err != nil {
			t.Fatalf("Current: %v", err)
		}
		got = append(got, current)
		if _, err := iterator.MoveNext(); err != nil {
			t.Fatalf("MoveNext: %v", err)
		}
	}
	if !slices.Equal(got, source) {
		t.Errorf("tear-off iteration = %q, want %q", got, source)
	}

	if refs := iterator.Release(); refs != 0 {
		t.Errorf("iterator refs after release = %d, want 0", refs)
	}
	iterable.Release() // the QI reference (shared count with the view)
	if refs := view.Release(); refs != 0 {
		t.Errorf("view refs after release = %d, want 0", refs)
	}
}

// TestCollectionsIIDsMatchGenerated pins the runtime package's hard-coded
// collection IIDs to the generator's independently emitted constants (the
// derivation-level guard lives in internal/verify).
func TestCollectionsIIDsMatchGenerated(t *testing.T) {
	if winrt.IIDIterableOfString != globalization.IID_IIterableOfString {
		t.Errorf("IIDIterableOfString %s != generated %s", winrt.IIDIterableOfString, globalization.IID_IIterableOfString)
	}
	if winrt.IIDIteratorOfString != globalization.IID_IIteratorOfString {
		t.Errorf("IIDIteratorOfString %s != generated %s", winrt.IIDIteratorOfString, globalization.IID_IIteratorOfString)
	}
	if winrt.IIDVectorViewOfString != globalization.IID_IVectorViewOfString {
		t.Errorf("IIDVectorViewOfString %s != generated %s", winrt.IIDVectorViewOfString, globalization.IID_IVectorViewOfString)
	}
}
