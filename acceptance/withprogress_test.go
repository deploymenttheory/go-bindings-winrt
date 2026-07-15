//go:build windows && (amd64 || arm64)

package acceptance

import (
	"fmt"
	"net"
	"net/http"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/deploymenttheory/go-bindings-winrt/bindings/runtime/winrt"
	"github.com/deploymenttheory/go-bindings-winrt/bindings/winrt/foundation"
	"github.com/deploymenttheory/go-bindings-winrt/bindings/winrt/storage"
	winhttp "github.com/deploymenttheory/go-bindings-winrt/bindings/winrt/web/http"
)

// keep winrt imported for AsyncError references in log assertions.
var _ = winrt.AsyncError

// throttledServer serves `chunks` × `chunkSize` bytes with a pause between
// chunks, so a WinRT download observably progresses instead of completing
// in one burst. Returns the URL and total size.
func throttledServer(t *testing.T, chunks, chunkSize int, pause time.Duration) (string, int) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	total := chunks * chunkSize
	payload := make([]byte, chunkSize)
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", fmt.Sprint(total))
		w.WriteHeader(http.StatusOK)
		flusher := w.(http.Flusher)
		for i := 0; i < chunks; i++ {
			if _, err := w.Write(payload); err != nil {
				return
			}
			flusher.Flush()
			time.Sleep(pause)
		}
	})}
	go server.Serve(listener)
	t.Cleanup(func() { server.Close() })
	return "http://" + listener.Addr().String() + "/blob", total
}

// TestWithProgressAwaitLive proves the WithProgress async surface end to
// end on generated code, with DETERMINISTIC progress: a throttled localhost
// HTTP server feeds HttpContent.ReadAsBufferAsync — an
// IAsyncOperationWithProgress<IBuffer, UInt64> that stays running across
// chunks — so the generated progress handler (registered via SetProgress
// before the generated Await) must fire between chunks, and Await returns
// the fully buffered body.
func TestWithProgressAwaitLive(t *testing.T) {
	url, total := throttledServer(t, 5, 64*1024, 150*time.Millisecond)

	uri, err := foundation.CreateUri(url)
	if err != nil {
		t.Fatalf("CreateUri: %v", err)
	}
	defer uri.Release()
	client, err := winhttp.NewHttpClient()
	if err != nil {
		t.Fatalf("NewHttpClient: %v", err)
	}
	defer client.Release()

	// Headers-read completion: the response arrives while the body is still
	// streaming, so the ReadAsBufferAsync below is the operation that spans
	// the throttled chunks.
	responseOp, err := client.GetWithOptionAsync(
		&uri.IUriRuntimeClass, winhttp.HttpCompletionOptionResponseHeadersRead)
	if err != nil {
		t.Fatalf("GetWithOptionAsync: %v", err)
	}
	defer responseOp.Release()
	response, err := responseOp.Await() // Await on a WithProgress operation (struct-progress variant)
	if err != nil {
		t.Fatalf("awaiting response: %v", err)
	}
	defer response.Release()

	content, err := response.Content()
	if err != nil {
		t.Fatalf("Content: %v", err)
	}
	defer content.Release()
	readOp, err := content.ReadAsBufferAsync()
	if err != nil {
		t.Fatalf("ReadAsBufferAsync: %v", err)
	}
	defer readOp.Release()

	var progressCalls atomic.Int32
	var lastProgress atomic.Uint64
	progress, err := winhttp.NewAsyncOperationProgressHandlerOfIBufferAndUInt64(
		func(asyncInfo *winhttp.IAsyncOperationWithProgressOfIBufferAndUInt64, progressInfo uint64) {
			progressCalls.Add(1)
			lastProgress.Store(progressInfo)
		})
	if err != nil {
		t.Fatalf("NewAsyncOperationProgressHandlerOfIBufferAndUInt64: %v", err)
	}
	defer progress.Close()
	if err := readOp.SetProgress(progress); err != nil {
		t.Fatalf("SetProgress: %v", err)
	}

	buffer, err := readOp.Await()
	if err != nil {
		t.Fatalf("Await: %v", err)
	}
	defer buffer.Release()
	length, err := buffer.Length()
	if err != nil {
		t.Fatalf("IBuffer.Length: %v", err)
	}
	if int(length) != total {
		t.Errorf("read %d bytes, want %d", length, total)
	}
	if progressCalls.Load() < 1 {
		t.Error("progress handler never invoked (throttled body should progress)")
	}
	t.Logf("downloaded %d bytes; %d progress callbacks, last progressInfo=%d",
		length, progressCalls.Load(), lastProgress.Load())
}

// TestWithProgressAwaitFailureLive: the failure path returns (not hangs) a
// well-formed error through a WithProgress Await.
func TestWithProgressAwaitFailureLive(t *testing.T) {
	statics, err := storage.StorageFileStatics()
	if err != nil {
		t.Fatalf("StorageFileStatics: %v", err)
	}
	defer statics.Release()
	fileOp, err := statics.GetFileFromPathAsync(filepath.Join(t.TempDir(), "missing.bin"))
	if err != nil {
		t.Fatalf("GetFileFromPathAsync: %v", err)
	}
	defer fileOp.Release()
	if _, err := fileOp.Await(); err == nil {
		t.Fatal("awaiting a missing file succeeded")
	} else {
		t.Logf("failure path: %v", err)
	}
}
