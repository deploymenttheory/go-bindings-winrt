//go:build windows && (amd64 || arm64)

package verify

import (
	"path/filepath"
	"testing"

	win32 "github.com/deploymenttheory/go-bindings-win32/bindings/runtime/win32"
	winmd "github.com/deploymenttheory/go-winmd"

	"github.com/deploymenttheory/go-bindings-winrt/bindings/winrt/globalization"
)

// The hand-written vertical hardcodes vtable slots and IIDs. These tests
// pin both against the committed contract winmd, so a metadata update or a
// transcription mistake fails loudly instead of corrupting live calls.

func openContract(t *testing.T) *winmd.File {
	t.Helper()
	path := filepath.Join("..", "..", "metadata", "winmd", "Windows.Foundation.UniversalApiContract.winmd")
	file, err := winmd.Open(path)
	if err != nil {
		t.Fatalf("opening committed contract winmd: %v", err)
	}
	return file
}

func findTypeDef(t *testing.T, file *winmd.File, namespace, name string) (uint32, *winmd.TypeDefRow) {
	t.Helper()
	for i := range file.Tables.TypeDefs {
		typeDef := &file.Tables.TypeDefs[i]
		if typeDef.Namespace == namespace && typeDef.Name == name {
			return uint32(i + 1), typeDef
		}
	}
	t.Fatalf("%s.%s not found in the committed winmd", namespace, name)
	return 0, nil
}

// guidOf reassembles a TypeDef's [Guid] attribute into a win32.GUID.
func guidOf(t *testing.T, file *winmd.File, row uint32) win32.GUID {
	t.Helper()
	for _, attr := range file.AttributesFor(winmd.CodedIndex{Table: winmd.TableTypeDef, Row: row}) {
		if attr.Name != "GuidAttribute" || len(attr.Fixed) != 11 {
			continue
		}
		var guid win32.GUID
		guid.Data1, _ = attr.Fixed[0].(uint32)
		guid.Data2, _ = attr.Fixed[1].(uint16)
		guid.Data3, _ = attr.Fixed[2].(uint16)
		for i := range 8 {
			guid.Data4[i], _ = attr.Fixed[3+i].(byte)
		}
		return guid
	}
	t.Fatal("TypeDef has no GuidAttribute")
	return win32.GUID{}
}

// projectedName is the unique method name the bindings expose: WinRT
// overloads share a MethodDef name (e.g. two MonthAsString rows) and the
// [Overload] attribute carries the unique name (MonthAsFullString); plain
// methods just use their MethodDef name.
func projectedName(file *winmd.File, row uint32) string {
	for _, attr := range file.AttributesFor(winmd.CodedIndex{Table: winmd.TableMethodDef, Row: row}) {
		if attr.Name == "OverloadAttribute" && len(attr.Fixed) == 1 {
			if unique, ok := attr.Fixed[0].(string); ok && unique != "" {
				return unique
			}
		}
	}
	return file.Tables.Methods[row-1].Name
}

// slotOf returns the vtable slot of the method with the given projected
// name: 6 (past IInspectable) + its MethodDef index within the TypeDef.
func slotOf(t *testing.T, file *winmd.File, typeDef *winmd.TypeDefRow, method string) (int, *winmd.MethodDefRow) {
	t.Helper()
	for row := typeDef.MethodFirst; row < typeDef.MethodEnd; row++ {
		if projectedName(file, row) == method {
			return 6 + int(row-typeDef.MethodFirst), &file.Tables.Methods[row-1]
		}
	}
	t.Fatalf("method %s not found on %s", method, typeDef.Name)
	return 0, nil
}

// TestICalendarIIDs pins the hand-written IID vars to the metadata [Guid].
func TestICalendarIIDs(t *testing.T) {
	file := openContract(t)

	icalRow, _ := findTypeDef(t, file, "Windows.Globalization", "ICalendar")
	if got := guidOf(t, file, icalRow); got != globalization.IID_ICalendar {
		t.Errorf("IID_ICalendar = %s, metadata says %s", globalization.IID_ICalendar, got)
	}
	tzRow, _ := findTypeDef(t, file, "Windows.Globalization", "ITimeZoneOnCalendar")
	if got := guidOf(t, file, tzRow); got != globalization.IID_ITimeZoneOnCalendar {
		t.Errorf("IID_ITimeZoneOnCalendar = %s, metadata says %s", globalization.IID_ITimeZoneOnCalendar, got)
	}
}

// TestICalendarSlots pins every hand-written {method, slot} pair to the
// metadata MethodDef order.
func TestICalendarSlots(t *testing.T) {
	file := openContract(t)
	_, icalendar := findTypeDef(t, file, "Windows.Globalization", "ICalendar")

	for method, want := range map[string]int{
		"GetCalendarSystem":     12,
		"ChangeCalendarSystem":  13,
		"GetClock":              14,
		"GetDateTime":           16,
		"SetDateTime":           17,
		"SetToNow":              18,
		"get_Languages":         9,
		"get_Year":              30,
		"put_Year":              31,
		"AddYears":              32,
		"get_Month":             39,
		"AddMonths":             41,
		"MonthAsFullString":     42,
		"get_Day":               52,
		"AddDays":               54,
		"get_DayOfWeek":         57,
		"DayOfWeekAsFullString": 58,
	} {
		if got, _ := slotOf(t, file, icalendar, method); got != want {
			t.Errorf("ICalendar.%s slot = %d, hand-written code dispatches %d", method, got, want)
		}
	}

	_, timeZone := findTypeDef(t, file, "Windows.Globalization", "ITimeZoneOnCalendar")
	for method, want := range map[string]int{
		"GetTimeZone":          6,
		"ChangeTimeZone":       7,
		"TimeZoneAsFullString": 8,
	} {
		if got, _ := slotOf(t, file, timeZone, method); got != want {
			t.Errorf("ITimeZoneOnCalendar.%s slot = %d, hand-written code dispatches %d", method, got, want)
		}
	}
}

// TestICalendarSignatureShapes spot-checks that the metadata's logical
// signatures match the Go shapes the vertical exposes.
func TestICalendarSignatureShapes(t *testing.T) {
	file := openContract(t)
	_, icalendar := findTypeDef(t, file, "Windows.Globalization", "ICalendar")

	shape := func(method string) winmd.MethodSig {
		_, methodRow := slotOf(t, file, icalendar, method)
		sig, err := file.MethodSignature(methodRow.Signature)
		if err != nil {
			t.Fatalf("%s signature: %v", method, err)
		}
		return sig
	}

	if sig := shape("GetDateTime"); sig.Return.Kind != winmd.SigNamed || !sig.Return.IsValueType ||
		sig.Return.Namespace != "Windows.Foundation" || sig.Return.Name != "DateTime" {
		t.Errorf("GetDateTime returns %+v, want valuetype Windows.Foundation.DateTime", sig.Return)
	}
	if sig := shape("MonthAsFullString"); sig.Return.Kind != winmd.SigPrimitive || sig.Return.Primitive != winmd.ElemString {
		t.Errorf("MonthAsFullString returns %+v, want STRING (HSTRING)", sig.Return)
	}
	if sig := shape("get_DayOfWeek"); sig.Return.Kind != winmd.SigNamed || !sig.Return.IsValueType ||
		sig.Return.Namespace != "Windows.Globalization" || sig.Return.Name != "DayOfWeek" {
		t.Errorf("get_DayOfWeek returns %+v, want valuetype Windows.Globalization.DayOfWeek", sig.Return)
	}
	if sig := shape("SetDateTime"); len(sig.Params) != 1 || sig.Params[0].Kind != winmd.SigNamed ||
		!sig.Params[0].IsValueType || sig.Params[0].Name != "DateTime" {
		t.Errorf("SetDateTime params = %+v, want one by-value DateTime", sig.Params)
	}
}

// TestCalendarIsWindowsRuntime asserts the runtime-class TypeDef carries the
// tdWindowsRuntime flag the ingest stage will rely on.
func TestCalendarIsWindowsRuntime(t *testing.T) {
	file := openContract(t)
	_, calendar := findTypeDef(t, file, "Windows.Globalization", "Calendar")
	if calendar.Flags&winmd.TypeAttrWindowsRuntime == 0 {
		t.Error("Calendar TypeDef lacks tdWindowsRuntime")
	}
}
