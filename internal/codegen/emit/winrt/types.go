package emitwinrt

import (
	"github.com/deploymenttheory/go-bindings-winrt/internal/codegen/emit/winrt/view"
	"github.com/deploymenttheory/go-bindings-winrt/internal/codegen/naming"
	"github.com/deploymenttheory/go-bindings-winrt/internal/codegen/typemap"
	"github.com/deploymenttheory/go-bindings-winrt/internal/winrtmeta"
)

// buildEnumModels converts a namespace's enums into render models. Enum
// members are prefixed with the type name (DayOfWeekSunday), matching the
// family's generated-enum convention.
func (g *Generator) buildEnumModels(meta *winrtmeta.NamespaceMeta) []view.EnumModel {
	models := make([]view.EnumModel, 0, len(meta.Enums))
	for _, name := range sortedKeys(meta.Enums) {
		goName := naming.Export(name)
		if !g.claimTypeName(goName) {
			g.diag("name-collision-skipped", "enum %s.%s", meta.Namespace, name)
			continue
		}
		enum := meta.Enums[name]
		model := view.EnumModel{
			TypeName: goName,
			FullName: meta.Namespace + "." + name,
			BaseType: enum.BaseType,
			IsFlags:  enum.IsFlags,
		}
		seenValues := map[string]bool{}
		for _, member := range enum.Members {
			memberName := goName + naming.Export(member.Name)
			if !g.claimName(memberName) {
				g.diag("name-collision-skipped", "enum member %s.%s.%s", meta.Namespace, name, member.Name)
				continue
			}
			memberModel := view.EnumMemberModel{Name: memberName, Value: member.Value}
			model.Members = append(model.Members, memberModel)
			if !seenValues[member.Value] {
				seenValues[member.Value] = true
				model.UniqueMembers = append(model.UniqueMembers, memberModel)
			}
		}
		models = append(models, model)
	}
	return models
}

// buildStructModels converts a namespace's structs into render models.
// External types (EventRegistrationToken, HResult) are never re-emitted;
// structs with unrepresentable fields are skipped with a diagnostic, and
// the typemap degrades any reference to them.
func (g *Generator) buildStructModels(meta *winrtmeta.NamespaceMeta, imports typemap.ImportSet) []view.StructModel {
	context := typemap.Context{Namespace: meta.Namespace}
	models := make([]view.StructModel, 0, len(meta.Structs))
	for _, name := range sortedKeys(meta.Structs) {
		if typemap.IsExternalType(meta.Namespace, name) {
			continue // routed to the shared ABI foundation, never emitted
		}
		goName := naming.Export(name)
		if !g.claimTypeName(goName) {
			g.diag("name-collision-skipped", "struct %s.%s", meta.Namespace, name)
			continue
		}
		if !g.mapper.StructEmittable(meta.Namespace, name) {
			g.diag("struct-field-skipped", "%s.%s has unrepresentable fields", meta.Namespace, name)
			continue
		}
		definition := meta.Structs[name]
		model := view.StructModel{
			TypeName: goName,
			FullName: meta.Namespace + "." + name,
		}
		scratch := typemap.ImportSet{}
		for i := range definition.Fields {
			field := &definition.Fields[i]
			resolved := g.mapper.GoType(&field.Type, context, scratch)
			model.Fields = append(model.Fields, view.StructFieldModel{
				Name:   naming.Export(field.Name),
				GoType: resolved.GoType,
			})
		}
		imports.Merge(scratch)
		models = append(models, model)
	}
	return models
}
