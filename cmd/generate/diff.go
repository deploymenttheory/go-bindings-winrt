package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"sort"

	"github.com/deploymenttheory/go-bindings-winrt/internal/winrtmeta"
)

// runDiff compares two metadata trees and prints a semantic API diff in
// markdown (--json for machine output). Use it to review contract version
// bumps instead of eyeballing megabytes of regenerated JSON.
func runDiff(args []string) error {
	flags := flag.NewFlagSet("diff", flag.ExitOnError)
	oldDir := flags.String("old", "", "old metadata directory (required)")
	newDir := flags.String("new", "", "new metadata directory (required)")
	asJSON := flags.Bool("json", false, "emit machine-readable JSON")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *oldDir == "" || *newDir == "" {
		return fmt.Errorf("diff requires --old and --new")
	}
	oldNamespaces, err := winrtmeta.ReadAll(*oldDir)
	if err != nil {
		return err
	}
	newNamespaces, err := winrtmeta.ReadAll(*newDir)
	if err != nil {
		return err
	}

	report := compareTrees(indexNamespaces(oldNamespaces), indexNamespaces(newNamespaces))
	if *asJSON {
		data, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(data))
		return nil
	}
	printMarkdown(report)
	return nil
}

func indexNamespaces(namespaces []*winrtmeta.NamespaceMeta) map[string]*winrtmeta.NamespaceMeta {
	index := make(map[string]*winrtmeta.NamespaceMeta, len(namespaces))
	for _, meta := range namespaces {
		index[meta.Namespace] = meta
	}
	return index
}

// DiffReport is the machine-readable diff shape.
type DiffReport struct {
	AddedNamespaces   []string        `json:"added_namespaces,omitempty"`
	RemovedNamespaces []string        `json:"removed_namespaces,omitempty"`
	Namespaces        []NamespaceDiff `json:"namespaces,omitempty"`
}

// NamespaceDiff lists changes within one namespace, per construct category.
type NamespaceDiff struct {
	Namespace string              `json:"namespace"`
	Changes   map[string][]string `json:"changes"` // category → "+Name"/"-Name"/"~Name"
}

func compareTrees(oldIndex, newIndex map[string]*winrtmeta.NamespaceMeta) DiffReport {
	var report DiffReport
	for namespace := range newIndex {
		if oldIndex[namespace] == nil {
			report.AddedNamespaces = append(report.AddedNamespaces, namespace)
		}
	}
	for namespace := range oldIndex {
		if newIndex[namespace] == nil {
			report.RemovedNamespaces = append(report.RemovedNamespaces, namespace)
		}
	}
	sort.Strings(report.AddedNamespaces)
	sort.Strings(report.RemovedNamespaces)

	shared := make([]string, 0, len(newIndex))
	for namespace := range newIndex {
		if oldIndex[namespace] != nil {
			shared = append(shared, namespace)
		}
	}
	sort.Strings(shared)
	for _, namespace := range shared {
		changes := compareNamespace(oldIndex[namespace], newIndex[namespace])
		if len(changes) > 0 {
			report.Namespaces = append(report.Namespaces, NamespaceDiff{Namespace: namespace, Changes: changes})
		}
	}
	return report
}

// compareNamespace diffs each construct category by name; "~" marks entries
// whose definition changed (compared via canonical JSON).
func compareNamespace(oldMeta, newMeta *winrtmeta.NamespaceMeta) map[string][]string {
	changes := map[string][]string{}
	record := func(category string, entries []string) {
		if len(entries) > 0 {
			sort.Strings(entries)
			changes[category] = entries
		}
	}

	record("enums", diffNamed(toAnyMap(oldMeta.Enums), toAnyMap(newMeta.Enums)))
	record("structs", diffNamed(toAnyMap(oldMeta.Structs), toAnyMap(newMeta.Structs)))
	record("interfaces", diffNamed(toAnyMap(oldMeta.Interfaces), toAnyMap(newMeta.Interfaces)))
	record("classes", diffNamed(toAnyMap(oldMeta.Classes), toAnyMap(newMeta.Classes)))
	record("delegates", diffNamed(toAnyMap(oldMeta.Delegates), toAnyMap(newMeta.Delegates)))
	return changes
}

func toAnyMap[T any](in map[string]T) map[string]any {
	out := make(map[string]any, len(in))
	for name, value := range in {
		out[name] = value
	}
	return out
}

func diffNamed(oldByName, newByName map[string]any) []string {
	var entries []string
	for name, newValue := range newByName {
		oldValue, exists := oldByName[name]
		if !exists {
			entries = append(entries, "+"+name)
			continue
		}
		if !jsonEqual(oldValue, newValue) {
			entries = append(entries, "~"+name)
		}
	}
	for name := range oldByName {
		if _, exists := newByName[name]; !exists {
			entries = append(entries, "-"+name)
		}
	}
	return entries
}

func jsonEqual(a, b any) bool {
	aJSON, errA := json.Marshal(a)
	bJSON, errB := json.Marshal(b)
	return errA == nil && errB == nil && string(aJSON) == string(bJSON)
}

func printMarkdown(report DiffReport) {
	fmt.Println("# Metadata API diff")
	if len(report.AddedNamespaces) == 0 && len(report.RemovedNamespaces) == 0 && len(report.Namespaces) == 0 {
		fmt.Println("\nNo changes.")
		return
	}
	if len(report.AddedNamespaces) > 0 {
		fmt.Println("\n## Added namespaces")
		for _, namespace := range report.AddedNamespaces {
			fmt.Printf("- %s\n", namespace)
		}
	}
	if len(report.RemovedNamespaces) > 0 {
		fmt.Println("\n## Removed namespaces")
		for _, namespace := range report.RemovedNamespaces {
			fmt.Printf("- %s\n", namespace)
		}
	}
	for _, namespaceDiff := range report.Namespaces {
		fmt.Printf("\n## %s\n", namespaceDiff.Namespace)
		categories := make([]string, 0, len(namespaceDiff.Changes))
		for category := range namespaceDiff.Changes {
			categories = append(categories, category)
		}
		sort.Strings(categories)
		for _, category := range categories {
			entries := namespaceDiff.Changes[category]
			const listCap = 50
			fmt.Printf("- **%s** (%d): ", category, len(entries))
			if len(entries) > listCap {
				fmt.Printf("%v … and %d more\n", entries[:listCap], len(entries)-listCap)
			} else {
				fmt.Printf("%v\n", entries)
			}
		}
	}
}
