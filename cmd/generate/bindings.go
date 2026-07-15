package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	emitwinrt "github.com/deploymenttheory/go-bindings-winrt/internal/codegen/emit/winrt"
	"github.com/deploymenttheory/go-bindings-winrt/internal/codegen/pipeline"
	"github.com/deploymenttheory/go-bindings-winrt/internal/diagnostics"
)

// modulePath is this module's import path root.
const modulePath = "github.com/deploymenttheory/go-bindings-winrt"

// runBindings emits the bindings tree (bindings/winrt) from the ingested
// metadata. It is self-cleaning — generated files from an earlier run that
// this run does not rewrite are pruned (hand-written files, which lack the
// DO-NOT-EDIT header, are never touched). A --namespace filter emits the
// named namespaces plus the transitive closure of namespaces referenced by
// their emitted members.
func runBindings(args []string) error {
	flags := flag.NewFlagSet("bindings", flag.ExitOnError)
	metadataDir := flags.String("metadata", filepath.Join("metadata", "winrt"), "directory of .winrtmeta.json files")
	outDir := flags.String("out", filepath.Join("bindings", "winrt"), "bindings output root")
	namespaceFilter := flags.String("namespace", "", "comma-separated namespace filter (full names, e.g. Windows.Globalization); empty = all loaded")
	verbose := flags.Bool("v", false, "print every diagnostic")
	writeBaseline := flags.String("diagnostics", "", "write the diagnostics baseline to this path")
	checkBaseline := flags.String("diagnostics-baseline", "", "fail if any diagnostic is not in this committed baseline")
	if err := flags.Parse(args); err != nil {
		return err
	}

	registry, err := pipeline.LoadAll(*metadataDir)
	if err != nil {
		return err
	}
	filter := map[string]bool{}
	for _, name := range strings.Split(*namespaceFilter, ",") {
		if name = strings.TrimSpace(name); name != "" {
			filter[name] = true
		}
	}

	generator := emitwinrt.New(registry, modulePath, *outDir)
	written, err := generator.EmitAll(filter)
	if err != nil {
		return err
	}
	diags := generator.Diagnostics
	if *verbose {
		for _, diagnostic := range diags {
			fmt.Fprintln(os.Stderr, "diagnostic:", diagnostic)
		}
	}
	fmt.Printf("emitted %d packages → %s (%d diagnostics)\n", written, *outDir, len(diags))
	printDiagnosticSummary(diags)

	if *writeBaseline != "" {
		if err := diagnostics.WriteBaseline(*writeBaseline, diags); err != nil {
			return err
		}
		fmt.Printf("wrote diagnostics baseline → %s\n", *writeBaseline)
	}
	if *checkBaseline != "" {
		newEntries, err := diagnostics.CheckBaseline(*checkBaseline, diags)
		if err != nil {
			return err
		}
		if len(newEntries) > 0 {
			for _, entry := range newEntries {
				fmt.Fprintln(os.Stderr, "new diagnostic:", entry)
			}
			return fmt.Errorf("%d diagnostics beyond baseline %s (fix them, or rewrite the baseline with --diagnostics after review)",
				len(newEntries), *checkBaseline)
		}
		fmt.Println("diagnostics within baseline")
	}
	return nil
}
