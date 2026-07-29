package typemap

import "github.com/deploymenttheory/go-bindings-winrt/internal/winrtmeta"

// The amd64 C layout model, and the by-value parameter classification it
// exists to answer.
//
// Windows x64 passes an aggregate parameter one of two ways, decided purely
// by its SIZE: an aggregate of 1, 2, 4 or 8 bytes travels in a general
// purpose register as an integer of that width, and an aggregate of any
// other size travels as a POINTER to a caller-owned temporary. Unlike the
// System V ABI, MSVC never places an aggregate in an XMM register whatever
// its field types, so a two-float Point is an 8-byte integer word rather
// than two floats. Both cases are expressible through syscall.SyscallN,
// which is why this package can classify by-value structs rather than skip
// them.
//
// Sizes are computed rather than measured because the decision is needed at
// generate time. That is sound here: emittable WinRT structs contain only
// scalars, floats, bools, GUIDs, enums and nested structs (see
// StructEmittable), and Go lays all of those out with the same natural
// alignment rules as C, so the computed size equals unsafe.Sizeof of the
// emitted Go type. layout_test.go pins that against the real metadata.

// abiPointerSize is the amd64 pointer width — the one place the
// architecture assumption is written down.
const abiPointerSize = 8

// win32.GUID is {uint32, uint16, uint16, [8]byte}: sixteen bytes aligned to
// four, not to eight.
const (
	guidSize  = 16
	guidAlign = 4
)

// Layout is a type's amd64 size and alignment, in bytes.
type Layout struct {
	Size  int
	Align int
}

// ParamClass says how a by-value aggregate parameter reaches the callee.
type ParamClass uint8

const (
	// ParamInline: 1, 2, 4 or 8 bytes — the aggregate's bytes travel in a
	// general purpose register, zero-extended to the register width.
	ParamInline ParamClass = iota
	// ParamByRef: any other size — a pointer to a caller-owned temporary
	// travels instead. The callee reads through it for the duration of the
	// call and does not retain it.
	ParamByRef
)

// ClassifyAggregate returns how an aggregate of the given size is passed.
func ClassifyAggregate(size int) ParamClass {
	switch size {
	case 1, 2, 4, 8:
		return ParamInline
	}
	return ParamByRef
}

// StructLayout computes a value struct's amd64 layout. ok is false when the
// struct is unknown, empty, or holds a field this model cannot size — the
// caller must then degrade rather than guess a calling convention.
func (m *Mapper) StructLayout(namespace, name string) (Layout, bool) {
	return m.structLayoutIn(namespace, name, map[string]bool{})
}

type layoutResult struct {
	layout Layout
	ok     bool
}

// structLayoutIn is StructLayout with a cycle guard: a struct that, per
// invalid metadata, transitively contains itself has no layout and must not
// recurse forever discovering that.
func (m *Mapper) structLayoutIn(namespace, name string, visiting map[string]bool) (Layout, bool) {
	key := namespace + "." + name
	if cached, seen := m.structLayout[key]; seen {
		return cached.layout, cached.ok
	}
	if visiting[key] {
		return Layout{}, false
	}
	visiting[key] = true
	defer delete(visiting, key)

	definition := m.Registry.Struct(namespace, name)
	if definition == nil {
		return Layout{}, false
	}

	// A C struct's alignment is the widest of its fields'; its size is the
	// offset past the last field, rounded up to that alignment. An empty
	// struct occupies a byte in C, but WinRT declares none, so that
	// degenerate case is reported unsizeable rather than guessed.
	size, align := 0, 1
	ok := len(definition.Fields) > 0
	for i := range definition.Fields {
		fieldLayout, fieldOK := m.fieldLayout(&definition.Fields[i], namespace, visiting)
		if !fieldOK {
			ok = false
			break
		}
		if fieldLayout.Align > align {
			align = fieldLayout.Align
		}
		size = roundUp(size, fieldLayout.Align) + fieldLayout.Size
	}

	result := layoutResult{ok: ok}
	if ok {
		result.layout = Layout{Size: roundUp(size, align), Align: align}
	}
	if m.structLayout == nil {
		m.structLayout = map[string]layoutResult{}
	}
	m.structLayout[key] = result
	return result.layout, result.ok
}

// fieldLayout sizes one struct field.
func (m *Mapper) fieldLayout(field *winrtmeta.StructField, namespace string, visiting map[string]bool) (Layout, bool) {
	scratch := ImportSet{}
	resolved := m.GoType(&field.Type, Context{Namespace: namespace}, scratch)
	switch resolved.Kind {
	case KindBool:
		// WinRT boolean is one byte at the ABI, which Go bool matches.
		return Layout{Size: 1, Align: 1}, true
	case KindGUID:
		return Layout{Size: guidSize, Align: guidAlign}, true
	case KindScalar, KindFloat:
		return selfAligned(scalarSize(resolved.GoType))
	case KindEnum:
		// An enum resolves to a named Go type, so its width comes from the
		// declared integral base rather than from the rendered type name.
		return selfAligned(scalarSize(m.Registry.EnumBase(field.Type.Namespace, field.Type.Name)))
	case KindStruct:
		return m.structLayoutIn(resolved.StructNamespace, resolved.StructName, visiting)
	}
	return Layout{}, false
}

// selfAligned turns a scalar width into a layout: every scalar admitted here
// is aligned to its own size.
func selfAligned(size int, ok bool) (Layout, bool) {
	if !ok {
		return Layout{}, false
	}
	return Layout{Size: size, Align: size}, true
}

// scalarSize is the amd64 width of a Go scalar type name. Anything
// unrecognized is reported rather than assumed.
func scalarSize(goType string) (int, bool) {
	switch goType {
	case "int8", "uint8", "byte":
		return 1, true
	case "int16", "uint16":
		return 2, true
	case "int32", "uint32", "float32":
		return 4, true
	case "int64", "uint64", "float64":
		return 8, true
	case "uintptr":
		return abiPointerSize, true
	}
	return 0, false
}

// roundUp rounds offset up to the next multiple of align.
func roundUp(offset, align int) int {
	if align <= 1 {
		return offset
	}
	if remainder := offset % align; remainder != 0 {
		return offset + align - remainder
	}
	return offset
}
