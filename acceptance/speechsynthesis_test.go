//go:build windows && (amd64 || arm64)

package acceptance

import (
	"testing"

	"github.com/deploymenttheory/go-bindings-winrt/bindings/winrt/media/speechsynthesis"
)

// The full-surface sanity proof: Windows.Media.SpeechSynthesis was never part
// of the rooted 25-package tree — it exists only because the emit roots now
// pin every IR namespace. Activation, statics access, a monomorphized
// IVectorView over a class type, and property dispatch all run live against
// a package the generator had never produced before. Hardware-free: every
// Windows host ships at least one installed voice.

func TestSpeechSynthesizerVoicesLive(t *testing.T) {
	synthesizer, err := speechsynthesis.NewSpeechSynthesizer()
	if err != nil {
		t.Fatalf("NewSpeechSynthesizer: %v", err)
	}
	defer synthesizer.Release()

	// The default voice, through the instance property.
	voice, err := synthesizer.Voice()
	if err != nil {
		t.Fatalf("ISpeechSynthesizer.Voice: %v", err)
	}
	if voice == nil {
		t.Fatal("default voice is nil")
	}
	defer voice.Release()
	defaultName, err := voice.DisplayName()
	if err != nil {
		t.Fatalf("IVoiceInformation.DisplayName: %v", err)
	}
	if defaultName == "" {
		t.Error("default voice DisplayName is empty")
	}

	// The installed-voices statics enumeration.
	statics, err := speechsynthesis.InstalledVoicesStatic()
	if err != nil {
		t.Fatalf("InstalledVoicesStatic: %v", err)
	}
	defer statics.Release()
	voices, err := statics.AllVoices()
	if err != nil {
		t.Fatalf("IInstalledVoicesStatic.AllVoices: %v", err)
	}
	if voices == nil {
		t.Fatal("AllVoices returned a nil vector view")
	}
	defer voices.Release()
	size, err := voices.Size()
	if err != nil {
		t.Fatalf("IVectorViewOfVoiceInformation.Size: %v", err)
	}
	if size == 0 {
		t.Fatal("no installed voices; Windows always ships at least one")
	}
	first, err := voices.GetAt(0)
	if err != nil {
		t.Fatalf("IVectorViewOfVoiceInformation.GetAt(0): %v", err)
	}
	defer first.Release()
	firstName, err := first.DisplayName()
	if err != nil {
		t.Fatalf("IVoiceInformation.DisplayName: %v", err)
	}
	language, err := first.Language()
	if err != nil {
		t.Fatalf("IVoiceInformation.Language: %v", err)
	}
	if firstName == "" || language == "" {
		t.Errorf("first voice = %q (%q), want non-empty name and language", firstName, language)
	}
	t.Logf("default voice %q; %d installed, first %q (%s)", defaultName, size, firstName, language)
}
