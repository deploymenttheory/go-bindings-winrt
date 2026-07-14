//go:build windows && (amd64 || arm64)

package globalization

// DayOfWeek is Windows.Globalization.DayOfWeek.
type DayOfWeek int32

const (
	DayOfWeekSunday    DayOfWeek = 0
	DayOfWeekMonday    DayOfWeek = 1
	DayOfWeekTuesday   DayOfWeek = 2
	DayOfWeekWednesday DayOfWeek = 3
	DayOfWeekThursday  DayOfWeek = 4
	DayOfWeekFriday    DayOfWeek = 5
	DayOfWeekSaturday  DayOfWeek = 6
)

// String returns the day name.
func (d DayOfWeek) String() string {
	switch d {
	case DayOfWeekSunday:
		return "Sunday"
	case DayOfWeekMonday:
		return "Monday"
	case DayOfWeekTuesday:
		return "Tuesday"
	case DayOfWeekWednesday:
		return "Wednesday"
	case DayOfWeekThursday:
		return "Thursday"
	case DayOfWeekFriday:
		return "Friday"
	case DayOfWeekSaturday:
		return "Saturday"
	}
	return "DayOfWeek(unknown)"
}
