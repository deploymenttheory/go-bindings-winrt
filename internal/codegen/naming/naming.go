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

// PackagePath converts a namespace ("Windows.Foundation.Collections") to the
// generated package's directory path below the output root
// ("foundation/collections").
func PackagePath(namespace string) string {
	segments := strings.Split(stripRoot(namespace), ".")
	for i, segment := range segments {
		segments[i] = strings.ToLower(segment)
	}
	return strings.Join(segments, "/")
}

// PackageName returns the Go package name for a namespace: the lowercased
// final segment ("Windows.Globalization" → "globalization").
func PackageName(namespace string) string {
	segments := strings.Split(namespace, ".")
	return strings.ToLower(segments[len(segments)-1])
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
	trimmed := interfaceName
	if len(trimmed) >= 2 && trimmed[0] == 'I' && trimmed[1] >= 'A' && trimmed[1] <= 'Z' {
		trimmed = trimmed[1:]
	}
	return "As" + Export(trimmed)
}
