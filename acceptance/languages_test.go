//go:build windows && (amd64 || arm64)

package acceptance

import (
	"testing"

	"github.com/deploymenttheory/go-bindings-winrt/bindings/winrt/globalization"
)

// TestCalendarLanguagesGenerated exercises the GENERATED generic
// instantiation surface end to end: ICalendar.get_Languages (vtable slot 9)
// now emits as Calendar.Languages() returning *globalization.
// IVectorViewOfString — a monomorphized IVectorView`1<String> whose IID the
// generator derived through the pinterface engine — and the typed Size/GetAt
// dispatch must read real data through it. This is the generated-code
// counterpart of TestPinterfaceIIDsLive's raw-syscall proof.
func TestCalendarLanguagesGenerated(t *testing.T) {
	calendar := newCalendar(t)

	// The declared type pins the generated surface: Languages() must return
	// the package-local monomorphized instantiation.
	var languages *globalization.IVectorViewOfString
	languages, err := calendar.Languages()
	if err != nil {
		t.Fatalf("Languages: %v", err)
	}
	if languages == nil {
		t.Fatal("Languages returned a nil vector view")
	}
	defer languages.Release()

	size, err := languages.Size()
	if err != nil {
		t.Fatalf("IVectorViewOfString.Size: %v", err)
	}
	if size == 0 {
		t.Fatal("Languages vector is empty; the system always reports at least one language")
	}
	first, err := languages.GetAt(0)
	if err != nil {
		t.Fatalf("IVectorViewOfString.GetAt(0): %v", err)
	}
	if first == "" {
		t.Fatal("first language tag is empty")
	}
	t.Logf("languages: %d entries, first = %s (through generated dispatch)", size, first)

	// IndexOf round-trips the tag we just read (string in-param + non-retval
	// out-param through the instantiated interface).
	var index uint32
	found, err := languages.IndexOf(first, &index)
	if err != nil {
		t.Fatalf("IVectorViewOfString.IndexOf: %v", err)
	}
	if !found || index != 0 {
		t.Errorf("IndexOf(%q) = %v at %d, want found at 0", first, found, index)
	}
}
