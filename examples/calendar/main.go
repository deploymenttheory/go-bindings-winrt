//go:build windows && (amd64 || arm64)

// Command calendar prints the current date through several WinRT calendar
// systems plus the system time zone — the Windows.Globalization.Calendar
// vertical end to end.
package main

import (
	"fmt"
	"log"

	"github.com/deploymenttheory/go-bindings-winrt/bindings/winrt/globalization"
)

func main() {
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
		weekday, err := calendar.DayOfWeek()
		if err != nil {
			log.Fatalf("DayOfWeek: %v", err)
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
}
