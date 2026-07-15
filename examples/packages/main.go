//go:build windows && (amd64 || arm64)

// Command packages queries the installed app packages for the current user
// through Windows.Management.Deployment.PackageManager and prints the first
// few names and versions.
//
// The current-user query (empty security ID = the caller) needs no
// elevation. The all-users PackageManager.FindPackages() query needs
// administrator rights on most hosts — this example sticks to the
// current-user form so it runs anywhere.
package main

import (
	"fmt"
	"log"

	"github.com/deploymenttheory/go-bindings-winrt/bindings/winrt/management/deployment"
)

// limit caps how many packages are printed; the query itself returns all.
const limit = 10

func main() {
	// PackageManager activates directly (default constructor).
	manager, err := deployment.NewPackageManager()
	if err != nil {
		log.Fatalf("NewPackageManager: %v", err)
	}
	defer manager.Release()

	// Empty string = the current user. Returns IIterable<Package>,
	// monomorphized into the deployment package.
	packages, err := manager.FindPackagesByUserSecurityId("")
	if err != nil {
		log.Fatalf("FindPackagesByUserSecurityId: %v", err)
	}
	defer packages.Release()

	iterator, err := packages.First()
	if err != nil {
		log.Fatalf("First: %v", err)
	}
	defer iterator.Release()

	total := 0
	fmt.Printf("current-user packages (first %d):\n", limit)
	for {
		has, err := iterator.HasCurrent()
		if err != nil {
			log.Fatalf("HasCurrent: %v", err)
		}
		if !has {
			break
		}
		if total < limit {
			// Current returns a new reference each call — release it.
			pkg, err := iterator.Current()
			if err != nil {
				log.Fatalf("Current: %v", err)
			}
			id, err := pkg.Id()
			if err != nil {
				pkg.Release()
				log.Fatalf("Package.Id: %v", err)
			}
			name, err := id.Name()
			if err != nil {
				log.Fatalf("PackageId.Name: %v", err)
			}
			version, err := id.Version()
			if err != nil {
				log.Fatalf("PackageId.Version: %v", err)
			}
			id.Release()
			pkg.Release()
			fmt.Printf("  %-60s %d.%d.%d.%d\n", name,
				version.Major, version.Minor, version.Build, version.Revision)
		}
		total++
		if _, err := iterator.MoveNext(); err != nil {
			log.Fatalf("MoveNext: %v", err)
		}
	}
	fmt.Printf("%d package(s) total for the current user\n", total)
	fmt.Println("(the all-users query, manager.FindPackages(), needs elevation on most hosts)")
}
