//go:build windows && (amd64 || arm64)

// Command speech enumerates the installed text-to-speech voices, then
// synthesizes a short phrase to an in-memory audio stream and reports its
// size — SpeechSynthesizer end to end, including an awaited async operation
// whose result crosses namespaces (a Windows.Media.SpeechSynthesis stream
// read through Windows.Storage.Streams).
//
// Hardware-free: every Windows installation ships at least one voice.
package main

import (
	"fmt"
	"log"
	"unsafe"

	"github.com/deploymenttheory/go-bindings-winrt/bindings/runtime/winrt"
	"github.com/deploymenttheory/go-bindings-winrt/bindings/winrt/media/speechsynthesis"
	"github.com/deploymenttheory/go-bindings-winrt/bindings/winrt/storage/streams"
)

func main() {
	// The installed-voices statics: a vector view of VoiceInformation.
	statics, err := speechsynthesis.InstalledVoicesStatic()
	if err != nil {
		log.Fatalf("InstalledVoicesStatic: %v", err)
	}
	defer statics.Release()
	voices, err := statics.AllVoices()
	if err != nil {
		log.Fatalf("AllVoices: %v", err)
	}
	defer voices.Release()
	size, err := voices.Size()
	if err != nil {
		log.Fatalf("Size: %v", err)
	}
	fmt.Printf("installed voices (%d):\n", size)
	for i := uint32(0); i < size; i++ {
		voice, err := voices.GetAt(i)
		if err != nil {
			log.Fatalf("GetAt(%d): %v", i, err)
		}
		name, err := voice.DisplayName()
		if err != nil {
			log.Fatalf("DisplayName: %v", err)
		}
		language, err := voice.Language()
		if err != nil {
			log.Fatalf("Language: %v", err)
		}
		voice.Release()
		fmt.Printf("  %-40s %s\n", name, language)
	}

	// Synthesize a phrase with the default voice.
	synthesizer, err := speechsynthesis.NewSpeechSynthesizer()
	if err != nil {
		log.Fatalf("NewSpeechSynthesizer: %v", err)
	}
	defer synthesizer.Release()
	voice, err := synthesizer.Voice()
	if err != nil {
		log.Fatalf("Voice: %v", err)
	}
	defaultName, err := voice.DisplayName()
	if err != nil {
		log.Fatalf("DisplayName (default voice): %v", err)
	}
	voice.Release()

	operation, err := synthesizer.SynthesizeTextToStreamAsync("Hello from go bindings for the Windows Runtime")
	if err != nil {
		log.Fatalf("SynthesizeTextToStreamAsync: %v", err)
	}
	defer operation.Release()
	stream, err := operation.Await()
	if err != nil {
		// Stripped-down hosts (no speech platform) fail here well-formed.
		fmt.Printf("synthesis unavailable on this host: %v\n", err)
		return
	}
	defer stream.Release()

	// The synthesized stream's byte size lives on IRandomAccessStream
	// (Windows.Storage.Streams), another interface of the same object.
	random, err := winrt.QueryInterface[streams.IRandomAccessStream](
		unsafe.Pointer(stream), &streams.IID_IRandomAccessStream)
	if err != nil {
		log.Fatalf("QueryInterface(IRandomAccessStream): %v", err)
	}
	defer random.Release()
	bytes, err := random.Size()
	if err != nil {
		log.Fatalf("IRandomAccessStream.Size: %v", err)
	}
	fmt.Printf("synthesized with %q: %d bytes of audio\n", defaultName, bytes)
}
