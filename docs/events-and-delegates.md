# Events and delegates

WinRT events are add/remove accessor pairs taking a **delegate** — a COM
object with an `Invoke` method. The bindings generate a typed Go handler per
delegate type and project the accessors as `Add<Event>`/`Remove<Event>`, so
subscribing a Go function to a WinRT event is three calls and two cleanups.

## The full cycle

From the [events example](../examples/events)
(`IMemoryBufferReference.Closed`):

```go
// 1. Construct the typed handler around a Go function.
handler, err := foundation.NewTypedEventHandlerOfIMemoryBufferReferenceAndObject(
	func(sender *foundation.IMemoryBufferReference, args *syswinrt.IInspectable) {
		// runs when the event fires
	})
defer handler.Close() // 4. last: after no native code can still invoke it

// 2. Register: returns the token you unregister with.
token, err := reference.AddClosed(handler)

// ... the event fires ...

// 3. Unregister with the token.
err = reference.RemoveClosed(token)
```

- **Handler constructors** are generated per delegate type into the consuming
  package's `<pkg>_delegates.go`. Generic delegates monomorphize by name
  (`TypedEventHandler<A, B>` → `NewTypedEventHandlerOfAAndB`); non-generic
  delegates keep their declared name and IID.
- **Tokens** are `syswinrt.EventRegistrationToken` values. Keep the token;
  it is the only way to unregister.
- **`handler.Close()`** drops the Go-held reference. Call it once no native
  code can still invoke the handler — i.e. after `Remove<Event>` (and after
  any object that captured the handler is released). `defer handler.Close()`
  right after construction is idiomatic because deferred calls run
  last-in-first-out, after your deferred `Remove`.

One handler instance can be registered, removed, and re-registered; the
[bluetooth example](../examples/bluetooth) does exactly that.

## Callback arguments are borrowed

Pointer-typed arguments your callback receives are **owned by the event
source for the duration of the callback**: read what you need inside the
callback, do not `Release` them, do not retain them past its return. If you
must keep data, extract values (strings, numbers) or `AddRef` explicitly —
extracting values is almost always right:

```go
func(sender *advertisement.IBluetoothLEAdvertisementWatcher,
	args *advertisement.IBluetoothLEAdvertisementReceivedEventArgs) {
	address, _ := args.BluetoothAddress() // value: safe to keep
	rssi, _ := args.RawSignalStrengthInDBm()
	// but a pointer RETURNED by a method on a borrowed arg is a normal
	// owned reference — release it before returning:
	if adv, err := args.Advertisement(); err == nil && adv != nil {
		name, _ := adv.LocalName()
		adv.Release()
		_ = name
	}
	_, _ = address, rssi
}
```

## The execution model

Handler bodies run on a **fresh goroutine per invocation** — never on the
goroutine that registered them, and never on the runtime's shared callback
worker. The native thread delivering the event blocks until your handler
returns (`Invoke` is synchronous at the ABI). Consequences:

- **Synchronize shared state** — a mutex or channel, exactly as with any
  goroutine. Events raised from OS threadpool threads (a BLE `Received`, an
  async `Completed`) run fully concurrently with every goroutine of yours.
- **Synchronously-raised events block their trigger.** WinRT raises some
  events inside the call that causes them (`Closed` fires during
  `IClosable.Close`): the triggering call does not return until your handler
  has run — on its own goroutine, but awaited by the native side.
- **Keep handlers short.** A slow handler stalls the event source's thread;
  hand heavy work to a goroutine you own and return.
- Handlers may freely call back into WinRT (including blocking calls); the
  fresh goroutine exists precisely so a handler cannot jam the runtime's
  callback machinery.
- A handler that panics takes the process down like any goroutine panic —
  recover inside the callback if the event source is not trusted.

## Agility

Go-implemented delegates answer `QueryInterface` for their own IID,
`IUnknown`, and `IAgileObject` — they declare themselves apartment-agile, so
the runtime may invoke them from any thread without marshaling. That is the
right contract for Go, which has no thread-affine state; it also means your
handler must be safe to call from any OS thread (it is, if you follow the
synchronization rule above).

## Registration is a live COM handshake

`Add<Event>` hands the OS a real COM object: during registration the OS
QueryInterfaces and AddRefs your handler through Go-implemented vtables. You
do not see this, but it is why a handler must not be `Close`d while
registered — the OS still holds references it will Release on `Remove`.

## Raw delegates

`winrt.NewDelegate(iid, paramCount, invoke)` is the untyped core the
generated handlers wrap: `invoke` receives 1–3 raw ABI words and returns an
HRESULT. Reach for it only when a delegate type was not generated (see the
[generator guide](generator.md) for why a member might be skipped); otherwise
the typed constructors do the raw-word adaptation for you.

## What is not covered

- Events whose delegate cannot be adapted (float/struct/array parameters, an
  Invoke return value, or more than three parameters) are skipped with an
  `event-delegate-unloweable` diagnostic — the add/remove accessors are
  absent, with a `// slot N: ... skipped` comment in the generated file.
- Delegate-typed **returns** (`get_Completed`) are skipped.
