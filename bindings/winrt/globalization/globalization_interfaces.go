//go:build windows && (amd64 || arm64)

package globalization

import (
	"syscall"
	"unsafe"

	win32 "github.com/deploymenttheory/go-bindings-win32/bindings/runtime/win32"
	syswinrt "github.com/deploymenttheory/go-bindings-win32/bindings/win32/system/winrt"

	"github.com/deploymenttheory/go-bindings-winrt/bindings/runtime/winrt"
	"github.com/deploymenttheory/go-bindings-winrt/bindings/winrt/foundation"
)

// Vtable slots: IInspectable occupies 0–5 (QueryInterface/AddRef/Release/
// GetIids/GetRuntimeClassName/GetTrustLevel); an interface's first method is
// slot 6, in metadata MethodDef order. The slots below are pinned against
// the committed winmd by internal/verify.

// ICalendar: the default interface of Windows.Globalization.Calendar.
// IID: ca30221d-86d9-40fb-a26b-d44eb7cf08ea
type ICalendar struct {
	syswinrt.IInspectable
}

var IID_ICalendar = win32.GUID{Data1: 0xca30221d, Data2: 0x86d9, Data3: 0x40fb, Data4: [8]byte{0xa2, 0x6b, 0xd4, 0x4e, 0xb7, 0xcf, 0x08, 0xea}}

// GetCalendarSystem dispatches through ICalendar's vtable slot 12.
func (self *ICalendar) GetCalendarSystem() (string, error) {
	var value syswinrt.HSTRING
	r1, _, _ := syscall.SyscallN(self.LpVtbl[12], uintptr(unsafe.Pointer(self)), uintptr(unsafe.Pointer(&value)))
	if err := win32.ErrIfFailed(int32(r1)); err != nil {
		return "", err
	}
	return winrt.TakeHString(value), nil
}

// ChangeCalendarSystem dispatches through ICalendar's vtable slot 13.
func (self *ICalendar) ChangeCalendarSystem(value string) error {
	h, err := winrt.NewHString(value)
	if err != nil {
		return err
	}
	defer h.Close()
	r1, _, _ := syscall.SyscallN(self.LpVtbl[13], uintptr(unsafe.Pointer(self)), uintptr(h.Raw()))
	return win32.ErrIfFailed(int32(r1))
}

// GetClock dispatches through ICalendar's vtable slot 14.
func (self *ICalendar) GetClock() (string, error) {
	var value syswinrt.HSTRING
	r1, _, _ := syscall.SyscallN(self.LpVtbl[14], uintptr(unsafe.Pointer(self)), uintptr(unsafe.Pointer(&value)))
	if err := win32.ErrIfFailed(int32(r1)); err != nil {
		return "", err
	}
	return winrt.TakeHString(value), nil
}

// GetDateTime dispatches through ICalendar's vtable slot 16.
func (self *ICalendar) GetDateTime() (foundation.DateTime, error) {
	var result foundation.DateTime
	r1, _, _ := syscall.SyscallN(self.LpVtbl[16], uintptr(unsafe.Pointer(self)), uintptr(unsafe.Pointer(&result)))
	return result, win32.ErrIfFailed(int32(r1))
}

// SetDateTime dispatches through ICalendar's vtable slot 17. DateTime is a
// single-word struct at the WinRT ABI, passed by value.
func (self *ICalendar) SetDateTime(value foundation.DateTime) error {
	r1, _, _ := syscall.SyscallN(self.LpVtbl[17], uintptr(unsafe.Pointer(self)), uintptr(value.UniversalTime))
	return win32.ErrIfFailed(int32(r1))
}

// SetToNow dispatches through ICalendar's vtable slot 18.
func (self *ICalendar) SetToNow() error {
	r1, _, _ := syscall.SyscallN(self.LpVtbl[18], uintptr(unsafe.Pointer(self)))
	return win32.ErrIfFailed(int32(r1))
}

// Year (propget get_Year) dispatches through ICalendar's vtable slot 30.
func (self *ICalendar) Year() (int32, error) {
	var value int32
	r1, _, _ := syscall.SyscallN(self.LpVtbl[30], uintptr(unsafe.Pointer(self)), uintptr(unsafe.Pointer(&value)))
	return value, win32.ErrIfFailed(int32(r1))
}

// SetYear (propput put_Year) dispatches through ICalendar's vtable slot 31.
func (self *ICalendar) SetYear(value int32) error {
	r1, _, _ := syscall.SyscallN(self.LpVtbl[31], uintptr(unsafe.Pointer(self)), uintptr(value))
	return win32.ErrIfFailed(int32(r1))
}

// AddYears dispatches through ICalendar's vtable slot 32.
func (self *ICalendar) AddYears(years int32) error {
	r1, _, _ := syscall.SyscallN(self.LpVtbl[32], uintptr(unsafe.Pointer(self)), uintptr(years))
	return win32.ErrIfFailed(int32(r1))
}

// Month (propget get_Month) dispatches through ICalendar's vtable slot 39.
func (self *ICalendar) Month() (int32, error) {
	var value int32
	r1, _, _ := syscall.SyscallN(self.LpVtbl[39], uintptr(unsafe.Pointer(self)), uintptr(unsafe.Pointer(&value)))
	return value, win32.ErrIfFailed(int32(r1))
}

// AddMonths dispatches through ICalendar's vtable slot 41.
func (self *ICalendar) AddMonths(months int32) error {
	r1, _, _ := syscall.SyscallN(self.LpVtbl[41], uintptr(unsafe.Pointer(self)), uintptr(months))
	return win32.ErrIfFailed(int32(r1))
}

// MonthAsFullString dispatches through ICalendar's vtable slot 42.
func (self *ICalendar) MonthAsFullString() (string, error) {
	var result syswinrt.HSTRING
	r1, _, _ := syscall.SyscallN(self.LpVtbl[42], uintptr(unsafe.Pointer(self)), uintptr(unsafe.Pointer(&result)))
	if err := win32.ErrIfFailed(int32(r1)); err != nil {
		return "", err
	}
	return winrt.TakeHString(result), nil
}

// Day (propget get_Day) dispatches through ICalendar's vtable slot 52.
func (self *ICalendar) Day() (int32, error) {
	var value int32
	r1, _, _ := syscall.SyscallN(self.LpVtbl[52], uintptr(unsafe.Pointer(self)), uintptr(unsafe.Pointer(&value)))
	return value, win32.ErrIfFailed(int32(r1))
}

// AddDays dispatches through ICalendar's vtable slot 54.
func (self *ICalendar) AddDays(days int32) error {
	r1, _, _ := syscall.SyscallN(self.LpVtbl[54], uintptr(unsafe.Pointer(self)), uintptr(days))
	return win32.ErrIfFailed(int32(r1))
}

// DayOfWeek (propget get_DayOfWeek) dispatches through ICalendar's vtable
// slot 57.
func (self *ICalendar) DayOfWeek() (DayOfWeek, error) {
	var value DayOfWeek
	r1, _, _ := syscall.SyscallN(self.LpVtbl[57], uintptr(unsafe.Pointer(self)), uintptr(unsafe.Pointer(&value)))
	return value, win32.ErrIfFailed(int32(r1))
}

// DayOfWeekAsFullString dispatches through ICalendar's vtable slot 58.
func (self *ICalendar) DayOfWeekAsFullString() (string, error) {
	var result syswinrt.HSTRING
	r1, _, _ := syscall.SyscallN(self.LpVtbl[58], uintptr(unsafe.Pointer(self)), uintptr(unsafe.Pointer(&result)))
	if err := win32.ErrIfFailed(int32(r1)); err != nil {
		return "", err
	}
	return winrt.TakeHString(result), nil
}

// ITimeZoneOnCalendar: time-zone operations on a Calendar instance.
// IID: bb3c25e5-46cf-4317-a3f5-02621ad54478
type ITimeZoneOnCalendar struct {
	syswinrt.IInspectable
}

var IID_ITimeZoneOnCalendar = win32.GUID{Data1: 0xbb3c25e5, Data2: 0x46cf, Data3: 0x4317, Data4: [8]byte{0xa3, 0xf5, 0x02, 0x62, 0x1a, 0xd5, 0x44, 0x78}}

// GetTimeZone dispatches through ITimeZoneOnCalendar's vtable slot 6.
func (self *ITimeZoneOnCalendar) GetTimeZone() (string, error) {
	var value syswinrt.HSTRING
	r1, _, _ := syscall.SyscallN(self.LpVtbl[6], uintptr(unsafe.Pointer(self)), uintptr(unsafe.Pointer(&value)))
	if err := win32.ErrIfFailed(int32(r1)); err != nil {
		return "", err
	}
	return winrt.TakeHString(value), nil
}

// ChangeTimeZone dispatches through ITimeZoneOnCalendar's vtable slot 7.
func (self *ITimeZoneOnCalendar) ChangeTimeZone(timeZoneId string) error {
	h, err := winrt.NewHString(timeZoneId)
	if err != nil {
		return err
	}
	defer h.Close()
	r1, _, _ := syscall.SyscallN(self.LpVtbl[7], uintptr(unsafe.Pointer(self)), uintptr(h.Raw()))
	return win32.ErrIfFailed(int32(r1))
}

// TimeZoneAsFullString dispatches through ITimeZoneOnCalendar's vtable
// slot 8.
func (self *ITimeZoneOnCalendar) TimeZoneAsFullString() (string, error) {
	var result syswinrt.HSTRING
	r1, _, _ := syscall.SyscallN(self.LpVtbl[8], uintptr(unsafe.Pointer(self)), uintptr(unsafe.Pointer(&result)))
	if err := win32.ErrIfFailed(int32(r1)); err != nil {
		return "", err
	}
	return winrt.TakeHString(result), nil
}
