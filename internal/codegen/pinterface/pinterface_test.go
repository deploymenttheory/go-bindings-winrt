package pinterface

import (
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/deploymenttheory/go-bindings-winrt/internal/codegen/pipeline"
	"github.com/deploymenttheory/go-bindings-winrt/internal/winrtmeta"
)

var registryState struct {
	once     sync.Once
	registry *pipeline.Registry
	err      error
}

func testRegistry(t *testing.T) *pipeline.Registry {
	t.Helper()
	registryState.once.Do(func() {
		registryState.registry, registryState.err = pipeline.LoadAll(filepath.Join("..", "..", "..", "metadata", "winrt"))
	})
	if registryState.err != nil {
		t.Fatalf("loading committed IR: %v", registryState.err)
	}
	return registryState.registry
}

func genericInst(namespace, name string, args ...winrtmeta.TypeRef) *winrtmeta.TypeRef {
	return &winrtmeta.TypeRef{Kind: "GenericInst", Namespace: namespace, Name: name, Args: args}
}

func native(name string) winrtmeta.TypeRef {
	return winrtmeta.TypeRef{Kind: "Native", Name: name}
}

// TestIIterableOfStringIID pins the algorithm to the ecosystem-wide known
// IID for IIterable<String> — the same value every projection derives.
func TestIIterableOfStringIID(t *testing.T) {
	registry := testRegistry(t)

	inst := genericInst("Windows.Foundation.Collections", "IIterable`1", native("HString"))
	signature, err := Signature(inst, registry)
	if err != nil {
		t.Fatalf("Signature: %v", err)
	}
	// The open IIterable`1 IID is faa585ea-6214-4217-afda-7f46de5869b3.
	if want := "pinterface({faa585ea-6214-4217-afda-7f46de5869b3};string)"; signature != want {
		t.Errorf("signature = %q, want %q", signature, want)
	}
	iid, err := InstanceIID(inst, registry)
	if err != nil {
		t.Fatalf("InstanceIID: %v", err)
	}
	if want := "e2fcc7c1-3bfc-5a0b-b2b0-72e769d1cb7e"; iid != want {
		t.Errorf("IIterable<String> IID = %s, want %s", iid, want)
	}
}

// TestSignatureShapes exercises each grammar production against real IR.
func TestSignatureShapes(t *testing.T) {
	registry := testRegistry(t)

	sig := func(ref *winrtmeta.TypeRef) string {
		t.Helper()
		s, err := Signature(ref, registry)
		if err != nil {
			t.Fatalf("Signature(%+v): %v", ref, err)
		}
		return s
	}

	// Enum.
	if got := sig(&winrtmeta.TypeRef{Kind: "ApiRef", Namespace: "Windows.Foundation", Name: "AsyncStatus", TargetKind: "Enum"}); got != "enum(Windows.Foundation.AsyncStatus;i4)" {
		t.Errorf("enum signature = %q", got)
	}
	// Struct with recursive fields.
	if got := sig(&winrtmeta.TypeRef{Kind: "ApiRef", Namespace: "Windows.Foundation", Name: "DateTime", TargetKind: "Struct"}); got != "struct(Windows.Foundation.DateTime;i8)" {
		t.Errorf("struct signature = %q", got)
	}
	// Non-generic interface.
	got := sig(&winrtmeta.TypeRef{Kind: "ApiRef", Namespace: "Windows.Globalization", Name: "ICalendar", TargetKind: "Interface"})
	if got != "{ca30221d-86d9-40fb-a26b-d44eb7cf08ea}" {
		t.Errorf("interface signature = %q", got)
	}
	// Runtime class → rc(name;{default-iid}).
	got = sig(&winrtmeta.TypeRef{Kind: "ApiRef", Namespace: "Windows.Globalization", Name: "Calendar", TargetKind: "Class"})
	if got != "rc(Windows.Globalization.Calendar;{ca30221d-86d9-40fb-a26b-d44eb7cf08ea})" {
		t.Errorf("class signature = %q", got)
	}
	// Non-generic delegate.
	got = sig(&winrtmeta.TypeRef{Kind: "ApiRef", Namespace: "Windows.Media.Protection", Name: "RebootNeededEventHandler", TargetKind: "Delegate"})
	if got != "delegate({64e12a45-973b-4a3a-b260-91898a49a82c})" {
		t.Errorf("delegate signature = %q", got)
	}
	// Object.
	if got := sig(&winrtmeta.TypeRef{Kind: "Native", Name: "Object"}); got != "cinterface(IInspectable)" {
		t.Errorf("object signature = %q", got)
	}
}

// TestNestedInstantiation grounds a pinterface inside a pinterface
// (TypedEventHandler<DeviceWatcher, Object> style shapes appear all over the
// event surface).
func TestNestedInstantiation(t *testing.T) {
	registry := testRegistry(t)

	inner := genericInst("Windows.Foundation.Collections", "IIterable`1", native("HString"))
	outer := genericInst("Windows.Foundation.Collections", "IIterable`1", *inner)
	signature, err := Signature(outer, registry)
	if err != nil {
		t.Fatalf("Signature: %v", err)
	}
	want := "pinterface({faa585ea-6214-4217-afda-7f46de5869b3};pinterface({faa585ea-6214-4217-afda-7f46de5869b3};string))"
	if signature != want {
		t.Errorf("nested signature = %q, want %q", signature, want)
	}
	if _, err := InstanceIID(outer, registry); err != nil {
		t.Fatalf("nested InstanceIID: %v", err)
	}
}

// TestUUIDv5Shape checks version/variant bits and determinism.
func TestUUIDv5Shape(t *testing.T) {
	a := uuidV5(wrtPinterfaceNamespace, "pinterface({faa585ea-6214-4217-afda-7f46de5869b3};string)")
	b := uuidV5(wrtPinterfaceNamespace, "pinterface({faa585ea-6214-4217-afda-7f46de5869b3};string)")
	if a != b {
		t.Fatalf("not deterministic: %s vs %s", a, b)
	}
	if a[14] != '5' {
		t.Errorf("version nibble = %c, want 5 (%s)", a[14], a)
	}
	if v := a[19]; v != '8' && v != '9' && v != 'a' && v != 'b' {
		t.Errorf("variant nibble = %c, want 8/9/a/b (%s)", v, a)
	}
	if strings.ToLower(a) != a {
		t.Errorf("not lowercase: %s", a)
	}
}

// TestUngroundable rejects unbound generic params and arrays.
func TestUngroundable(t *testing.T) {
	registry := testRegistry(t)
	if _, err := Signature(&winrtmeta.TypeRef{Kind: "GenericParamRef", Index: 0}, registry); err == nil {
		t.Error("unbound generic param grounded")
	}
	elem := native("I4")
	if _, err := Signature(&winrtmeta.TypeRef{Kind: "Array", Elem: &elem}, registry); err == nil {
		t.Error("array grounded")
	}
}
