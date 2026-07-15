//go:build windows && (amd64 || arm64)

package winrt

import "testing"

func TestHStringRoundTrip(t *testing.T) {
	for _, s := range []string{
		"Hello, WinRT!",
		"Windows.Globalization.Calendar",
		"héllo wörld",
		"🌍🚀", // non-BMP: surrogate pairs
		"日本語",
	} {
		h, err := NewHString(s)
		if err != nil {
			t.Fatalf("NewHString(%q): %v", s, err)
		}
		if got := h.String(); got != s {
			t.Errorf("round-trip %q = %q", s, got)
		}
		if err := h.Close(); err != nil {
			t.Errorf("Close after %q: %v", s, err)
		}
	}
}

func TestHStringEmpty(t *testing.T) {
	h, err := NewHString("")
	if err != nil {
		t.Fatalf("NewHString(\"\"): %v", err)
	}
	if h.Raw() != 0 {
		t.Errorf("empty string HSTRING = %#x, want the null handle", h.Raw())
	}
	if got := h.String(); got != "" {
		t.Errorf("empty round-trip = %q", got)
	}
	if err := h.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
}

func TestHStringRejectsNUL(t *testing.T) {
	if _, err := NewHString("a\x00b"); err == nil {
		t.Fatal("NewHString accepted an embedded NUL")
	}
}

func TestHStringDoubleClose(t *testing.T) {
	h, err := NewHString("close me twice")
	if err != nil {
		t.Fatalf("NewHString: %v", err)
	}
	if err := h.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	if err := h.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
	if h.Raw() != 0 {
		t.Errorf("raw handle not zeroed after Close")
	}
}

func TestTakeHString(t *testing.T) {
	h, err := NewHString("taken")
	if err != nil {
		t.Fatalf("NewHString: %v", err)
	}
	// TakeHString consumes the handle; do not Close afterwards.
	if got := TakeHString(h.Raw()); got != "taken" {
		t.Errorf("TakeHString = %q, want taken", got)
	}
	if got := TakeHString(0); got != "" {
		t.Errorf("TakeHString(null) = %q, want empty", got)
	}
}
