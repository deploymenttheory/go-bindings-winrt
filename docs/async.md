# Async operations

WinRT models async as `IAsyncOperation<T>` / `IAsyncAction` objects with a
`Completed` handler. The generator monomorphizes each instantiation into the
consuming package (`storage.IAsyncOperationOfStorageFile`) and synthesizes a
blocking **`Await()`** on every one whose `SetCompleted` and `GetResults`
both emitted, plus the plain `foundation.IAsyncAction`.

## Await

```go
statics, err := storage.StorageFileStatics()
defer statics.Release()
operation, err := statics.GetFileFromPathAsync(path) // starts, returns immediately
defer operation.Release()

file, err := operation.Await() // blocks until terminal, then GetResults()
defer file.Release()
```

Semantics:

- **Blocking, no timeout.** Await blocks until the operation reaches a
  terminal state — indefinitely by design. A context-aware variant is future
  work; bound it yourself (below).
- **Single-use per operation.** WinRT's `put_Completed` accepts one
  assignment per operation instance, so Await (or a manual `SetCompleted`)
  can be used **at most once** per operation. Get a fresh operation for a
  retry.
- **Safe on an already-completed operation.** WinRT invokes a handler
  assigned after completion immediately; Await returns instead of hanging.
- The result is a caller-owned reference (Release it) or a value type,
  matching the operation's `T`.

## Failures: AsyncError and errors.As

An operation that ends `Canceled` or `Error` makes Await return a
`winrt.AsyncError`: it names the terminal `AsyncStatus` and wraps the
operation's `IAsyncInfo` error code as a `win32.HRESULT`, so the standard
error chain reaches the real code:

```go
_, err := operation.Await()
// winrt: async operation ended with status Error: HRESULT 0x80070002:
// The system cannot find the file specified.
var hr win32.HRESULT
if errors.As(err, &hr) && uint32(hr) == 0x80070002 {
	// file not found
}
```

`win32` is go-bindings-win32's `bindings/runtime/win32`. The
[async example](../examples/async) exercises both paths live.

## IAsyncInfo: status, cancel

Every operation also implements `foundation.IAsyncInfo` — query for it to
poll or cancel:

```go
info, err := winrt.QueryInterface[foundation.IAsyncInfo](
	unsafe.Pointer(operation), &foundation.IID_IAsyncInfo)
defer info.Release()

status, err := info.Status() // foundation.AsyncStatusStarted / Completed / Canceled / Error
err = info.Cancel()          // request cancellation; the operation ends Canceled
err = info.Close()           // after it is terminal and consumed
```

`Cancel` is a request: an Await already in flight returns a
`winrt.AsyncError` with status `Canceled` once the operation honors it.

## Bounding Await

Await blocks until the operation reaches a terminal state and has no
timeout of its own. (An aside on quiet programs: the completion arrives on
a native threadpool thread that Go's runtime cannot see, which would
normally trip the runtime's deadlock detector in a program where nothing
else is runnable — the winrt runtime keeps a permanent keepalive timer
registered precisely so that a bare `Await()` in a minimal CLI is safe.
`acceptance/testdata/awaitprobe` is the regression proof.)

To bound the wait, use the select-with-timeout idiom:

```go
type result struct {
	file *storage.IStorageFile
	err  error
}
done := make(chan result, 1)
go func() {
	file, err := operation.Await()
	done <- result{file, err}
}()
select {
case r := <-done:
	// r.file / r.err
case <-time.After(30 * time.Second):
	// timed out; the goroutine stays parked on the operation —
	// info.Cancel() via IAsyncInfo to reclaim it
}
```

The [async](../examples/async) example shows the idiom; on timeout,
`info.Cancel()` via IAsyncInfo reclaims the parked goroutine.

## Under the hood

Await registers a generated Completed handler (a Go-implemented delegate)
that sends the terminal status on a buffered channel, blocks, then calls
`GetResults()` on success or builds the `AsyncError` from `IAsyncInfo` on
failure. The handler's Invoke runs on a fresh goroutine, so a completed
operation can never deadlock Await against the runtime's callback worker.

## Not covered yet

- **`IAsyncOperationWithProgress` / `IAsyncActionWithProgress`**: their
  `SetCompleted`/`SetProgress` setters emit, but no Await is synthesized for
  them — drive the handlers manually if you need progress.
- **Context-aware Await** (`AwaitContext(ctx)`): future work; use the
  select idiom above.
- Methods *returning* delegates (`get_Completed`) are skipped by the
  generator.
