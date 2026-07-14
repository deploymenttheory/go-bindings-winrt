package main

import (
	"crypto/sha256"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/deploymenttheory/go-winmd/nuget"
)

const (
	contractsPackage        = "microsoft.windows.sdk.contracts"
	contractsPackageDisplay = "Microsoft.Windows.SDK.Contracts"

	// defaultContractsVersion seeds a fresh checkout with no PROVENANCE.json;
	// afterwards the committed provenance is the pin.
	defaultContractsVersion = "10.0.26100.8249"
)

// contractFiles are the per-contract winmds consumed from the Contracts
// package (which ships ~94 of them; there is no merged Windows.winmd on
// NuGet). FoundationContract carries the Windows.Foundation core types;
// UniversalApiContract carries the bulk of the Windows.* surface. Order is
// the PROVENANCE.json record order.
var contractFiles = []string{
	"ref/netstandard2.0/Windows.Foundation.FoundationContract.winmd",
	"ref/netstandard2.0/Windows.Foundation.UniversalApiContract.winmd",
}

// runFetchMetadata downloads the Microsoft.Windows.SDK.Contracts NuGet
// package once and extracts the pinned contract winmds. With no --version
// it re-fetches the pinned (committed) version; --version latest resolves
// the newest published one. When the committed winmds already match, it
// exits without changes (so CI can detect updates via git diff).
func runFetchMetadata(args []string) error {
	flags := flag.NewFlagSet("fetch-metadata", flag.ExitOnError)
	version := flags.String("version", "", `package version ("latest" = newest published; empty = pinned)`)
	outDir := flags.String("out", filepath.Join("metadata", "winmd"), "output directory")
	force := flags.Bool("force", false, "re-download even when the version matches")
	if err := flags.Parse(args); err != nil {
		return err
	}

	client := nuget.NewClient()
	provenancePath := filepath.Join(*outDir, "PROVENANCE.json")
	current, _ := nuget.ReadProvenance(provenancePath)

	target := *version
	switch target {
	case "latest":
		latest, err := nuget.LatestVersion(client, contractsPackage)
		if err != nil {
			return err
		}
		target = latest
	case "":
		target = defaultContractsVersion
		if len(current) > 0 {
			target = current[0].Version
		}
	}

	if !*force && len(current) == len(contractFiles) && allPresent(current, *outDir, target) {
		fmt.Printf("up-to-date %s\n", target)
		return nil
	}

	sourceURL := nuget.SourceURL(contractsPackage, target)
	fmt.Printf("downloading %s\n", sourceURL)
	nupkg, err := httpGet(client, sourceURL)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(*outDir, 0o755); err != nil {
		return err
	}
	records := make([]nuget.Provenance, 0, len(contractFiles))
	for _, zipPath := range contractFiles {
		content, err := nuget.ExtractFile(nupkg, zipPath)
		if err != nil {
			return err
		}
		localPath := filepath.Join(*outDir, filepath.Base(zipPath))
		if err := os.WriteFile(localPath, content, 0o644); err != nil {
			return err
		}
		records = append(records, nuget.Provenance{
			Package: contractsPackageDisplay,
			Version: target,
			Source:  sourceURL,
			File:    zipPath,
			SHA256:  fmt.Sprintf("%x", sha256.Sum256(content)),
			Fetched: time.Now().UTC().Format("2006-01-02"),
		})
		fmt.Printf("extracted %s (%d bytes)\n", filepath.Base(zipPath), len(content))
	}
	if err := nuget.WriteProvenance(provenancePath, records); err != nil {
		return err
	}
	previous := "(none)"
	if len(current) > 0 {
		previous = current[0].Version
	}
	fmt.Printf("updated %s -> %s\n", previous, target)
	return nil
}

// allPresent reports whether every pinned winmd exists on disk with the
// recorded hash and the target version.
func allPresent(records []nuget.Provenance, outDir, version string) bool {
	for _, record := range records {
		if record.Version != version {
			return false
		}
		data, err := os.ReadFile(filepath.Join(outDir, filepath.Base(record.File)))
		if err != nil || fmt.Sprintf("%x", sha256.Sum256(data)) != record.SHA256 {
			return false
		}
	}
	return true
}

func httpGet(client *http.Client, url string) ([]byte, error) {
	response, err := client.Get(url)
	if err != nil {
		return nil, fmt.Errorf("GET %s: %w", url, err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: %s", url, response.Status)
	}
	data, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, fmt.Errorf("GET %s: %w", url, err)
	}
	return data, nil
}
