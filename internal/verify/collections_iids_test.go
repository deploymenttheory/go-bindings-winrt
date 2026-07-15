//go:build windows && (amd64 || arm64)

package verify

import (
	"path/filepath"
	"testing"

	win32 "github.com/deploymenttheory/go-bindings-win32/bindings/runtime/win32"

	"github.com/deploymenttheory/go-bindings-winrt/bindings/runtime/winrt"
	"github.com/deploymenttheory/go-bindings-winrt/internal/codegen/pinterface"
	"github.com/deploymenttheory/go-bindings-winrt/internal/codegen/pipeline"
	"github.com/deploymenttheory/go-bindings-winrt/internal/winrtmeta"
)

// The Go-implemented string collections (bindings/runtime/winrt/
// collections.go) hard-code their pinterface IIDs. This test pins those
// constants against a fresh derivation through the pinterface engine over
// the committed IR, so a transcription mistake or an algorithm change fails
// here instead of in a silently-rejected live QueryInterface.
func TestCollectionsIIDsMatchDerivation(t *testing.T) {
	registry, err := pipeline.LoadAll(filepath.Join("..", "..", "metadata", "winrt"))
	if err != nil {
		t.Fatalf("loading committed IR: %v", err)
	}
	hstringArg := winrtmeta.TypeRef{Kind: "Native", Name: "HString"}
	for name, runtime := range map[string]win32.GUID{
		"IIterable`1":   winrt.IIDIterableOfString,
		"IIterator`1":   winrt.IIDIteratorOfString,
		"IVectorView`1": winrt.IIDVectorViewOfString,
	} {
		inst := &winrtmeta.TypeRef{
			Kind: "GenericInst", Namespace: "Windows.Foundation.Collections",
			Name: name, Args: []winrtmeta.TypeRef{hstringArg},
		}
		derived, err := pinterface.InstanceIID(inst, registry)
		if err != nil {
			t.Fatalf("InstanceIID(%s<String>): %v", name, err)
		}
		guid, err := winrt.ParseGUID(derived)
		if err != nil {
			t.Fatalf("ParseGUID(%s): %v", derived, err)
		}
		if guid != runtime {
			t.Errorf("%s<String>: runtime constant %s, derivation says %s", name, runtime, guid)
		}
	}
}
