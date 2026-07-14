package ingest

import (
	"fmt"
	"reflect"
	"strconv"

	"github.com/deploymenttheory/go-bindings-winrt/internal/winrtmeta"
	winmd "github.com/deploymenttheory/go-winmd"
)

// fileProjector projects one contract file's TypeDefs, resolving TypeRef
// targets against the owning Ingester's union kindIndex.
type fileProjector struct {
	in   *Ingester
	file *winmd.File
	name string

	// constantIndex maps 1-based Field rows → Constant rows.
	constantIndex map[uint32]*winmd.ConstantRow
	// implIndex maps 1-based TypeDef rows → their InterfaceImpl entries
	// (row numbers kept: [Default] sits on the InterfaceImpl row itself).
	implIndex map[uint32][]implEntry
	// arityIndex maps 1-based TypeDef rows → owned GenericParam count.
	arityIndex map[uint32]int
	// eventRanges/propertyRanges map 1-based TypeDef rows → their
	// EventMap/PropertyMap row.
	eventRanges    map[uint32]*winmd.EventMapRow
	propertyRanges map[uint32]*winmd.PropertyMapRow
	// semanticsIndex maps a HasSemantics association (Event or Property
	// coded index) → its MethodSemantics rows.
	semanticsIndex map[uint64][]*winmd.MethodSemanticsRow
}

// implEntry is one InterfaceImpl row: the 1-based row number (attribute
// target) plus the implemented-interface coded index.
type implEntry struct {
	row    uint32
	target winmd.CodedIndex
}

// assocKey packs a CodedIndex into a comparable map key.
func assocKey(index winmd.CodedIndex) uint64 {
	return uint64(index.Table)<<32 | uint64(index.Row)
}

// newFileProjector precomputes the per-file lookup tables used during
// projection.
func newFileProjector(in *Ingester, source Source) *fileProjector {
	p := &fileProjector{in: in, file: source.File, name: source.Name}
	tables := &p.file.Tables

	p.constantIndex = make(map[uint32]*winmd.ConstantRow, len(tables.Constants))
	for i := range tables.Constants {
		constant := &tables.Constants[i]
		if constant.Parent.Table == winmd.TableField {
			p.constantIndex[constant.Parent.Row] = constant
		}
	}

	p.implIndex = make(map[uint32][]implEntry, len(tables.InterfaceImpls))
	for i := range tables.InterfaceImpls {
		impl := &tables.InterfaceImpls[i]
		p.implIndex[impl.Class] = append(p.implIndex[impl.Class], implEntry{row: uint32(i + 1), target: impl.Interface})
	}

	p.arityIndex = make(map[uint32]int, len(tables.GenericParams))
	for i := range tables.GenericParams {
		owner := tables.GenericParams[i].Owner
		if owner.Table == winmd.TableTypeDef {
			p.arityIndex[owner.Row]++
		}
	}

	p.eventRanges = make(map[uint32]*winmd.EventMapRow, len(tables.EventMaps))
	for i := range tables.EventMaps {
		p.eventRanges[tables.EventMaps[i].Parent] = &tables.EventMaps[i]
	}
	p.propertyRanges = make(map[uint32]*winmd.PropertyMapRow, len(tables.PropertyMaps))
	for i := range tables.PropertyMaps {
		p.propertyRanges[tables.PropertyMaps[i].Parent] = &tables.PropertyMaps[i]
	}

	p.semanticsIndex = make(map[uint64][]*winmd.MethodSemanticsRow, len(tables.MethodSemantics))
	for i := range tables.MethodSemantics {
		semantics := &tables.MethodSemantics[i]
		p.semanticsIndex[assocKey(semantics.Association)] = append(p.semanticsIndex[assocKey(semantics.Association)], semantics)
	}
	return p
}

// project routes every projected TypeDef in this file into the shared
// per-namespace metas, appending to namespaces begun by an earlier file.
func (p *fileProjector) project(byNamespace map[string]*winrtmeta.NamespaceMeta) error {
	tables := &p.file.Tables
	for typeDefRow := range tables.TypeDefs {
		typeDef := &tables.TypeDefs[typeDefRow]
		row := uint32(typeDefRow + 1)
		// Skip the <Module> pseudo-type only: [ExclusiveTo] interfaces are
		// marked NotPublic in the contracts but ARE the ABI surface.
		if typeDef.Namespace == "" {
			continue
		}
		fullName := typeDef.Namespace + "." + typeDef.Name

		kind := classifyTypeDef(p.file, typeDef)
		if kind == "" {
			// Attribute definitions (Windows.Foundation.Metadata et al.):
			// skipped as types; their instances are consumed wherever they
			// are attached.
			p.in.diag("attribute-type-skipped", "%s", fullName)
			continue
		}
		// Every WinRT TypeDef must carry tdWindowsRuntime; a bare CLR
		// type in a contract file means a mispinned metadata source.
		if typeDef.Flags&winmd.TypeAttrWindowsRuntime == 0 {
			p.in.diag("non-windowsruntime-type", "%s", fullName)
			continue
		}

		meta := byNamespace[typeDef.Namespace]
		if meta == nil {
			meta = &winrtmeta.NamespaceMeta{
				Namespace:     typeDef.Namespace,
				SchemaVersion: winrtmeta.CurrentSchemaVersion,
				WinmdVersion:  p.in.winmdVersion,
				Enums:         map[string]winrtmeta.Enum{},
				Structs:       map[string]winrtmeta.Struct{},
				Interfaces:    map[string]winrtmeta.Interface{},
				Classes:       map[string]winrtmeta.Class{},
				Delegates:     map[string]winrtmeta.Delegate{},
			}
			byNamespace[typeDef.Namespace] = meta
		}

		switch kind {
		case "Enum":
			meta.Enums[typeDef.Name] = p.projectEnum(typeDef, row)
		case "Struct":
			meta.Structs[typeDef.Name] = p.projectStruct(typeDef)
		case "Interface":
			meta.Interfaces[typeDef.Name] = p.projectInterface(typeDef, row)
		case "Class":
			meta.Classes[typeDef.Name] = p.projectClass(typeDef, row)
		case "Delegate":
			meta.Delegates[typeDef.Name] = p.projectDelegate(typeDef, row)
		}
	}
	return nil
}

// ── Attribute helpers ─────────────────────────────────────────────────────────

// hasAttribute reports whether the target carries the named attribute.
func (p *fileProjector) hasAttribute(target winmd.CodedIndex, name string) bool {
	for _, attr := range p.file.AttributesFor(target) {
		if attr.Name == name {
			return true
		}
	}
	return false
}

// guidOf reassembles a [Guid] attribute's 11 fixed args into canonical
// lowercase form.
func (p *fileProjector) guidOf(target winmd.CodedIndex) string {
	for _, attr := range p.file.AttributesFor(target) {
		if attr.Name != "GuidAttribute" || len(attr.Fixed) != 11 {
			continue
		}
		data1, _ := attr.Fixed[0].(uint32)
		data2, _ := attr.Fixed[1].(uint16)
		data3, _ := attr.Fixed[2].(uint16)
		var data4 [8]byte
		for i := 0; i < 8; i++ {
			data4[i], _ = attr.Fixed[3+i].(byte)
		}
		return fmt.Sprintf("%08x-%04x-%04x-%02x%02x-%02x%02x%02x%02x%02x%02x",
			data1, data2, data3,
			data4[0], data4[1], data4[2], data4[3], data4[4], data4[5], data4[6], data4[7])
	}
	return ""
}

// ── TypeRef conversion ────────────────────────────────────────────────────────

// nativeNames maps ECMA element types to IR Native names. ElemString means
// HSTRING and ElemObject means IInspectable under the WinRT projection;
// Bool is one byte at the ABI (encoded at emit via the typemap).
var nativeNames = map[winmd.ElementType]string{
	winmd.ElemVoid:    "Void",
	winmd.ElemBoolean: "Bool",
	winmd.ElemChar:    "Char16",
	winmd.ElemInt8:    "I1",
	winmd.ElemUInt8:   "U1",
	winmd.ElemInt16:   "I2",
	winmd.ElemUInt16:  "U2",
	winmd.ElemInt32:   "I4",
	winmd.ElemUInt32:  "U4",
	winmd.ElemInt64:   "I8",
	winmd.ElemUInt64:  "U8",
	winmd.ElemFloat32: "F32",
	winmd.ElemFloat64: "F64",
	winmd.ElemString:  "HString",
	winmd.ElemObject:  "Object",
}

// typeRefOf converts a decoded winmd type signature into the IR TypeRef.
func (p *fileProjector) typeRefOf(sig *winmd.TypeSig) winrtmeta.TypeRef {
	switch sig.Kind {
	case winmd.SigPrimitive:
		name, ok := nativeNames[sig.Primitive]
		if !ok {
			p.in.diag("unmapped-primitive", "0x%02x", byte(sig.Primitive))
			name = "Object"
		}
		return winrtmeta.TypeRef{Kind: "Native", Name: name}

	case winmd.SigNamed:
		return p.namedTypeRef(sig.Namespace, sig.Name)

	case winmd.SigPointer:
		// ELEMENT_TYPE_BYREF: a WinRT [out]/fill parameter. The logical
		// type is the pointee — direction is recorded on Param.Out — and
		// emit adds the indirection when lowering to the ABI.
		return p.typeRefOf(sig.Child)

	case winmd.SigGenericInst:
		ref := p.namedTypeRef(sig.Namespace, sig.Name)
		ref.Kind = "GenericInst"
		ref.Args = make([]winrtmeta.TypeRef, len(sig.GenericArgs))
		for i := range sig.GenericArgs {
			ref.Args[i] = p.typeRefOf(&sig.GenericArgs[i])
		}
		return ref

	case winmd.SigVar, winmd.SigMVar:
		return winrtmeta.TypeRef{Kind: "GenericParamRef", Index: sig.GenericIndex}

	case winmd.SigSZArray:
		elem := p.typeRefOf(sig.Child)
		return winrtmeta.TypeRef{Kind: "Array", Elem: &elem}
	}
	p.in.diag("unmapped-signature-kind", "%d", sig.Kind)
	return winrtmeta.TypeRef{Kind: "Native", Name: "Object"}
}

// namedTypeRef resolves a namespace-qualified type name against the union
// kindIndex, handling the mscorlib markers that never resolve as real types
// (System.Guid is the Guid native kind; the rest are type-system signals
// that must not reach a signature).
func (p *fileProjector) namedTypeRef(namespace, name string) winrtmeta.TypeRef {
	if namespace == "System" {
		if name == "Guid" {
			return winrtmeta.TypeRef{Kind: "Native", Name: "Guid"}
		}
		p.in.diag("unmapped-system-type", "System.%s", name)
		return winrtmeta.TypeRef{Kind: "Native", Name: "Object"}
	}
	fullName := namespace + "." + name
	kind, ok := p.in.kindIndex[fullName]
	if !ok {
		// The sensor for a missing contract file: fix = pin the third
		// contract as another PROVENANCE record, no design change.
		p.in.diag("unresolved-typeref", "%s", fullName)
	}
	return winrtmeta.TypeRef{Kind: "ApiRef", Namespace: namespace, Name: name, TargetKind: kind}
}

// typeRefOfTarget converts a TypeDefOrRef coded index (Extends,
// InterfaceImpl and Event targets) into the IR TypeRef; TypeSpec targets
// (generic instantiations) decode through their signature blob.
func (p *fileProjector) typeRefOfTarget(target winmd.CodedIndex) winrtmeta.TypeRef {
	tables := &p.file.Tables
	switch target.Table {
	case winmd.TableTypeRef:
		if target.Row != 0 && int(target.Row) <= len(tables.TypeRefs) {
			ref := &tables.TypeRefs[target.Row-1]
			return p.namedTypeRef(ref.Namespace, ref.Name)
		}
	case winmd.TableTypeDef:
		if target.Row != 0 && int(target.Row) <= len(tables.TypeDefs) {
			def := &tables.TypeDefs[target.Row-1]
			return p.namedTypeRef(def.Namespace, def.Name)
		}
	case winmd.TableTypeSpec:
		if target.Row != 0 && int(target.Row) <= len(tables.TypeSpecs) {
			sig, err := p.file.TypeSpecSignature(tables.TypeSpecs[target.Row-1])
			if err != nil {
				p.in.diag("typespec-error", "%v", err)
				break
			}
			return p.typeRefOf(&sig)
		}
	}
	p.in.diag("unmapped-typedeforref", "table 0x%02x row %d", target.Table, target.Row)
	return winrtmeta.TypeRef{Kind: "Native", Name: "Object"}
}

// ── Methods ───────────────────────────────────────────────────────────────────

// methodOf projects one MethodDef into the logical IR method. The signature
// is recorded exactly as the metadata declares it: no HRESULT return, no
// synthesized [out, retval] parameter — emit lowers to the ABI shape.
func (p *fileProjector) methodOf(row uint32) winrtmeta.Method {
	methodDef := &p.file.Tables.Methods[row-1]
	method := winrtmeta.Method{Name: methodDef.Name}

	// WinRT overloads share the MethodDef name; [Overload] carries the
	// unique name the projection uses.
	for _, attr := range p.file.AttributesFor(winmd.CodedIndex{Table: winmd.TableMethodDef, Row: row}) {
		switch attr.Name {
		case "OverloadAttribute":
			if len(attr.Fixed) == 1 {
				method.Overload, _ = attr.Fixed[0].(string)
			}
		case "DefaultOverloadAttribute":
			method.IsDefaultOverload = true
		}
	}

	sig, err := p.file.MethodSignature(methodDef.Signature)
	if err != nil {
		// Keep the method so later slots stay correct; emit skips it.
		p.in.diag("method-signature-error", "%s: %v", methodDef.Name, err)
		return method
	}
	method.Params = p.paramsOf(methodDef, &sig)
	if !(sig.Return.Kind == winmd.SigPrimitive && sig.Return.Primitive == winmd.ElemVoid) {
		returnRef := p.typeRefOf(&sig.Return)
		method.Return = &returnRef
	}
	return method
}

// paramsOf assembles IR params for a method: signature types matched with
// Param rows by sequence number (the Sequence 0 row, when present, carries
// return-value attributes and is skipped).
func (p *fileProjector) paramsOf(methodDef *winmd.MethodDefRow, sig *winmd.MethodSig) []winrtmeta.Param {
	params := make([]winrtmeta.Param, len(sig.Params))
	for i := range sig.Params {
		params[i] = winrtmeta.Param{
			Name: fmt.Sprintf("param%d", i),
			Type: p.typeRefOf(&sig.Params[i]),
		}
	}
	tables := &p.file.Tables
	for row := methodDef.ParamFirst; row < methodDef.ParamEnd && int(row) <= len(tables.Params); row++ {
		paramRow := &tables.Params[row-1]
		if paramRow.Sequence == 0 || int(paramRow.Sequence) > len(params) {
			continue
		}
		param := &params[paramRow.Sequence-1]
		param.Name = paramRow.Name
		param.Out = paramRow.Flags&winmd.ParamAttrOut != 0
	}
	return params
}

// ── Interfaces ────────────────────────────────────────────────────────────────

func (p *fileProjector) projectInterface(typeDef *winmd.TypeDefRow, row uint32) winrtmeta.Interface {
	fullName := typeDef.Namespace + "." + typeDef.Name
	projected := winrtmeta.Interface{
		GUID:  p.guidOf(typeDefTarget(row)),
		Arity: p.arityIndex[row],
	}
	for _, attr := range p.file.AttributesFor(typeDefTarget(row)) {
		if attr.Name == "ExclusiveToAttribute" && len(attr.Fixed) == 1 {
			projected.ExclusiveTo, _ = attr.Fixed[0].(string)
		}
	}
	for _, impl := range p.implIndex[row] {
		projected.Requires = append(projected.Requires, p.typeRefOfTarget(impl.target))
	}

	// MethodDef order IS the vtable order: slot = 6 + index. Every method
	// is kept — including generic-typed ones — so slots never shift.
	for methodRow := typeDef.MethodFirst; methodRow < typeDef.MethodEnd && int(methodRow) <= len(p.file.Tables.Methods); methodRow++ {
		projected.Methods = append(projected.Methods, p.methodOf(methodRow))
	}
	projected.Properties = p.propertiesOf(fullName, row)
	projected.Events = p.eventsOf(row)
	return projected
}

// propertiesOf projects the TypeDef's PropertyMap range, pairing accessors
// via MethodSemantics grouped by association.
func (p *fileProjector) propertiesOf(fullName string, typeDefRow uint32) []winrtmeta.Property {
	propertyRange := p.propertyRanges[typeDefRow]
	if propertyRange == nil {
		return nil
	}
	tables := &p.file.Tables
	var properties []winrtmeta.Property
	for row := propertyRange.PropertyFirst; row < propertyRange.PropertyEnd && int(row) <= len(tables.Properties); row++ {
		propertyRow := &tables.Properties[row-1]
		property := winrtmeta.Property{Name: propertyRow.Name}

		sig, err := p.file.PropertySignature(propertyRow.Type)
		if err != nil {
			p.in.diag("property-signature-error", "%s.%s: %v", fullName, propertyRow.Name, err)
			continue
		}
		property.Type = p.typeRefOf(&sig.Return)

		var getterRow uint32
		for _, semantics := range p.semanticsIndex[assocKey(winmd.CodedIndex{Table: winmd.TableProperty, Row: row})] {
			if semantics.Method == 0 || int(semantics.Method) > len(tables.Methods) {
				continue
			}
			switch {
			case semantics.Semantics&winmd.MethodSemanticsGetter != 0:
				property.Getter = tables.Methods[semantics.Method-1].Name
				getterRow = semantics.Method
			case semantics.Semantics&winmd.MethodSemanticsSetter != 0:
				property.Setter = tables.Methods[semantics.Method-1].Name
			}
		}
		p.crossCheckPropertyType(fullName, &property, getterRow)
		properties = append(properties, property)
	}
	return properties
}

// crossCheckPropertyType verifies the PropertySig type against the getter's
// logical return type — the two encode the same type independently, so a
// mismatch means a decoding bug or malformed metadata.
func (p *fileProjector) crossCheckPropertyType(fullName string, property *winrtmeta.Property, getterRow uint32) {
	if getterRow == 0 {
		return
	}
	sig, err := p.file.MethodSignature(p.file.Tables.Methods[getterRow-1].Signature)
	if err != nil {
		return // reported once via the method projection
	}
	if !reflect.DeepEqual(p.typeRefOf(&sig.Return), property.Type) {
		p.in.diag("property-type-mismatch", "%s.%s", fullName, property.Name)
	}
}

// eventsOf projects the TypeDef's EventMap range, pairing add_/remove_
// accessors via MethodSemantics.
func (p *fileProjector) eventsOf(typeDefRow uint32) []winrtmeta.Event {
	eventRange := p.eventRanges[typeDefRow]
	if eventRange == nil {
		return nil
	}
	tables := &p.file.Tables
	var events []winrtmeta.Event
	for row := eventRange.EventFirst; row < eventRange.EventEnd && int(row) <= len(tables.Events); row++ {
		eventRow := &tables.Events[row-1]
		event := winrtmeta.Event{
			Name: eventRow.Name,
			Type: p.typeRefOfTarget(eventRow.EventType),
		}
		for _, semantics := range p.semanticsIndex[assocKey(winmd.CodedIndex{Table: winmd.TableEvent, Row: row})] {
			if semantics.Method == 0 || int(semantics.Method) > len(tables.Methods) {
				continue
			}
			switch {
			case semantics.Semantics&winmd.MethodSemanticsAddOn != 0:
				event.AddMethod = tables.Methods[semantics.Method-1].Name
			case semantics.Semantics&winmd.MethodSemanticsRemoveOn != 0:
				event.RemoveMethod = tables.Methods[semantics.Method-1].Name
			}
		}
		events = append(events, event)
	}
	return events
}

// ── Classes ───────────────────────────────────────────────────────────────────

func (p *fileProjector) projectClass(typeDef *winmd.TypeDefRow, row uint32) winrtmeta.Class {
	extendsNamespace, extendsName := extendsOf(p.file, typeDef)
	projected := winrtmeta.Class{
		// Extends System.Object → plain class; extends another runtime
		// class → composable (skipped at emit until composition lands).
		Composable: !(extendsNamespace == "System" && extendsName == "Object"),
	}

	for _, impl := range p.implIndex[row] {
		target := p.typeRefOfTarget(impl.target)
		projected.Interfaces = append(projected.Interfaces, target)
		// [Default] sits on the InterfaceImpl ROW, not the TypeDef.
		if p.hasAttribute(winmd.CodedIndex{Table: winmd.TableInterfaceImpl, Row: impl.row}, "DefaultAttribute") {
			defaultInterface := target
			projected.DefaultInterface = &defaultInterface
		}
	}

	for _, attr := range p.file.AttributesFor(typeDefTarget(row)) {
		switch attr.Name {
		case "ActivatableAttribute":
			// Factory overloads lead with a System.Type fixed arg (decoded
			// as the factory's full name); version-only overloads lead
			// with the uint32 contract version → direct activation.
			if factory, ok := firstFixedString(attr.Fixed); ok {
				projected.ActivatableFactories = append(projected.ActivatableFactories, factory)
			} else {
				projected.ActivatableDirect = true
			}
		case "StaticAttribute":
			if static, ok := firstFixedString(attr.Fixed); ok {
				projected.StaticInterfaces = append(projected.StaticInterfaces, static)
			}
		case "ComposableAttribute":
			// Recorded but not processed until composition lands.
			if factory, ok := firstFixedString(attr.Fixed); ok {
				projected.ComposableFactories = append(projected.ComposableFactories, factory)
			}
		}
	}
	return projected
}

// firstFixedString returns the attribute's first fixed argument when it is
// a string (a System.Type arg decodes as the type's full name).
func firstFixedString(fixed []any) (string, bool) {
	if len(fixed) == 0 {
		return "", false
	}
	value, ok := fixed[0].(string)
	return value, ok
}

// ── Enums ─────────────────────────────────────────────────────────────────────

func (p *fileProjector) projectEnum(typeDef *winmd.TypeDefRow, row uint32) winrtmeta.Enum {
	tables := &p.file.Tables
	enum := winrtmeta.Enum{
		BaseType: "int32",
		IsFlags:  p.hasAttribute(typeDefTarget(row), "FlagsAttribute"),
	}
	for fieldRow := typeDef.FieldFirst; fieldRow < typeDef.FieldEnd && int(fieldRow) <= len(tables.Fields); fieldRow++ {
		field := &tables.Fields[fieldRow-1]
		if field.Name == "value__" {
			// WinRT enums are Int32 or UInt32 backed, nothing else.
			sig, err := p.file.FieldSignature(field.Signature)
			switch {
			case err == nil && sig.Kind == winmd.SigPrimitive && sig.Primitive == winmd.ElemInt32:
				enum.BaseType = "int32"
			case err == nil && sig.Kind == winmd.SigPrimitive && sig.Primitive == winmd.ElemUInt32:
				enum.BaseType = "uint32"
			default:
				p.in.diag("enum-bad-underlying", "%s.%s", typeDef.Namespace, typeDef.Name)
			}
			continue
		}
		constantRow := p.constantIndex[fieldRow]
		if constantRow == nil {
			continue
		}
		enum.Members = append(enum.Members, winrtmeta.EnumMember{
			Name:  field.Name,
			Value: decodeConstantValue(p.file, constantRow),
		})
	}
	return enum
}

// decodeConstantValue renders a Constant table value as a decimal string.
func decodeConstantValue(file *winmd.File, row *winmd.ConstantRow) string {
	switch typed := winmd.DecodeConstant(row.Type, file.Blobs.Get(row.Value)).(type) {
	case int64:
		return strconv.FormatInt(typed, 10)
	case uint64:
		return strconv.FormatUint(typed, 10)
	}
	return "0"
}

// ── Structs ───────────────────────────────────────────────────────────────────

func (p *fileProjector) projectStruct(typeDef *winmd.TypeDefRow) winrtmeta.Struct {
	tables := &p.file.Tables
	var projected winrtmeta.Struct
	for fieldRow := typeDef.FieldFirst; fieldRow < typeDef.FieldEnd && int(fieldRow) <= len(tables.Fields); fieldRow++ {
		field := &tables.Fields[fieldRow-1]
		if field.Flags&winmd.FieldAttrStatic != 0 {
			continue
		}
		sig, err := p.file.FieldSignature(field.Signature)
		if err != nil {
			p.in.diag("field-signature-error", "%s.%s.%s: %v", typeDef.Namespace, typeDef.Name, field.Name, err)
			continue
		}
		projected.Fields = append(projected.Fields, winrtmeta.StructField{Name: field.Name, Type: p.typeRefOf(&sig)})
	}
	return projected
}

// ── Delegates ─────────────────────────────────────────────────────────────────

func (p *fileProjector) projectDelegate(typeDef *winmd.TypeDefRow, row uint32) winrtmeta.Delegate {
	tables := &p.file.Tables
	delegate := winrtmeta.Delegate{
		GUID:  p.guidOf(typeDefTarget(row)),
		Arity: p.arityIndex[row],
	}
	for methodRow := typeDef.MethodFirst; methodRow < typeDef.MethodEnd && int(methodRow) <= len(tables.Methods); methodRow++ {
		if tables.Methods[methodRow-1].Name == "Invoke" {
			delegate.Invoke = p.methodOf(methodRow)
			return delegate
		}
	}
	p.in.diag("delegate-missing-invoke", "%s.%s", typeDef.Namespace, typeDef.Name)
	return delegate
}
