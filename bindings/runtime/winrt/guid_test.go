//go:build windows && (amd64 || arm64)

package winrt

import "testing"

func TestParseGUIDRoundTrip(t *testing.T) {
	for _, s := range []string{
		"ca30221d-86d9-40fb-a26b-d44eb7cf08ea", // ICalendar
		"bb3c25e5-46cf-4317-a3f5-02621ad54478", // ITimeZoneOnCalendar
		"00000000-0000-0000-c000-000000000046", // IUnknown
		"af86e2e0-b12d-4c6a-9c5a-d7aa65101e90", // IInspectable
	} {
		guid, err := ParseGUID(s)
		if err != nil {
			t.Fatalf("ParseGUID(%q): %v", s, err)
		}
		if got := guid.String(); got != s {
			t.Errorf("round-trip %q = %q", s, got)
		}
	}
}

func TestParseGUIDForms(t *testing.T) {
	want, err := ParseGUID("CA30221D-86D9-40FB-A26B-D44EB7CF08EA") // uppercase
	if err != nil {
		t.Fatalf("uppercase: %v", err)
	}
	braced, err := ParseGUID("{ca30221d-86d9-40fb-a26b-d44eb7cf08ea}")
	if err != nil {
		t.Fatalf("braced: %v", err)
	}
	if want != braced {
		t.Errorf("uppercase %v != braced %v", want, braced)
	}
	if want.Data1 != 0xCA30221D || want.Data2 != 0x86D9 || want.Data3 != 0x40FB ||
		want.Data4 != [8]byte{0xA2, 0x6B, 0xD4, 0x4E, 0xB7, 0xCF, 0x08, 0xEA} {
		t.Errorf("field decomposition wrong: %+v", want)
	}
}

func TestParseGUIDErrors(t *testing.T) {
	for _, s := range []string{
		"",
		"not-a-guid",
		"ca30221d-86d9-40fb-a26b-d44eb7cf08e",   // one short
		"ca30221d-86d9-40fb-a26b-d44eb7cf08eaa", // one long
		"ca30221dx86d9-40fb-a26b-d44eb7cf08ea",  // bad separator
		"gg30221d-86d9-40fb-a26b-d44eb7cf08ea",  // non-hex
		"{ca30221d-86d9-40fb-a26b-d44eb7cf08ea", // unbalanced brace
	} {
		if _, err := ParseGUID(s); err == nil {
			t.Errorf("ParseGUID(%q) succeeded, want error", s)
		}
	}
}

func TestMustGUIDPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("MustGUID did not panic on malformed input")
		}
	}()
	MustGUID("nope")
}
