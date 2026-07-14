//go:build windows && (amd64 || arm64)

package globalization

import (
	"unsafe"

	"github.com/deploymenttheory/go-bindings-winrt/bindings/runtime/winrt"
)

// Calendar is the Windows.Globalization.Calendar runtime class, surfaced
// through its default interface ICalendar. Release when done (promoted from
// the embedded IInspectable → IUnknown chain).
type Calendar struct {
	ICalendar
}

// NewCalendar activates Windows.Globalization.Calendar through its default
// constructor.
func NewCalendar() (*Calendar, error) {
	instance, err := winrt.ActivateInstance("Windows.Globalization.Calendar")
	if err != nil {
		return nil, err
	}
	defer instance.Release()
	return winrt.QueryInterface[Calendar](unsafe.Pointer(instance), &IID_ICalendar)
}

// AsTimeZoneOnCalendar queries the instance's ITimeZoneOnCalendar interface.
// The returned reference is owned by the caller.
func (c *Calendar) AsTimeZoneOnCalendar() (*ITimeZoneOnCalendar, error) {
	return winrt.QueryInterface[ITimeZoneOnCalendar](unsafe.Pointer(c), &IID_ITimeZoneOnCalendar)
}
