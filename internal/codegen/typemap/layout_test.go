package typemap

import (
	"testing"

	"github.com/deploymenttheory/go-bindings-winrt/internal/codegen/pipeline"
	"github.com/deploymenttheory/go-bindings-winrt/internal/winrtmeta"
)

func native(name string) winrtmeta.TypeRef { return winrtmeta.TypeRef{Kind: "Native", Name: name} }

func structRef(name string) winrtmeta.TypeRef {
	return winrtmeta.TypeRef{Kind: "ApiRef", Namespace: "Windows.Test", Name: name, TargetKind: "Struct"}
}

func enumRef(name string) winrtmeta.TypeRef {
	return winrtmeta.TypeRef{Kind: "ApiRef", Namespace: "Windows.Test", Name: name, TargetKind: "Enum"}
}

func field(name string, ref winrtmeta.TypeRef) winrtmeta.StructField {
	return winrtmeta.StructField{Name: name, Type: ref}
}

// layoutMapper builds a Mapper over structs mirroring the real WinRT shapes
// whose sizes decide the by-value calling convention.
func layoutMapper(t *testing.T) *Mapper {
	t.Helper()
	f32, f64 := native("F32"), native("F64")
	u1, i4, i8 := native("U1"), native("I4"), native("I8")

	structs := map[string]winrtmeta.Struct{
		// Real WinRT structs, by size class.
		"DateTime":  {Fields: []winrtmeta.StructField{field("UniversalTime", i8)}},
		"Point":     {Fields: []winrtmeta.StructField{field("X", f32), field("Y", f32)}},
		"Color":     {Fields: []winrtmeta.StructField{field("A", u1), field("R", u1), field("G", u1), field("B", u1)}},
		"Rect":      {Fields: []winrtmeta.StructField{field("X", f32), field("Y", f32), field("Width", f32), field("Height", f32)}},
		"Vector3":   {Fields: []winrtmeta.StructField{field("X", f32), field("Y", f32), field("Z", f32)}},
		"Thickness": {Fields: []winrtmeta.StructField{field("Left", f64), field("Top", f64), field("Right", f64), field("Bottom", f64)}},

		// Alignment and tail-padding cases.
		"BytePlusInt":  {Fields: []winrtmeta.StructField{field("Flag", u1), field("Value", i4)}},
		"LongPlusByte": {Fields: []winrtmeta.StructField{field("Value", i8), field("Flag", u1)}},
		"Byte":         {Fields: []winrtmeta.StructField{field("Value", u1)}},
		"Short":        {Fields: []winrtmeta.StructField{field("Value", native("I2"))}},

		// Composition: a GUID field, an enum field, a nested struct.
		"WithGUID":   {Fields: []winrtmeta.StructField{field("ID", native("Guid"))}},
		"WithEnum":   {Fields: []winrtmeta.StructField{field("Kind", enumRef("Flavour")), field("Value", i4)}},
		"WithNested": {Fields: []winrtmeta.StructField{field("Origin", structRef("Point")), field("Extent", structRef("Point"))}},

		// Degenerate shapes that must report "no layout" rather than guess.
		"Empty":     {},
		"SelfCycle": {Fields: []winrtmeta.StructField{field("Next", structRef("SelfCycle"))}},
	}
	enums := map[string]winrtmeta.Enum{
		"Flavour": {BaseType: "int32"},
	}

	meta := &winrtmeta.NamespaceMeta{Namespace: "Windows.Test", Structs: structs, Enums: enums}
	registry := &pipeline.Registry{
		Namespaces:     []*winrtmeta.NamespaceMeta{meta},
		ByNamespace:    map[string]*winrtmeta.NamespaceMeta{"Windows.Test": meta},
		EnumIndex:      map[string]*winrtmeta.Enum{},
		StructIndex:    map[string]*winrtmeta.Struct{},
		InterfaceIndex: map[string]*winrtmeta.Interface{},
		ClassIndex:     map[string]*winrtmeta.Class{},
		DelegateIndex:  map[string]*winrtmeta.Delegate{},
	}
	for name := range structs {
		definition := structs[name]
		registry.StructIndex["Windows.Test."+name] = &definition
	}
	for name := range enums {
		definition := enums[name]
		registry.EnumIndex["Windows.Test."+name] = &definition
	}
	return &Mapper{Registry: registry, ModulePath: "example.com/mod"}
}

func TestStructLayout(t *testing.T) {
	mapper := layoutMapper(t)
	for _, testCase := range []struct {
		name  string
		size  int
		align int
	}{
		{name: "DateTime", size: 8, align: 8},
		{name: "Point", size: 8, align: 4},
		{name: "Color", size: 4, align: 1},
		{name: "Rect", size: 16, align: 4},
		{name: "Vector3", size: 12, align: 4},
		{name: "Thickness", size: 32, align: 8},
		{name: "Byte", size: 1, align: 1},
		{name: "Short", size: 2, align: 2},
		// One byte, three bytes of padding, then the int.
		{name: "BytePlusInt", size: 8, align: 4},
		// Eight bytes, one byte, then seven bytes of TAIL padding to keep
		// the struct's own alignment — the case a naive sum gets wrong.
		{name: "LongPlusByte", size: 16, align: 8},
		// GUID is sixteen bytes aligned to four, not to eight.
		{name: "WithGUID", size: 16, align: 4},
		// The enum contributes its declared int32 base, not its type name.
		{name: "WithEnum", size: 8, align: 4},
		{name: "WithNested", size: 16, align: 4},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			layout, ok := mapper.StructLayout("Windows.Test", testCase.name)
			if !ok {
				t.Fatalf("StructLayout(%s) reported no layout", testCase.name)
			}
			if layout.Size != testCase.size || layout.Align != testCase.align {
				t.Errorf("StructLayout(%s) = %+v, want {Size:%d Align:%d}",
					testCase.name, layout, testCase.size, testCase.align)
			}
		})
	}
}

func TestStructLayoutUnsizeable(t *testing.T) {
	mapper := layoutMapper(t)
	for _, name := range []string{"Empty", "SelfCycle", "NoSuchStruct"} {
		if layout, ok := mapper.StructLayout("Windows.Test", name); ok {
			t.Errorf("StructLayout(%s) = %+v, want no layout", name, layout)
		}
	}
}

// TestStructLayoutMemoized guards the cache: a second call must agree with
// the first, and the cycle guard must not have poisoned it.
func TestStructLayoutMemoized(t *testing.T) {
	mapper := layoutMapper(t)
	first, ok := mapper.StructLayout("Windows.Test", "WithNested")
	if !ok {
		t.Fatal("first StructLayout reported no layout")
	}
	second, ok := mapper.StructLayout("Windows.Test", "WithNested")
	if !ok {
		t.Fatal("second StructLayout reported no layout")
	}
	if first != second {
		t.Errorf("memoized layout %+v differs from %+v", second, first)
	}
}

func TestClassifyAggregate(t *testing.T) {
	// Windows x64 keys on size alone: 1/2/4/8 travel in a general purpose
	// register, everything else as a pointer to a caller-owned temporary.
	for _, size := range []int{1, 2, 4, 8} {
		if got := ClassifyAggregate(size); got != ParamInline {
			t.Errorf("ClassifyAggregate(%d) = %v, want ParamInline", size, got)
		}
	}
	for _, size := range []int{0, 3, 5, 6, 7, 12, 16, 24, 64} {
		if got := ClassifyAggregate(size); got != ParamByRef {
			t.Errorf("ClassifyAggregate(%d) = %v, want ParamByRef", size, got)
		}
	}
}

func TestRoundUp(t *testing.T) {
	for _, testCase := range []struct{ offset, align, want int }{
		{0, 1, 0}, {1, 1, 1}, {1, 4, 4}, {4, 4, 4}, {5, 4, 8}, {9, 8, 16}, {3, 0, 3},
	} {
		if got := roundUp(testCase.offset, testCase.align); got != testCase.want {
			t.Errorf("roundUp(%d, %d) = %d, want %d",
				testCase.offset, testCase.align, got, testCase.want)
		}
	}
}
