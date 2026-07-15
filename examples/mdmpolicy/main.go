//go:build windows && (amd64 || arm64)

// Command mdmpolicy reads the Windows.Management.Workplace MDM allow
// policies — pure statics reads, no MDM enrollment required. On an unmanaged
// host every policy reports its "allowed" default; on an MDM-managed host
// whatever policy the management server applied.
package main

import (
	"fmt"
	"log"

	"github.com/deploymenttheory/go-bindings-winrt/bindings/winrt/management/workplace"
)

func main() {
	// MdmAllowPolicy is a statics-only class: no instances, just the
	// package-level accessor for its [Static] interface.
	statics, err := workplace.MdmAllowPolicyStatics()
	if err != nil {
		log.Fatalf("MdmAllowPolicyStatics: %v", err)
	}
	defer statics.Release()

	browser, err := statics.IsBrowserAllowed()
	if err != nil {
		log.Fatalf("IsBrowserAllowed: %v", err)
	}
	camera, err := statics.IsCameraAllowed()
	if err != nil {
		log.Fatalf("IsCameraAllowed: %v", err)
	}
	account, err := statics.IsMicrosoftAccountAllowed()
	if err != nil {
		log.Fatalf("IsMicrosoftAccountAllowed: %v", err)
	}
	store, err := statics.IsStoreAllowed()
	if err != nil {
		log.Fatalf("IsStoreAllowed: %v", err)
	}

	fmt.Println("MDM allow policies (true = allowed / unmanaged default):")
	fmt.Printf("  browser:           %v\n", browser)
	fmt.Printf("  camera:            %v\n", camera)
	fmt.Printf("  microsoft account: %v\n", account)
	fmt.Printf("  store:             %v\n", store)
}
