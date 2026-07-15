//go:build windows && (amd64 || arm64)

// Command calendar prints the current date through several WinRT calendar
// systems plus the system time zone — the Windows.Globalization.Calendar
// vertical end to end. It shows both ways to construct a runtime class:
//
//   - NewCalendar() — direct activation through the default constructor.
//   - CreateCalendarWithTimeZone(...) — a factory constructor whose
//     IIterable<String> languages argument is a Go-implemented WinRT
//     collection (winrt.NewStringIterable): the OS iterates OUR object.
package main

import (
	"fmt"
	"log"
	"unsafe"

	"github.com/deploymenttheory/go-bindings-winrt/bindings/runtime/winrt"
	"github.com/deploymenttheory/go-bindings-winrt/bindings/winrt/globalization"
)

func main() {
	// Direct activation: the class's default constructor. The returned
	// *Calendar owns one reference — Release it when done.
	calendar, err := globalization.NewCalendar()
	if err != nil {
		log.Fatalf("activating Calendar: %v", err)
	}
	defer calendar.Release()

	if err := calendar.SetToNow(); err != nil {
		log.Fatalf("SetToNow: %v", err)
	}

	for _, system := range []string{"GregorianCalendar", "HebrewCalendar", "HijriCalendar", "JapaneseCalendar"} {
		if err := calendar.ChangeCalendarSystem(system); err != nil {
			log.Fatalf("ChangeCalendarSystem(%s): %v", system, err)
		}
		year, err := calendar.Year()
		if err != nil {
			log.Fatalf("Year: %v", err)
		}
		month, err := calendar.MonthAsFullString()
		if err != nil {
			log.Fatalf("MonthAsFullString: %v", err)
		}
		day, err := calendar.Day()
		if err != nil {
			log.Fatalf("Day: %v", err)
		}
		weekday, err := calendar.DayOfWeekAsFullString()
		if err != nil {
			log.Fatalf("DayOfWeekAsFullString: %v", err)
		}
		fmt.Printf("%-18s %s, %s %d, %d\n", system+":", weekday, month, day, year)
	}

	timeZone, err := calendar.AsTimeZoneOnCalendar()
	if err != nil {
		log.Fatalf("AsTimeZoneOnCalendar: %v", err)
	}
	defer timeZone.Release()
	zone, err := timeZone.GetTimeZone()
	if err != nil {
		log.Fatalf("GetTimeZone: %v", err)
	}
	full, err := timeZone.TimeZoneAsFullString()
	if err != nil {
		log.Fatalf("TimeZoneAsFullString: %v", err)
	}
	fmt.Printf("%-18s %s (%s)\n", "Time zone:", zone, full)

	// Factory constructor: CreateCalendarWithTimeZone takes an
	// IIterable<String> of BCP-47 language tags plus explicit calendar
	// system, clock, and time-zone IDs. WinRT rejects a null iterable
	// (E_POINTER), so we pass a Go-implemented one: NewStringIterable wraps
	// a Go slice as a real COM object the OS can call First/MoveNext on.
	// The cast to the generated consumer type is sound because both are a
	// single vtable-pointer word.
	languages := winrt.NewStringIterable([]string{"fr-FR", "en-US"})
	french, err := globalization.CreateCalendarWithTimeZone(
		(*globalization.IIterableOfString)(unsafe.Pointer(languages)),
		"GregorianCalendar", "24HourClock", "UTC")
	// The factory has finished consuming the iterable; drop our reference.
	languages.Release()
	if err != nil {
		log.Fatalf("CreateCalendarWithTimeZone: %v", err)
	}
	defer french.Release()

	if err := french.SetToNow(); err != nil {
		log.Fatalf("SetToNow (factory calendar): %v", err)
	}
	month, err := french.MonthAsFullString()
	if err != nil {
		log.Fatalf("MonthAsFullString (factory calendar): %v", err)
	}
	clock, err := french.GetClock()
	if err != nil {
		log.Fatalf("GetClock: %v", err)
	}
	utcZone, err := french.AsTimeZoneOnCalendar()
	if err != nil {
		log.Fatalf("AsTimeZoneOnCalendar (factory calendar): %v", err)
	}
	defer utcZone.Release()
	zoneID, err := utcZone.GetTimeZone()
	if err != nil {
		log.Fatalf("GetTimeZone (factory calendar): %v", err)
	}
	fmt.Printf("%-18s month=%s clock=%s zone=%s (via Go-implemented IIterable<String>)\n",
		"Factory (fr-FR):", month, clock, zoneID)
}
