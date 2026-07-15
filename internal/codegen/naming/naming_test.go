package naming

import "testing"

// TestPackageNameKeywordEscape pins the keyword escaping: a namespace segment
// that lowercases to a Go keyword cannot be a package clause
// ("package import" does not parse), so it takes a trailing underscore —
// and the directory path must match the package name.
func TestPackageNameKeywordEscape(t *testing.T) {
	cases := []struct {
		namespace string
		wantName  string
		wantPath  string
	}{
		{"Windows.Media.Import", "import_", "media/import_"},
		{"Windows.Globalization", "globalization", "globalization"},
		{"Windows.Foundation.Collections", "collections", "foundation/collections"},
		// Predeclared identifiers are legal package names — no escaping.
		{"Windows.Data.Text", "text", "data/text"},
	}
	for _, testCase := range cases {
		if got := PackageName(testCase.namespace); got != testCase.wantName {
			t.Errorf("PackageName(%q) = %q, want %q", testCase.namespace, got, testCase.wantName)
		}
		if got := PackagePath(testCase.namespace); got != testCase.wantPath {
			t.Errorf("PackagePath(%q) = %q, want %q", testCase.namespace, got, testCase.wantPath)
		}
	}
}

// TestImportAliasStaysUniquePerNamespace pins the alias rule: all
// root-stripped segments joined, so namespaces sharing a leaf still get
// distinct aliases.
func TestImportAliasStaysUniquePerNamespace(t *testing.T) {
	if a, b := ImportAlias("Windows.Devices.Power"), ImportAlias("Windows.System.Power"); a == b {
		t.Fatalf("aliases collide: %q", a)
	}
	if got := ImportAlias("Windows.Foundation.Collections"); got != "foundationcollections" {
		t.Fatalf("ImportAlias = %q", got)
	}
}
