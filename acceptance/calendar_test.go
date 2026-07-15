//go:build windows && (amd64 || arm64)

// Package acceptance exercises the bindings against the live Windows
// Runtime. These tests make real WinRT calls; they are the ground truth
// that slots, IIDs, and ABI shaping are correct.
package acceptance

import (
	"testing"

	syswinrt "github.com/deploymenttheory/go-bindings-win32/bindings/win32/system/winrt"

	"github.com/deploymenttheory/go-bindings-winrt/bindings/runtime/winrt"
	"github.com/deploymenttheory/go-bindings-winrt/bindings/winrt/globalization"
)

func newCalendar(t *testing.T) *globalization.Calendar {
	t.Helper()
	calendar, err := globalization.NewCalendar()
	if err != nil {
		t.Fatalf("NewCalendar: %v", err)
	}
	t.Cleanup(func() { calendar.Release() })
	return calendar
}

// TestCalendarClassName exercises IInspectable dispatch (slot 4) plus the
// HSTRING retval path.
func TestCalendarClassName(t *testing.T) {
	calendar := newCalendar(t)

	var name syswinrt.HSTRING
	if err := calendar.GetRuntimeClassName(&name); err != nil {
		t.Fatalf("GetRuntimeClassName: %v", err)
	}
	if got := winrt.TakeHString(name); got != "Windows.Globalization.Calendar" {
		t.Errorf("runtime class name = %q", got)
	}
}

// TestCalendarNow exercises void dispatch, propget int32, HSTRING retval,
// enum retval, and struct out-param — one live call per ABI shape.
func TestCalendarNow(t *testing.T) {
	calendar := newCalendar(t)

	if err := calendar.SetToNow(); err != nil {
		t.Fatalf("SetToNow: %v", err)
	}
	year, err := calendar.Year()
	if err != nil {
		t.Fatalf("Year: %v", err)
	}
	if year < 2025 || year > 2200 {
		t.Errorf("year = %d, want a plausible Gregorian year", year)
	}
	month, err := calendar.Month()
	if err != nil {
		t.Fatalf("Month: %v", err)
	}
	if month < 1 || month > 13 {
		t.Errorf("month = %d", month)
	}
	monthName, err := calendar.MonthAsFullString()
	if err != nil {
		t.Fatalf("MonthAsFullString: %v", err)
	}
	if monthName == "" {
		t.Error("MonthAsFullString is empty")
	}
	dayOfWeek, err := calendar.DayOfWeek()
	if err != nil {
		t.Fatalf("DayOfWeek: %v", err)
	}
	if dayOfWeek < globalization.DayOfWeekSunday || dayOfWeek > globalization.DayOfWeekSaturday {
		t.Errorf("day of week = %d", dayOfWeek)
	}
	dateTime, err := calendar.GetDateTime()
	if err != nil {
		t.Fatalf("GetDateTime: %v", err)
	}
	if dateTime.UniversalTime <= 0 {
		t.Errorf("UniversalTime = %d, want positive ticks", dateTime.UniversalTime)
	}
}

// TestCalendarMutation exercises propput, int32 params, and by-value struct
// params with round-trips.
func TestCalendarMutation(t *testing.T) {
	calendar := newCalendar(t)

	if err := calendar.SetToNow(); err != nil {
		t.Fatalf("SetToNow: %v", err)
	}
	if err := calendar.SetYear(2030); err != nil {
		t.Fatalf("SetYear: %v", err)
	}
	if year, err := calendar.Year(); err != nil || year != 2030 {
		t.Errorf("Year after SetYear(2030) = %d, %v", year, err)
	}

	day, err := calendar.Day()
	if err != nil {
		t.Fatalf("Day: %v", err)
	}
	if err := calendar.AddDays(1); err != nil {
		t.Fatalf("AddDays: %v", err)
	}
	next, err := calendar.Day()
	if err != nil {
		t.Fatalf("Day after AddDays: %v", err)
	}
	if next == day {
		t.Errorf("AddDays(1) did not change the day (%d)", day)
	}

	// By-value DateTime round-trip.
	dateTime, err := calendar.GetDateTime()
	if err != nil {
		t.Fatalf("GetDateTime: %v", err)
	}
	if err := calendar.SetDateTime(dateTime); err != nil {
		t.Fatalf("SetDateTime: %v", err)
	}
	back, err := calendar.GetDateTime()
	if err != nil {
		t.Fatalf("GetDateTime after SetDateTime: %v", err)
	}
	if back.UniversalTime != dateTime.UniversalTime {
		t.Errorf("DateTime round-trip: %d -> %d", dateTime.UniversalTime, back.UniversalTime)
	}
}

// TestCalendarSystem exercises HSTRING in + out on the same call pair.
func TestCalendarSystem(t *testing.T) {
	calendar := newCalendar(t)

	system, err := calendar.GetCalendarSystem()
	if err != nil {
		t.Fatalf("GetCalendarSystem: %v", err)
	}
	if system == "" {
		t.Fatal("calendar system is empty")
	}
	if err := calendar.ChangeCalendarSystem("JapaneseCalendar"); err != nil {
		t.Fatalf("ChangeCalendarSystem: %v", err)
	}
	if got, err := calendar.GetCalendarSystem(); err != nil || got != "JapaneseCalendar" {
		t.Errorf("calendar system after change = %q, %v", got, err)
	}
	if clock, err := calendar.GetClock(); err != nil || clock == "" {
		t.Errorf("GetClock = %q, %v", clock, err)
	}
}

// TestTimeZoneOnCalendar exercises QueryInterface to a second interface.
func TestTimeZoneOnCalendar(t *testing.T) {
	calendar := newCalendar(t)

	timeZone, err := calendar.AsTimeZoneOnCalendar()
	if err != nil {
		t.Fatalf("AsTimeZoneOnCalendar: %v", err)
	}
	defer timeZone.Release()

	zone, err := timeZone.GetTimeZone()
	if err != nil {
		t.Fatalf("GetTimeZone: %v", err)
	}
	if zone == "" {
		t.Error("time zone is empty")
	}
}

// TestRefCounting sanity-checks AddRef/Release promotion through the
// embedded IInspectable → IUnknown chain.
func TestRefCounting(t *testing.T) {
	calendar := newCalendar(t)

	after := calendar.AddRef()
	if after < 2 {
		t.Errorf("AddRef = %d, want >= 2", after)
	}
	calendar.Release()
}
