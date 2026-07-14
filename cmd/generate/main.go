// Command generate drives the go-bindings-winrt pipeline:
//
//	generate fetch-metadata   download the pinned contract winmds from NuGet
//	generate ingest           project the winmds into per-namespace .winrtmeta.json files
//	generate list             list the ingested namespaces with construct counts
//
// Further subcommands (bindings, validate, diff) arrive with later
// milestones.
package main

import (
	"fmt"
	"os"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "fetch-metadata":
		err = runFetchMetadata(os.Args[2:])
	case "ingest":
		err = runIngest(os.Args[2:])
	case "list":
		err = runList(os.Args[2:])
	default:
		usage()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "generate:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, `usage: generate <command> [flags]

commands:
  fetch-metadata  download the contract winmds from NuGet into metadata/winmd
  ingest          project the contract winmds into per-namespace .winrtmeta.json files
  list            list the ingested namespaces with construct counts`)
}
