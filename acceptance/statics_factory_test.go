//go:build windows && (amd64 || arm64)

package acceptance

import (
	"testing"

	"github.com/deploymenttheory/go-bindings-winrt/bindings/winrt/globalization"
)

// The generated statics surface's live proof: package-level accessors fetch
// the class's activation factory queried to the statics IID, and the
// generated statics interface methods dispatch through it — including for
// statics-ONLY classes (CalendarIdentifiers, ClockIdentifiers,
// ApplicationLanguages), which have no class type at all.

func TestStaticsCalendarIdentifiersLive(t *testing.T) {
	statics, err := globalization.CalendarIdentifiersStatics()
	if err != nil {
		t.Fatalf("CalendarIdentifiersStatics: %v", err)
	}
	defer statics.Release()

	gregorian, err := statics.Gregorian()
	if err != nil {
		t.Fatalf("ICalendarIdentifiersStatics.Gregorian: %v", err)
	}
	if gregorian != "GregorianCalendar" {
		t.Errorf("Gregorian = %q, want GregorianCalendar", gregorian)
	}
	hebrew, err := statics.Hebrew()
	if err != nil {
		t.Fatalf("ICalendarIdentifiersStatics.Hebrew: %v", err)
	}
	if hebrew != "HebrewCalendar" {
		t.Errorf("Hebrew = %q, want HebrewCalendar", hebrew)
	}
}

func TestStaticsClockIdentifiersLive(t *testing.T) {
	statics, err := globalization.ClockIdentifiersStatics()
	if err != nil {
		t.Fatalf("ClockIdentifiersStatics: %v", err)
	}
	defer statics.Release()

	twentyFour, err := statics.TwentyFourHour()
	if err != nil {
		t.Fatalf("IClockIdentifiersStatics.TwentyFourHour: %v", err)
	}
	if twentyFour != "24HourClock" {
		t.Errorf("TwentyFourHour = %q, want 24HourClock", twentyFour)
	}
}

func TestStaticsApplicationLanguagesLive(t *testing.T) {
	statics, err := globalization.ApplicationLanguagesStatics()
	if err != nil {
		t.Fatalf("ApplicationLanguagesStatics: %v", err)
	}
	defer statics.Release()

	languages, err := statics.Languages()
	if err != nil {
		t.Fatalf("IApplicationLanguagesStatics.Languages: %v", err)
	}
	if languages == nil {
		t.Fatal("Languages returned a nil vector view")
	}
	defer languages.Release()
	size, err := languages.Size()
	if err != nil {
		t.Fatalf("IVectorViewOfString.Size: %v", err)
	}
	if size == 0 {
		t.Fatal("application languages list is empty; the system always reports at least one")
	}
	first, err := languages.GetAt(0)
	if err != nil {
		t.Fatalf("IVectorViewOfString.GetAt(0): %v", err)
	}
	if first == "" {
		t.Fatal("first application language tag is empty")
	}
	t.Logf("application languages: %d entries, first = %s", size, first)
}

// The generated factory-constructor surface's live proof: the package-level
// constructor fetches the factory, dispatches the generated factory-interface
// method, and re-types the returned default interface as the class.
// IGeographicRegionFactory.CreateGeographicRegion takes only an HSTRING, so
// every argument is constructible from Go.
func TestFactoryGeographicRegionLive(t *testing.T) {
	region, err := globalization.CreateGeographicRegion("US")
	if err != nil {
		t.Fatalf("CreateGeographicRegion(US): %v", err)
	}
	defer region.Release()

	code, err := region.CodeTwoLetter()
	if err != nil {
		t.Fatalf("IGeographicRegion.CodeTwoLetter: %v", err)
	}
	if code != "US" {
		t.Errorf("CodeTwoLetter = %q, want US", code)
	}
	display, err := region.DisplayName()
	if err != nil {
		t.Fatalf("IGeographicRegion.DisplayName: %v", err)
	}
	if display == "" {
		t.Error("DisplayName is empty")
	}
	t.Logf("region %s: %s", code, display)
}

// Calendar's factory constructors require an IIterable<String> languages
// argument; the OS rejects a null iterable with E_POINTER (verified live).
// Go-implemented collections landed since (winrt.NewStringIterable) —
// collections_test.go exercises those constructors end to end;
// CreateGeographicRegion above remains the simplest factory proof.
