//go:build windows && amd64

package acceptance

import (
	"math"
	"testing"

	"github.com/deploymenttheory/go-bindings-winrt/bindings/winrt/data/json"
	"github.com/deploymenttheory/go-bindings-winrt/bindings/winrt/devices/geolocation"
	"github.com/deploymenttheory/go-bindings-winrt/bindings/winrt/foundation"
)

// The live ground truth for the amd64 float and by-value aggregate
// lowerings.
//
// Both rest on how Go's asm_windows_amd64.s implements syscall.SyscallN: it
// mirrors each of the first four argument words into XMM0-XMM3 before the
// call, so a float's bit pattern placed in an ordinary argument word arrives
// in the register a double is actually read from. That is a claim about
// generated assembly, and reading assembly is not the same as watching the
// value come back, which is what these tests do.
//
// arm64 is excluded by build tag for the same reason the generated tree is:
// its asm_windows_arm64.s loads only R0-R7 and never touches V0-V7.

// TestFloat64ParamAndReturnLive drives a float64 in through a parameter and
// back out through an [out, retval] pointer.
//
// JsonValue is the ideal subject: CreateNumberValue takes a double and
// GetNumber returns one, with no device, permission or state in between, so
// a mismatch can only be the ABI.
func TestFloat64ParamAndReturnLive(t *testing.T) {
	statics, err := json.JsonValueStatics()
	if err != nil {
		t.Fatalf("JsonValueStatics: %v", err)
	}
	defer statics.Release()

	for _, want := range []float64{
		0,
		1,
		-1,
		0.5,
		-273.15,
		math.Pi,
		// Bit patterns that would survive a naive integer truncation only
		// by luck.
		1e300,
		-1e-300,
		math.SmallestNonzeroFloat64,
		math.MaxFloat64,
	} {
		value, err := statics.CreateNumberValue(want)
		if err != nil {
			t.Fatalf("CreateNumberValue(%v): %v", want, err)
		}

		got, err := value.GetNumber()
		value.Release()
		if err != nil {
			t.Fatalf("GetNumber after %v: %v", want, err)
		}
		if got != want {
			t.Errorf("round-trip = %v, want %v (float ABI)", got, want)
		}
	}
}

// TestFloat64StoredNotEchoedLive confirms the double reached the object's
// state rather than merely bouncing off the register it arrived in: the
// value goes in through one vtable slot and is read back through a
// different one that formats it as text.
func TestFloat64StoredNotEchoedLive(t *testing.T) {
	statics, err := json.JsonValueStatics()
	if err != nil {
		t.Fatalf("JsonValueStatics: %v", err)
	}
	defer statics.Release()

	value, err := statics.CreateNumberValue(-273.15)
	if err != nil {
		t.Fatalf("CreateNumberValue: %v", err)
	}
	defer value.Release()

	text, err := value.Stringify()
	if err != nil {
		t.Fatalf("Stringify: %v", err)
	}
	if text != "-273.15" {
		t.Errorf("Stringify = %q, want \"-273.15\"", text)
	}
}

// TestByValueStructInlineParamLive drives an 8-byte by-value struct through
// a parameter. Windows x64 passes a 1/2/4/8-byte aggregate in a general
// purpose register as an integer of that width, which the generator lowers
// as a width-exact read of the value's own bytes.
func TestByValueStructInlineParamLive(t *testing.T) {
	calendar := newCalendar(t)

	// An arbitrary but exact instant; DateTime is a single int64 of 100ns
	// ticks, so a truncated or misread word cannot round-trip by accident.
	want := foundation.DateTime{UniversalTime: 132559488000000000}
	if err := calendar.SetDateTime(want); err != nil {
		t.Fatalf("SetDateTime: %v", err)
	}
	got, err := calendar.GetDateTime()
	if err != nil {
		t.Fatalf("GetDateTime: %v", err)
	}
	if got != want {
		t.Errorf("DateTime round-trip = %+v, want %+v (inline by-value struct ABI)", got, want)
	}
}

// TestByValueStructByRefParamLive drives a 24-byte by-value struct through a
// parameter. Anything other than 1/2/4/8 bytes travels as a pointer to a
// caller-owned temporary, so this is the other half of the aggregate rule —
// and because BasicGeoposition is three doubles, it also shows the float
// payload surviving the indirection intact.
func TestByValueStructByRefParamLive(t *testing.T) {
	want := geolocation.BasicGeoposition{
		Latitude:  51.4816,
		Longitude: -3.1791,
		Altitude:  63.5,
	}

	point, err := geolocation.CreateGeopoint(want)
	if err != nil {
		t.Fatalf("CreateGeopoint: %v", err)
	}
	defer point.Release()

	got, err := point.Position()
	if err != nil {
		t.Fatalf("Position: %v", err)
	}
	if got != want {
		t.Errorf("BasicGeoposition round-trip = %+v, want %+v (by-ref by-value struct ABI)", got, want)
	}
}
