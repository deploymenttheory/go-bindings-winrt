//go:build windows && (amd64 || arm64)

package acceptance

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestAwaitStandaloneNoDeadlockFatal builds and runs testdata/awaitprobe as
// a STANDALONE process. `go test` registers a timeout timer that suppresses
// Go's deadlock detector, so only a separate binary can regress-test the
// checkdead false positive the runtime keepalive defuses (a program whose
// only wake-up source during Await is the invisible WinRT threadpool).
func TestAwaitStandaloneNoDeadlockFatal(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "awaitprobe.exe")
	build := exec.Command("go", "build", "-o", binary, "./testdata/awaitprobe")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("building probe: %v\n%s", err, out)
	}
	// Run repeatedly: pre-fix the fatal reproduced deterministically, but a
	// few runs guard against timing luck either way.
	for run := range 3 {
		out, err := exec.Command(binary).CombinedOutput()
		text := string(out)
		if strings.Contains(text, "all goroutines are asleep") {
			t.Fatalf("run %d: deadlock-detector false positive:\n%s", run, text)
		}
		if err != nil {
			t.Fatalf("run %d: probe failed: %v\n%s", run, err, text)
		}
		if !strings.Contains(text, "awaited: probe.txt") {
			t.Fatalf("run %d: unexpected output:\n%s", run, text)
		}
	}
	_ = os.Remove(binary)
}
