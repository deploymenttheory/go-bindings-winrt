package emitwinrt

// Collection-constructor synthesis. Every monomorphized IIterable`1<X> /
// IVectorView`1<X> / IVector`1<X> whose ELEMENT grounds to one of the
// runtime's element codecs (string, interface/class/Object pointer, enum,
// integer scalar, GUID) gains a package-level constructor
//
//	func New<Mangled>(items []<GoElem>) *<Mangled>
//
// that boxes the typed slice into the runtime collection core's payload,
// selects the codec, wires the pinterface-derived IIDs, and returns the
// Go-implemented object as the package-local consumer type via the layout
// cast (both are a single vtable-pointer word; Release works through vtable
// slot 2). The sibling instantiations the object spawns at runtime
// (IIterator`1<X> from First; IIterable`1<X> for the tear-off facet;
// IVectorView`1<X> from IVector.GetView) are REQUESTED through the existing
// substitution machinery so their types and IID_ vars exist in-package.
//
// Elements with no codec (bool, floats, structs, delegates, unresolvable
// references) skip the constructor — the interface type itself still emits —
// under the soft `collection-ctor-skipped` diagnostic.

import (
	"fmt"
	"strings"

	"github.com/deploymenttheory/go-bindings-winrt/internal/codegen/emit/winrt/view"
	"github.com/deploymenttheory/go-bindings-winrt/internal/codegen/typemap"
	"github.com/deploymenttheory/go-bindings-winrt/internal/winrtmeta"
)

// scalarElementSizes maps the IR Native names of codec-able integer scalars
// to their ABI element size (the CodecScalar argument). Bool is deliberately
// absent: its 1-byte ABI slot invites silent truncation bugs against Go
// bool, and no live consumer needs it this wave.
var scalarElementSizes = map[string]uintptr{
	"U1":     1,
	"Char16": 2,
	"I2":     2,
	"U2":     2,
	"I4":     4,
	"U4":     4,
	"I8":     8,
	"U8":     8,
}

// collectionShapes maps the open Windows.Foundation.Collections interface
// names that gain constructors to their runtime core constructor.
var collectionShapes = map[string]string{
	"IIterable`1":   "NewIterableObject",
	"IVectorView`1": "NewVectorViewObject",
	"IVector`1":     "NewVectorObject",
}

// attachCollectionCtor synthesizes the constructor view model onto a
// just-built collection instantiation model. Non-codec-able elements and
// ungroundable siblings record collection-ctor-skipped and leave the model
// untouched (the type still emits).
func (g *Generator) attachCollectionCtor(meta *winrtmeta.NamespaceMeta, model *view.InterfaceModel, inst *winrtmeta.TypeRef, imports typemap.ImportSet) {
	runtimeCtor := collectionShapes[inst.Name]
	if runtimeCtor == "" || inst.Namespace != "Windows.Foundation.Collections" || len(inst.Args) != 1 {
		return
	}
	if model.IIDVar == "" {
		g.diag("collection-ctor-skipped", "%s (no IID var to wire)", model.FullName)
		return
	}
	elem := &inst.Args[0]
	context := g.resolveContext(meta.Namespace)
	scratch := typemap.ImportSet{}
	resolved := g.mapper.GoType(elem, context, scratch)

	ctor := view.CollectionCtorModel{
		CtorName:    "New" + model.TypeName,
		ElemType:    resolved.GoType,
		RuntimeCtor: runtimeCtor,
		Class:       model.FullName,
	}
	var elemNotes []string
	switch resolved.Kind {
	case typemap.KindString:
		ctor.Codec = "winrt.CodecString"
		ctor.BoxExpr = "item"
		elemNotes = []string{
			"Items are copied; IndexOf compares string values.",
		}
	case typemap.KindInterfacePtr, typemap.KindObjectPtr:
		ctor.Codec = "winrt.CodecInterface"
		ctor.BoxExpr = "uintptr(unsafe.Pointer(item))"
		elemNotes = []string{
			"Items are BORROWED: the collection AddRefs each element and releases it",
			"as it is displaced, removed, or when the collection itself is released.",
			"IndexOf compares COM identity WORDS (no QueryInterface is issued): an",
			"element matches only the exact interface pointer it was built from.",
		}
	case typemap.KindEnum:
		ctor.Codec = "winrt.CodecScalar(4)"
		ctor.BoxExpr = "uint64(item)"
	case typemap.KindScalar:
		size := scalarElementSize(elem)
		if size == 0 {
			g.diag("collection-ctor-skipped", "%s (element %s has no runtime codec)", model.FullName, refDisplay(elem))
			return
		}
		ctor.Codec = fmt.Sprintf("winrt.CodecScalar(%d)", size)
		ctor.BoxExpr = "uint64(item)"
	case typemap.KindGUID:
		ctor.Codec = "winrt.CodecGuid"
		ctor.BoxExpr = "item"
	case typemap.KindUnsupported:
		// The element type itself degrades in this package (severed import
		// edge, unresolved reference, ...): no signature can name it.
		g.diag("collection-ctor-skipped", "%s (element %s does not resolve: %s)", model.FullName, refDisplay(elem), splitReason(resolved.Reason).detail)
		return
	default:
		g.diag("collection-ctor-skipped", "%s (element %s has no runtime codec)", model.FullName, refDisplay(elem))
		return
	}

	if !g.claimName(ctor.CtorName) {
		g.diag("collection-ctor-skipped", "%s (constructor name %s collides with an existing declaration)", model.FullName, ctor.CtorName)
		return
	}

	// Wire the CollectionIIDs from the sibling instantiations' IID vars,
	// requesting each sibling so its type and IID var exist in-package
	// (dedup makes re-requests free). The iterator is always spawned by
	// First; views and vectors also carry the iterable tear-off facet, and
	// vectors additionally spawn snapshot views from GetView.
	iidFields := make([]string, 0, 4)
	requestSibling := func(open, field string) bool {
		ref := winrtmeta.TypeRef{
			Kind: "GenericInst", Namespace: "Windows.Foundation.Collections",
			Name: open, TargetKind: "Interface",
			Args: []winrtmeta.TypeRef{cloneRef(elem)},
		}
		mangled, ok := g.requestInstantiation(&ref)
		if !ok {
			g.diag("collection-ctor-skipped", "%s (sibling instantiation %s not groundable)", model.FullName, refDisplay(&ref))
			return false
		}
		iidFields = append(iidFields, field+": IID_"+mangled)
		return true
	}
	switch inst.Name {
	case "IIterable`1":
		iidFields = append(iidFields, "Iterable: "+model.IIDVar)
		if !requestSibling("IIterator`1", "Iterator") {
			return
		}
	case "IVectorView`1":
		if !requestSibling("IIterable`1", "Iterable") || !requestSibling("IIterator`1", "Iterator") {
			return
		}
		iidFields = append(iidFields, "VectorView: "+model.IIDVar)
	case "IVector`1":
		if !requestSibling("IIterable`1", "Iterable") || !requestSibling("IIterator`1", "Iterator") ||
			!requestSibling("IVectorView`1", "VectorView") {
			return
		}
		iidFields = append(iidFields, "Vector: "+model.IIDVar)
	}
	ctor.IIDs = "winrt.CollectionIIDs{" + strings.Join(iidFields, ", ") + "}"

	ctor.CommentLines = []string{
		fmt.Sprintf("%s creates a Go-implemented %s", ctor.CtorName, model.FullName),
		"over items, for passing INTO WinRT methods that consume the collection —",
		"native code drives it through Go-implemented vtables (see the runtime's",
		"collection core). The object starts with one caller-owned reference:",
		"Release it (through the embedded IInspectable) once no native code can",
		"still hold it.",
	}
	ctor.CommentLines = append(ctor.CommentLines, elemNotes...)
	if inst.Name == "IVector`1" {
		ctor.CommentLines = append(ctor.CommentLines,
			"The vector is writable through the WinRT ABI (the Go side exposes no",
			"mutation API); GetView returns an immutable SNAPSHOT of the contents at",
			"call time.")
	}

	imports.Merge(scratch)
	model.CollectionCtor = &ctor
}

// scalarElementSize returns the CodecScalar size for a Native scalar element
// reference, 0 when the element has no scalar codec.
func scalarElementSize(elem *winrtmeta.TypeRef) uintptr {
	if elem.Kind != "Native" {
		return 0
	}
	return scalarElementSizes[elem.Name]
}
