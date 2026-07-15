// Package winrt is the runtime layer for the WinRT bindings: HSTRING
// lifecycle, Windows Runtime initialization, runtime-class activation, and
// interface querying.
//
// It builds strictly on top of go-bindings-win32 and never redeclares the
// ABI: HSTRING, IInspectable, IActivationFactory, EventRegistrationToken,
// and the Ro*/Windows* functions all come from that module's generated
// system/winrt package, and HRESULT/GUID/IUnknown from its hand-written
// runtime.
//
// A note on IUnknown: go-bindings-win32 has two IUnknown types — the
// runtime win32.IUnknown (used as the generic **win32.IUnknown out-param
// type) and the generated com.IUnknown (embedded by syswinrt.IInspectable).
// Both are a single vtable-pointer word with identical layout, as is every
// generated interface struct, so pointers convert freely via unsafe.Pointer;
// QueryInterface relies on this by design.
package winrt
