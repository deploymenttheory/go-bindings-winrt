// Package pinterface computes WinRT parameterized-type instantiation IIDs.
//
// A closed generic instantiation (e.g. IVector<String>) has no [Guid] in
// metadata; its IID is derived, per the WinMD specification's "Guid
// generation for parameterized types", as an RFC 4122 version-5-style UUID:
// SHA-1 over the WinRT pinterface namespace GUID plus the instantiation's
// grounded signature string. All projections (C++/WinRT, CsWinRT,
// windows-rs) derive the same IIDs, which is what makes cross-language
// QueryInterface on parameterized interfaces work.
package pinterface

import (
	"crypto/sha1"
	"encoding/binary"
	"fmt"
	"strings"

	"github.com/deploymenttheory/go-bindings-winrt/internal/codegen/pipeline"
	"github.com/deploymenttheory/go-bindings-winrt/internal/winrtmeta"
)

// wrtPinterfaceNamespace is the fixed UUID namespace for WinRT
// parameterized-type IIDs: {11f47ad5-7b73-42c0-abae-878b1e16adee}, as raw
// network-order bytes.
var wrtPinterfaceNamespace = [16]byte{
	0x11, 0xf4, 0x7a, 0xd5, 0x7b, 0x73, 0x42, 0xc0,
	0xab, 0xae, 0x87, 0x8b, 0x1e, 0x16, 0xad, 0xee,
}

// nativeSignatures maps the IR's Native kinds to their signature atoms
// (WinMD spec "type signature" grammar).
var nativeSignatures = map[string]string{
	"Bool":    "b1",
	"Char16":  "c2",
	"U1":      "u1",
	"I2":      "i2",
	"U2":      "u2",
	"I4":      "i4",
	"U4":      "u4",
	"I8":      "i8",
	"U8":      "u8",
	"F32":     "f4",
	"F64":     "f8",
	"HString": "string",
	"Guid":    "g16",
	"Object":  "cinterface(IInspectable)",
}

// InstanceIID computes the IID of a closed generic instantiation
// (TypeRef.Kind == "GenericInst") as a canonical lowercase GUID string.
func InstanceIID(inst *winrtmeta.TypeRef, registry *pipeline.Registry) (string, error) {
	if inst.Kind != "GenericInst" {
		return "", fmt.Errorf("pinterface: %s.%s is not a generic instantiation", inst.Namespace, inst.Name)
	}
	signature, err := Signature(inst, registry)
	if err != nil {
		return "", err
	}
	return uuidV5(wrtPinterfaceNamespace, signature), nil
}

// Signature builds the grounded signature string for a type reference
// (WinMD spec grammar). Every referenced type must resolve through the
// registry; unbound generic parameters and arrays cannot be grounded.
func Signature(ref *winrtmeta.TypeRef, registry *pipeline.Registry) (string, error) {
	switch ref.Kind {
	case "Native":
		if atom, ok := nativeSignatures[ref.Name]; ok {
			return atom, nil
		}
		return "", fmt.Errorf("pinterface: native kind %q has no signature form", ref.Name)

	case "ApiRef":
		return apiRefSignature(ref, registry)

	case "GenericInst":
		openGUID, err := openTypeGUID(ref, registry)
		if err != nil {
			return "", err
		}
		parts := make([]string, 0, len(ref.Args)+1)
		parts = append(parts, "{"+openGUID+"}")
		for i := range ref.Args {
			argSig, err := Signature(&ref.Args[i], registry)
			if err != nil {
				return "", err
			}
			parts = append(parts, argSig)
		}
		return "pinterface(" + strings.Join(parts, ";") + ")", nil

	case "GenericParamRef":
		return "", fmt.Errorf("pinterface: unbound generic parameter %d cannot be grounded", ref.Index)

	case "Array":
		return "", fmt.Errorf("pinterface: arrays have no signature form")
	}
	return "", fmt.Errorf("pinterface: unknown TypeRef kind %q", ref.Kind)
}

// apiRefSignature renders a resolved non-generic type reference.
func apiRefSignature(ref *winrtmeta.TypeRef, registry *pipeline.Registry) (string, error) {
	full := ref.Namespace + "." + ref.Name
	switch ref.TargetKind {
	case "Interface":
		iface := registry.Interface(ref.Namespace, ref.Name)
		if iface == nil || iface.GUID == "" {
			return "", fmt.Errorf("pinterface: interface %s unresolved or missing [Guid]", full)
		}
		return "{" + iface.GUID + "}", nil

	case "Delegate":
		delegate := registry.Delegate(ref.Namespace, ref.Name)
		if delegate == nil || delegate.GUID == "" {
			return "", fmt.Errorf("pinterface: delegate %s unresolved or missing [Guid]", full)
		}
		return "delegate({" + delegate.GUID + "})", nil

	case "Enum":
		base := registry.EnumBase(ref.Namespace, ref.Name)
		switch base {
		case "int32":
			return "enum(" + full + ";i4)", nil
		case "uint32":
			return "enum(" + full + ";u4)", nil
		}
		return "", fmt.Errorf("pinterface: enum %s unresolved or non-integral (%q)", full, base)

	case "Struct":
		definition := registry.Struct(ref.Namespace, ref.Name)
		if definition == nil {
			return "", fmt.Errorf("pinterface: struct %s unresolved", full)
		}
		parts := make([]string, 0, len(definition.Fields)+1)
		parts = append(parts, full)
		for i := range definition.Fields {
			fieldSig, err := Signature(&definition.Fields[i].Type, registry)
			if err != nil {
				return "", err
			}
			parts = append(parts, fieldSig)
		}
		return "struct(" + strings.Join(parts, ";") + ")", nil

	case "Class":
		class := registry.Class(ref.Namespace, ref.Name)
		if class == nil || class.DefaultInterface == nil {
			return "", fmt.Errorf("pinterface: class %s unresolved or without a default interface", full)
		}
		defaultSig, err := Signature(class.DefaultInterface, registry)
		if err != nil {
			return "", err
		}
		return "rc(" + full + ";" + defaultSig + ")", nil
	}
	return "", fmt.Errorf("pinterface: %s has unresolved target kind %q", full, ref.TargetKind)
}

// openTypeGUID resolves the [Guid] of the OPEN generic type behind an
// instantiation (interfaces and delegates both instantiate).
func openTypeGUID(ref *winrtmeta.TypeRef, registry *pipeline.Registry) (string, error) {
	if iface := registry.Interface(ref.Namespace, ref.Name); iface != nil && iface.GUID != "" {
		return iface.GUID, nil
	}
	if delegate := registry.Delegate(ref.Namespace, ref.Name); delegate != nil && delegate.GUID != "" {
		return delegate.GUID, nil
	}
	return "", fmt.Errorf("pinterface: open generic %s.%s unresolved or missing [Guid]", ref.Namespace, ref.Name)
}

// uuidV5 computes the version-5 UUID of name within the given namespace and
// renders it canonically (lowercase, unbraced).
func uuidV5(namespace [16]byte, name string) string {
	hash := sha1.New()
	hash.Write(namespace[:])
	hash.Write([]byte(name))
	sum := hash.Sum(nil)

	var uuid [16]byte
	copy(uuid[:], sum[:16])
	uuid[6] = (uuid[6] & 0x0F) | 0x50 // version 5
	uuid[8] = (uuid[8] & 0x3F) | 0x80 // RFC 4122 variant

	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		binary.BigEndian.Uint32(uuid[0:4]),
		binary.BigEndian.Uint16(uuid[4:6]),
		binary.BigEndian.Uint16(uuid[6:8]),
		binary.BigEndian.Uint16(uuid[8:10]),
		uuid[10:16])
}
