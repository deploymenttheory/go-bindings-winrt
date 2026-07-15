// Package naming holds the WinRT → Go naming rules shared by all emitters.
package naming

import "strings"

// goReservedWords are identifiers a parameter may not use: Go keywords plus
// predeclared identifiers and names the generated code itself binds (imports
// and locals in generated bodies).
var goReservedWords = map[string]bool{
	// keywords
	"break": true, "case": true, "chan": true, "const": true, "continue": true,
	"default": true, "defer": true, "else": true, "fallthrough": true, "for": true,
	"func": true, "go": true, "goto": true, "if": true, "import": true,
	"interface": true, "map": true, "package": true, "range": true, "return": true,
	"select": true, "struct": true, "switch": true, "type": true, "var": true,
	// predeclared
	"any": true, "bool": true, "byte": true, "error": true, "int": true,
	"int8": true, "int16": true, "int32": true, "int64": true, "rune": true,
	"string": true, "uint": true, "uint8": true, "uint16": true, "uint32": true,
	"uint64": true, "uintptr": true, "float32": true, "float64": true,
	"true": true, "false": true, "nil": true, "len": true, "cap": true,
	"new": true, "make": true, "copy": true, "append": true, "panic": true,
	"recover": true, "print": true, "println": true, "close": true, "delete": true,
	"complex": true, "complex64": true, "complex128": true, "imag": true, "real": true,
	"min": true, "max": true, "clear": true,
	// names bound by generated code
	"unsafe": true, "syscall": true, "win32": true, "syswinrt": true,
	"winrt": true, "err": true, "r1": true, "result": true, "instance": true,
	"factory": true, "factoryUnknown": true, // factory-constructor locals
	"self": true, // method receiver
}

// Export makes a metadata identifier usable as an exported Go package-level
// name: leading underscores are trimmed and the first letter is capitalized.
// Case-collapsed collisions are caught by the generator's per-package name
// claims.
func Export(name string) string {
	name = strings.TrimLeft(name, "_")
	if name == "" {
		return "X"
	}
	return strings.ToUpper(name[:1]) + name[1:]
}

// ParamName escapes a metadata parameter name for use as a Go parameter.
func ParamName(name string) string {
	if name == "" {
		return "param"
	}
	if goReservedWords[name] {
		return name + "_"
	}
	return name
}

// stripRoot removes the "Windows." root every WinRT namespace carries; the
// generated tree lives under bindings/winrt/, so the root is redundant.
func stripRoot(namespace string) string {
	return strings.TrimPrefix(namespace, "Windows.")
}

// goKeywords are the Go keywords proper — a package clause may not use one
// ("package import" does not parse), so a namespace segment that lowercases
// to a keyword takes a trailing underscore (Windows.Media.Import →
// package import_ in media/import_). Predeclared identifiers (string, error)
// are fine as package names and stay untouched.
var goKeywords = map[string]bool{
	"break": true, "case": true, "chan": true, "const": true, "continue": true,
	"default": true, "defer": true, "else": true, "fallthrough": true, "for": true,
	"func": true, "go": true, "goto": true, "if": true, "import": true,
	"interface": true, "map": true, "package": true, "range": true, "return": true,
	"select": true, "struct": true, "switch": true, "type": true, "var": true,
}

// packageSegment lowercases one namespace segment and escapes Go keywords so
// the segment is always usable as a package name (and its directory matches).
func packageSegment(segment string) string {
	segment = strings.ToLower(segment)
	if goKeywords[segment] {
		return segment + "_"
	}
	return segment
}

// PackagePath converts a namespace ("Windows.Foundation.Collections") to the
// generated package's directory path below the output root
// ("foundation/collections"). Each segment goes through the same keyword
// escaping as PackageName so the directory always matches the package clause.
func PackagePath(namespace string) string {
	segments := strings.Split(stripRoot(namespace), ".")
	for i, segment := range segments {
		segments[i] = packageSegment(segment)
	}
	return strings.Join(segments, "/")
}

// PackageName returns the Go package name for a namespace: the lowercased
// final segment ("Windows.Globalization" → "globalization"), keyword-escaped
// (Windows.Media.Import → "import_").
func PackageName(namespace string) string {
	segments := strings.Split(namespace, ".")
	return packageSegment(segments[len(segments)-1])
}

// ImportAlias returns the alias generated files use for a cross-namespace
// import. Namespaces can share a leaf name, so the alias joins every
// root-stripped segment ("Windows.Foundation.Collections" →
// "foundationcollections"), keeping aliases unique per full namespace.
func ImportAlias(namespace string) string {
	segments := strings.Split(stripRoot(namespace), ".")
	return strings.ToLower(strings.Join(segments, ""))
}

// InterfaceAsName is the runtime-class query-method name for an interface:
// "As" plus the interface name with its I prefix stripped
// (ITimeZoneOnCalendar → AsTimeZoneOnCalendar).
func InterfaceAsName(interfaceName string) string {
	return "As" + Export(trimInterfacePrefix(interfaceName))
}

// StaticsAccessorName is the package-level accessor name for a class's
// statics interface: the interface name with its I prefix stripped
// (ICalendarIdentifiersStatics → CalendarIdentifiersStatics).
func StaticsAccessorName(interfaceName string) string {
	return Export(trimInterfacePrefix(interfaceName))
}

// trimInterfacePrefix strips the WinRT interface I prefix when the name
// follows the ICapitalized convention.
func trimInterfacePrefix(interfaceName string) string {
	if len(interfaceName) >= 2 && interfaceName[0] == 'I' && interfaceName[1] >= 'A' && interfaceName[1] <= 'Z' {
		return interfaceName[1:]
	}
	return interfaceName
}
