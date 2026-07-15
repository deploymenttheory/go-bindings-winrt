package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/deploymenttheory/go-bindings-winrt/internal/codegen/pipeline"
	"github.com/deploymenttheory/go-bindings-winrt/internal/winrtmeta"
)

// runValidate performs structural integrity checks over the ingested
// metadata: dangling type references, malformed enums, interfaces without
// GUIDs. Errors fail the process; warnings report.
func runValidate(args []string) error {
	flags := flag.NewFlagSet("validate", flag.ExitOnError)
	metadataDir := flags.String("metadata", filepath.Join("metadata", "winrt"), "directory of .winrtmeta.json files")
	if err := flags.Parse(args); err != nil {
		return err
	}
	registry, err := pipeline.LoadAll(*metadataDir)
	if err != nil {
		return err
	}

	var errorsFound, warnings []string
	addError := func(format string, args ...any) {
		errorsFound = append(errorsFound, fmt.Sprintf(format, args...))
	}
	addWarning := func(format string, args ...any) {
		warnings = append(warnings, fmt.Sprintf(format, args...))
	}

	for _, meta := range registry.Namespaces {
		namespace := meta.Namespace

		// Every ApiRef/GenericInst must resolve against the registry.
		pipeline.WalkNamespaceRefs(meta, func(ref *winrtmeta.TypeRef) {
			if ref.Kind != "ApiRef" && ref.Kind != "GenericInst" {
				return
			}
			if ref.Namespace == "" {
				addError("[%s] %s reference %q has no namespace", namespace, ref.Kind, ref.Name)
				return
			}
			if registry.ByNamespace[ref.Namespace] == nil {
				addError("[%s] reference to unknown namespace %s (%s)", namespace, ref.Namespace, ref.Name)
				return
			}
			key := ref.Namespace + "." + ref.Name
			resolved := false
			switch ref.TargetKind {
			case "Enum":
				_, resolved = registry.EnumIndex[key]
			case "Struct":
				_, resolved = registry.StructIndex[key]
			case "Interface":
				_, resolved = registry.InterfaceIndex[key]
			case "Class":
				_, resolved = registry.ClassIndex[key]
			case "Delegate":
				_, resolved = registry.DelegateIndex[key]
			default:
				addError("[%s] reference %s has no target kind (unresolved at ingest)", namespace, key)
				return
			}
			if !resolved {
				addError("[%s] dangling %s reference %s", namespace, ref.TargetKind, key)
			}
		})

		for name, enum := range meta.Enums {
			if enum.BaseType != "int32" && enum.BaseType != "uint32" {
				addError("[%s] enum %s has invalid base type %q (WinRT enums are Int32 or UInt32)", namespace, name, enum.BaseType)
			}
			if len(enum.Members) == 0 {
				addWarning("[%s] enum %s has no members", namespace, name)
			}
		}
		for name, definition := range meta.Interfaces {
			if definition.GUID == "" {
				addWarning("[%s] interface %s has no GUID", namespace, name)
			}
		}
		for name, delegate := range meta.Delegates {
			if delegate.GUID == "" {
				addWarning("[%s] delegate %s has no GUID", namespace, name)
			}
		}
		for name, class := range meta.Classes {
			if class.DefaultInterface == nil && len(class.Interfaces) > 0 {
				addWarning("[%s] class %s has instance interfaces but no [Default]", namespace, name)
			}
		}
	}

	sort.Strings(errorsFound)
	sort.Strings(warnings)
	for _, warning := range warnings {
		fmt.Printf("warning: %s\n", warning)
	}
	for _, message := range errorsFound {
		fmt.Printf("error: %s\n", message)
	}
	fmt.Printf("validate: %d namespaces, %d errors, %d warnings\n",
		len(registry.Namespaces), len(errorsFound), len(warnings))
	if len(errorsFound) > 0 {
		os.Exit(1)
	}
	return nil
}
