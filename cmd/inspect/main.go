// Command inspect dumps a .winrtmeta.json namespace file: a summary by
// default, or one construct in full with --name.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"sort"

	"github.com/deploymenttheory/go-bindings-winrt/internal/winrtmeta"
)

func main() {
	name := flag.String("name", "", "dump one construct (interface/class/enum/struct/delegate) as JSON")
	flag.Parse()
	if flag.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "usage: inspect [--name <Type>] <path-to.winrtmeta.json>")
		os.Exit(2)
	}
	meta, err := winrtmeta.Read(flag.Arg(0))
	if err != nil {
		fmt.Fprintln(os.Stderr, "inspect:", err)
		os.Exit(1)
	}
	if *name != "" {
		dumpOne(meta, *name)
		return
	}
	summarize(meta)
}

func dumpOne(meta *winrtmeta.NamespaceMeta, name string) {
	var found any
	if i, ok := meta.Interfaces[name]; ok {
		found = i
	} else if c, ok := meta.Classes[name]; ok {
		found = c
	} else if e, ok := meta.Enums[name]; ok {
		found = e
	} else if s, ok := meta.Structs[name]; ok {
		found = s
	} else if d, ok := meta.Delegates[name]; ok {
		found = d
	}
	if found == nil {
		fmt.Fprintf(os.Stderr, "inspect: %q not found in %s\n", name, meta.Namespace)
		os.Exit(1)
	}
	out, _ := json.MarshalIndent(found, "", "  ")
	fmt.Println(string(out))
}

func summarize(meta *winrtmeta.NamespaceMeta) {
	fmt.Printf("namespace  %s (winmd %s, schema %d)\n", meta.Namespace, meta.WinmdVersion, meta.SchemaVersion)
	fmt.Printf("interfaces %d\n", len(meta.Interfaces))
	fmt.Printf("classes    %d\n", len(meta.Classes))
	fmt.Printf("enums      %d\n", len(meta.Enums))
	fmt.Printf("structs    %d\n", len(meta.Structs))
	fmt.Printf("delegates  %d\n", len(meta.Delegates))

	printSorted := func(label string, names []string) {
		sort.Strings(names)
		limit := min(len(names), 20)
		if limit > 0 {
			fmt.Printf("\n%s (first %d):\n", label, limit)
			for _, n := range names[:limit] {
				fmt.Println(" ", n)
			}
		}
	}
	var interfaceNames []string
	for n := range meta.Interfaces {
		interfaceNames = append(interfaceNames, n)
	}
	printSorted("interfaces", interfaceNames)
	var classNames []string
	for n := range meta.Classes {
		classNames = append(classNames, n)
	}
	printSorted("classes", classNames)
}
