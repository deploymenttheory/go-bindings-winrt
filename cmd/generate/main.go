// Command generate drives the go-bindings-winrt pipeline:
//
//	generate fetch-metadata   download the pinned contract winmds from NuGet
//
// Further subcommands (ingest, list, bindings, validate, diff) arrive with
// later milestones.
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
  fetch-metadata  download the contract winmds from NuGet into metadata/winmd`)
}
