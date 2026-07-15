package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/deploymenttheory/go-bindings-winrt/internal/winrtmeta"
	"github.com/deploymenttheory/go-bindings-winrt/internal/winrtmeta/ingest"
	winmd "github.com/deploymenttheory/go-winmd"
	"github.com/deploymenttheory/go-winmd/nuget"
)

// openSources opens every contract winmd listed in the directory's
// PROVENANCE.json (the pin is the source of truth for which files
// participate in the union), returning the sources and the pinned package
// version.
func openSources(winmdDir string) ([]ingest.Source, string, error) {
	records, err := nuget.ReadProvenance(filepath.Join(winmdDir, "PROVENANCE.json"))
	if err != nil {
		return nil, "", fmt.Errorf("reading %s: %w (run 'generate fetch-metadata')", filepath.Join(winmdDir, "PROVENANCE.json"), err)
	}
	if len(records) == 0 {
		return nil, "", fmt.Errorf("%s lists no contract files", filepath.Join(winmdDir, "PROVENANCE.json"))
	}
	sources := make([]ingest.Source, 0, len(records))
	for _, record := range records {
		name := filepath.Base(record.File)
		file, err := winmd.Open(filepath.Join(winmdDir, name))
		if err != nil {
			return nil, "", err
		}
		sources = append(sources, ingest.Source{Name: name, File: file})
	}
	return sources, records[0].Version, nil
}

func runIngest(args []string) error {
	flags := flag.NewFlagSet("ingest", flag.ExitOnError)
	winmdDir := flags.String("winmd-dir", filepath.Join("metadata", "winmd"), "directory of the pinned contract winmds + PROVENANCE.json")
	outDir := flags.String("out", filepath.Join("metadata", "winrt"), "output directory for .winrtmeta.json files")
	namespaceFilter := flags.String("namespace", "", "comma-separated namespace filter (full names, e.g. Windows.Globalization); empty = all")
	verbose := flags.Bool("v", false, "print every diagnostic")
	if err := flags.Parse(args); err != nil {
		return err
	}

	sources, version, err := openSources(*winmdDir)
	if err != nil {
		return err
	}
	ingester, err := ingest.New(sources, version)
	if err != nil {
		return err
	}
	namespaces, err := ingester.Ingest()
	if err != nil {
		return err
	}

	filter := map[string]bool{}
	for _, name := range strings.Split(*namespaceFilter, ",") {
		if name = strings.TrimSpace(name); name != "" {
			filter[name] = true
		}
	}

	if err := os.MkdirAll(*outDir, 0o755); err != nil {
		return err
	}
	written := 0
	for _, meta := range namespaces {
		if len(filter) > 0 && !filter[meta.Namespace] {
			continue
		}
		if err := winrtmeta.Write(*outDir, meta); err != nil {
			return err
		}
		written++
	}
	if *verbose {
		for _, diagnostic := range ingester.Diagnostics {
			fmt.Fprintln(os.Stderr, "diagnostic:", diagnostic)
		}
	}
	fmt.Printf("ingested %d namespaces → %s (%d diagnostics)\n", written, *outDir, len(ingester.Diagnostics))
	printDiagnosticSummary(ingester.Diagnostics)
	return nil
}

// printDiagnosticSummary prints diagnostic counts grouped by key (the part
// before the first colon), sorted by key.
func printDiagnosticSummary(diagnostics []string) {
	counts := map[string]int{}
	for _, diagnostic := range diagnostics {
		key, _, _ := strings.Cut(diagnostic, ":")
		counts[key]++
	}
	keys := make([]string, 0, len(counts))
	for key := range counts {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		fmt.Printf("  %-28s %d\n", key, counts[key])
	}
}

func runList(args []string) error {
	flags := flag.NewFlagSet("list", flag.ExitOnError)
	metadataDir := flags.String("metadata", filepath.Join("metadata", "winrt"), "directory of .winrtmeta.json files")
	if err := flags.Parse(args); err != nil {
		return err
	}
	namespaces, err := winrtmeta.ReadAll(*metadataDir)
	if err != nil {
		return err
	}
	if len(namespaces) == 0 {
		return fmt.Errorf("no .winrtmeta.json files in %s (run 'generate ingest')", *metadataDir)
	}
	totals := [5]int{}
	for _, meta := range namespaces {
		fmt.Printf("%-58s %4d ifaces %4d classes %4d enums %4d structs %4d delegates\n",
			meta.Namespace, len(meta.Interfaces), len(meta.Classes), len(meta.Enums),
			len(meta.Structs), len(meta.Delegates))
		totals[0] += len(meta.Interfaces)
		totals[1] += len(meta.Classes)
		totals[2] += len(meta.Enums)
		totals[3] += len(meta.Structs)
		totals[4] += len(meta.Delegates)
	}
	fmt.Printf("%-58s %4d ifaces %4d classes %4d enums %4d structs %4d delegates\n",
		fmt.Sprintf("total (%d namespaces)", len(namespaces)), totals[0], totals[1], totals[2], totals[3], totals[4])
	return nil
}
