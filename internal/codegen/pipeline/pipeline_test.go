package pipeline

import (
	"testing"

	"github.com/deploymenttheory/go-bindings-winrt/internal/winrtmeta"
)

// twoNamespaceFixture builds a minimal cyclic IR: A.IThing requires B.IOther
// and B.IOther requires A.IThing, with B.Widget's default interface in A
// (the heavyweight edge the cycle breaker must preserve).
func twoNamespaceFixture(t *testing.T, dir string) {
	t.Helper()
	ref := func(namespace, name, kind string) winrtmeta.TypeRef {
		return winrtmeta.TypeRef{Kind: "ApiRef", Namespace: namespace, Name: name, TargetKind: kind}
	}
	a := &winrtmeta.NamespaceMeta{
		Namespace: "Windows.TestA",
		Interfaces: map[string]winrtmeta.Interface{
			"IThing": {GUID: "11111111-1111-1111-1111-111111111111",
				Requires: []winrtmeta.TypeRef{ref("Windows.TestB", "IOther", "Interface")}},
		},
		Enums: map[string]winrtmeta.Enum{
			"Mode": {BaseType: "int32", Members: []winrtmeta.EnumMember{{Name: "Off", Value: "0"}}},
		},
	}
	defaultInterface := ref("Windows.TestA", "IThing", "Interface")
	b := &winrtmeta.NamespaceMeta{
		Namespace: "Windows.TestB",
		Interfaces: map[string]winrtmeta.Interface{
			"IOther": {GUID: "22222222-2222-2222-2222-222222222222",
				Requires: []winrtmeta.TypeRef{ref("Windows.TestA", "IThing", "Interface")}},
		},
		Classes: map[string]winrtmeta.Class{
			"Widget": {DefaultInterface: &defaultInterface,
				Interfaces: []winrtmeta.TypeRef{defaultInterface}},
		},
	}
	for _, meta := range []*winrtmeta.NamespaceMeta{a, b} {
		if err := winrtmeta.Write(dir, meta); err != nil {
			t.Fatalf("Write %s: %v", meta.Namespace, err)
		}
	}
}

func TestLoadAllAndCycleBreaking(t *testing.T) {
	dir := t.TempDir()
	twoNamespaceFixture(t, dir)

	registry, err := LoadAll(dir)
	if err != nil {
		t.Fatalf("LoadAll: %v", err)
	}
	if len(registry.Namespaces) != 2 {
		t.Fatalf("namespaces = %d, want 2", len(registry.Namespaces))
	}
	if registry.Interface("Windows.TestA", "IThing") == nil {
		t.Error("Windows.TestA.IThing not indexed")
	}
	if registry.Class("Windows.TestB", "Widget") == nil {
		t.Error("Windows.TestB.Widget not indexed")
	}
	if base := registry.EnumBase("Windows.TestA", "Mode"); base != "int32" {
		t.Errorf("EnumBase = %q, want int32", base)
	}

	// The A→B and B→A reference edges form a cycle; B→A carries the
	// default-interface embedding bonus, so A→B must be the severed edge.
	blocked := ComputeBlockedImports(registry)
	if !blocked["Windows.TestA"]["Windows.TestB"] {
		t.Errorf("blocked = %v, want Windows.TestA → Windows.TestB severed", blocked)
	}
	if blocked["Windows.TestB"]["Windows.TestA"] {
		t.Errorf("default-interface edge Windows.TestB → Windows.TestA must survive, got blocked = %v", blocked)
	}
}

func TestLoadAllEmptyDir(t *testing.T) {
	if _, err := LoadAll(t.TempDir()); err == nil {
		t.Fatal("LoadAll over an empty dir must error (run 'generate ingest')")
	}
}
