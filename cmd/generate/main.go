// Command generate drives the go-bindings-winrt pipeline:
//
//	generate fetch-metadata   download the pinned contract winmds from NuGet
//	generate ingest           project the winmds into per-namespace .winrtmeta.json files
//	generate bindings         emit the Go bindings from the .winrtmeta.json metadata (self-cleaning)
//	generate validate         structural integrity checks over the metadata
//	generate diff             semantic API diff between two metadata trees
//	generate list             list the ingested namespaces with construct counts
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
	case "bindings":
		err = runBindings(os.Args[2:])
	case "validate":
		err = runValidate(os.Args[2:])
	case "diff":
		err = runDiff(os.Args[2:])
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
  bindings        emit the Go bindings from the .winrtmeta.json metadata (self-cleaning)
  validate        structural integrity checks over the metadata
  diff            semantic API diff between two metadata trees
  list            list the ingested namespaces with construct counts`)
}
