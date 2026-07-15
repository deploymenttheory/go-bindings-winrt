//go:build windows && (amd64 || arm64)

package acceptance

import (
	"errors"
	"testing"
	"time"

	win32 "github.com/deploymenttheory/go-bindings-win32/bindings/runtime/win32"

	"github.com/deploymenttheory/go-bindings-winrt/bindings/winrt/devices/bluetooth"
	"github.com/deploymenttheory/go-bindings-winrt/bindings/winrt/devices/bluetooth/advertisement"
	radios "github.com/deploymenttheory/go-bindings-winrt/bindings/winrt/devices/radios"
	"github.com/deploymenttheory/go-bindings-winrt/bindings/winrt/management/deployment"
	"github.com/deploymenttheory/go-bindings-winrt/bindings/winrt/management/workplace"
)

// The Bluetooth + Management surface's live proof, hardware-independent by
// design: every test is well-formed both on hosts WITH a Bluetooth radio and
// on hosts without one (headless CI runners), and both for elevated and
// unelevated package queries — the branch actually taken is t.Log'd so a run
// records what the host allowed.

// requireHResult asserts a live-call error carries an HRESULT (the error
// shape every generated method returns via win32.ErrIfFailed) and returns it.
func requireHResult(t *testing.T, err error, context string) win32.HRESULT {
	t.Helper()
	var hr win32.HRESULT
	if !errors.As(err, &hr) {
		t.Fatalf("%s error does not carry an HRESULT: %v", context, err)
	}
	return hr
}

// defaultBluetoothAdapter awaits BluetoothAdapter.GetDefaultAsync. A host
// without Bluetooth completes the operation with a nil adapter (or fails it
// with an HRESULT) — both are well-formed; only a malformed error shape or a
// hung await fails. The returned adapter is nil when the host has none.
func defaultBluetoothAdapter(t *testing.T) *bluetooth.IBluetoothAdapter {
	t.Helper()
	statics, err := bluetooth.BluetoothAdapterStatics()
	if err != nil {
		t.Fatalf("BluetoothAdapterStatics: %v", err)
	}
	defer statics.Release()

	operation, err := statics.GetDefaultAsync()
	if err != nil {
		t.Fatalf("GetDefaultAsync: %v", err)
	}
	if operation == nil {
		t.Fatal("GetDefaultAsync returned a nil operation")
	}
	defer operation.Release()

	adapter, err := operation.Await()
	if err != nil {
		hr := requireHResult(t, err, "GetDefaultAsync().Await()")
		t.Logf("no Bluetooth adapter: Await failed well-formed with HRESULT 0x%08x: %v", uint32(hr), err)
		return nil
	}
	if adapter == nil {
		t.Log("no Bluetooth adapter: Await returned a nil adapter")
		return nil
	}
	t.Cleanup(func() { adapter.Release() })
	return adapter
}

// TestBluetoothAdapterGetDefaultLive drives the generated async surface of a
// brand-new namespace end to end: statics accessor → IAsyncOperation
// monomorphization → synthesized Await. With an adapter present it reads the
// capability properties; without one it asserts the nil/error contract.
func TestBluetoothAdapterGetDefaultLive(t *testing.T) {
	adapter := defaultBluetoothAdapter(t)
	if adapter == nil {
		return // the no-hardware branch was asserted and logged above
	}

	deviceID, err := adapter.DeviceId()
	if err != nil {
		t.Fatalf("DeviceId: %v", err)
	}
	if deviceID == "" {
		t.Error("DeviceId is empty for a live adapter")
	}
	address, err := adapter.BluetoothAddress()
	if err != nil {
		t.Fatalf("BluetoothAddress: %v", err)
	}
	lowEnergy, err := adapter.IsLowEnergySupported()
	if err != nil {
		t.Fatalf("IsLowEnergySupported: %v", err)
	}
	classic, err := adapter.IsClassicSupported()
	if err != nil {
		t.Fatalf("IsClassicSupported: %v", err)
	}
	central, err := adapter.IsCentralRoleSupported()
	if err != nil {
		t.Fatalf("IsCentralRoleSupported: %v", err)
	}
	t.Logf("adapter %s: address=%012x lowEnergy=%v classic=%v central=%v",
		deviceID, address, lowEnergy, classic, central)
}

// TestBluetoothLEAdvertisementWatcherLive proves the watcher's activation,
// state machine, and event surface without requiring an advertisement to
// arrive: construct (direct activation), Status must start Created, a
// Received handler registers and removes cleanly, and — only when the host
// has a BLE-capable central-role adapter — Start/Stop round-trips the
// documented states.
func TestBluetoothLEAdvertisementWatcherLive(t *testing.T) {
	watcher, err := advertisement.NewBluetoothLEAdvertisementWatcher()
	if err != nil {
		t.Fatalf("NewBluetoothLEAdvertisementWatcher: %v", err)
	}
	defer watcher.Release()

	status, err := watcher.Status()
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if status != advertisement.BluetoothLEAdvertisementWatcherStatusCreated {
		t.Errorf("fresh watcher status = %v, want Created", status)
	}

	// Event registration must work with no radio at all — no advertisement
	// is required (or waited for) to fire. Register and remove once to prove
	// the full add/remove round-trip in isolation.
	handler, err := advertisement.NewTypedEventHandlerOfBluetoothLEAdvertisementWatcherAndBluetoothLEAdvertisementReceivedEventArgs(
		func(sender *advertisement.IBluetoothLEAdvertisementWatcher, args *advertisement.IBluetoothLEAdvertisementReceivedEventArgs) {
			// Advertisements may genuinely arrive while started; nothing to assert.
		})
	if err != nil {
		t.Fatalf("NewTypedEventHandlerOf...ReceivedEventArgs: %v", err)
	}
	defer handler.Close()
	token, err := watcher.AddReceived(handler)
	if err != nil {
		t.Fatalf("AddReceived: %v", err)
	}
	if token.Value == 0 {
		t.Error("AddReceived registration token is zero")
	}
	if err := watcher.RemoveReceived(token); err != nil {
		t.Fatalf("RemoveReceived: %v", err)
	}

	// The OS refuses Start on a watcher with no Received subscription
	// (E_ILLEGAL_METHOD_CALL, verified live), so re-register the handler and
	// keep it attached across Start/Stop.
	if token, err = watcher.AddReceived(handler); err != nil {
		t.Fatalf("AddReceived (for Start): %v", err)
	}
	defer func() {
		if err := watcher.RemoveReceived(token); err != nil {
			t.Errorf("RemoveReceived (after Start/Stop): %v", err)
		}
	}()

	adapter := defaultBluetoothAdapter(t)
	if adapter == nil {
		t.Log("skipping Start/Stop: host has no Bluetooth adapter")
		return
	}
	lowEnergy, err := adapter.IsLowEnergySupported()
	if err != nil {
		t.Fatalf("IsLowEnergySupported: %v", err)
	}
	central, err := adapter.IsCentralRoleSupported()
	if err != nil {
		t.Fatalf("IsCentralRoleSupported: %v", err)
	}
	if !lowEnergy || !central {
		t.Logf("skipping Start/Stop: adapter lowEnergy=%v central=%v", lowEnergy, central)
		return
	}
	// A present adapter whose radio is switched off (airplane mode, the
	// Settings toggle) refuses Start with E_ILLEGAL_METHOD_CALL on current
	// builds — chase GetRadioAsync (a second awaited async, via the
	// Devices.Radios closure package) and only Start a radio that is On.
	radioOperation, err := adapter.GetRadioAsync()
	if err != nil {
		t.Fatalf("GetRadioAsync: %v", err)
	}
	defer radioOperation.Release()
	radio, err := radioOperation.Await()
	if err != nil {
		hr := requireHResult(t, err, "GetRadioAsync().Await()")
		t.Logf("skipping Start/Stop: radio unavailable, HRESULT 0x%08x: %v", uint32(hr), err)
		return
	}
	if radio == nil {
		t.Log("skipping Start/Stop: adapter has no radio object")
		return
	}
	defer radio.Release()
	radioState, err := radio.State()
	if err != nil {
		t.Fatalf("Radio.State: %v", err)
	}
	if radioState != radios.RadioStateOn {
		t.Logf("skipping Start/Stop: radio state = %v", radioState)
		return
	}

	if err := watcher.Start(); err != nil {
		// A present-but-unusable radio (disabled in firmware, denied by
		// policy) may still refuse; that is a well-formed HRESULT, not a hang.
		hr := requireHResult(t, err, "Start")
		t.Logf("Start refused well-formed with HRESULT 0x%08x: %v", uint32(hr), err)
		return
	}
	// The Created → Started transition is asynchronous (observed ~20ms
	// live); poll briefly rather than asserting the instantaneous value.
	status = pollWatcherStatus(t, watcher, advertisement.BluetoothLEAdvertisementWatcherStatusCreated)
	t.Logf("watcher started: status=%v", status)
	switch status {
	case advertisement.BluetoothLEAdvertisementWatcherStatusStarted,
		advertisement.BluetoothLEAdvertisementWatcherStatusAborted: // radio dropped
	default:
		t.Errorf("status after Start = %v, want Started or Aborted", status)
	}

	if status == advertisement.BluetoothLEAdvertisementWatcherStatusStarted {
		if err := watcher.Stop(); err != nil {
			t.Fatalf("Stop: %v", err)
		}
		status = pollWatcherStatus(t, watcher, advertisement.BluetoothLEAdvertisementWatcherStatusStopping)
		t.Logf("watcher stopped: status=%v", status)
		switch status {
		case advertisement.BluetoothLEAdvertisementWatcherStatusStopped,
			advertisement.BluetoothLEAdvertisementWatcherStatusCreated, // observed post-Stop resting state
			advertisement.BluetoothLEAdvertisementWatcherStatusAborted:
		default:
			t.Errorf("status after Stop = %v, want Stopped/Created/Aborted", status)
		}
	}
}

// pollWatcherStatus reads the watcher's Status until it leaves the given
// transitional state (or five seconds pass — the observed transitions take
// tens of milliseconds) and returns the settled value.
func pollWatcherStatus(t *testing.T, watcher *advertisement.BluetoothLEAdvertisementWatcher, transitional advertisement.BluetoothLEAdvertisementWatcherStatus) advertisement.BluetoothLEAdvertisementWatcherStatus {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		status, err := watcher.Status()
		if err != nil {
			t.Fatalf("Status: %v", err)
		}
		if status != transitional || time.Now().After(deadline) {
			return status
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// iteratePackages walks up to limit entries of a package query, asserting
// each has a non-empty full name, and returns how many it saw.
func iteratePackages(t *testing.T, packages *deployment.IIterableOfPackage, limit int) int {
	t.Helper()
	iterator, err := packages.First()
	if err != nil {
		t.Fatalf("IIterableOfPackage.First: %v", err)
	}
	defer iterator.Release()

	seen := 0
	hasCurrent, err := iterator.HasCurrent()
	if err != nil {
		t.Fatalf("HasCurrent: %v", err)
	}
	for hasCurrent && seen < limit {
		pkg, err := iterator.Current()
		if err != nil {
			t.Fatalf("Current: %v", err)
		}
		id, err := pkg.Id()
		if err != nil {
			pkg.Release()
			t.Fatalf("Package.Id: %v", err)
		}
		fullName, err := id.FullName()
		id.Release()
		pkg.Release()
		if err != nil {
			t.Fatalf("PackageId.FullName: %v", err)
		}
		if fullName == "" {
			t.Error("package has an empty full name")
		}
		if seen == 0 {
			t.Logf("first package: %s", fullName)
		}
		seen++
		if hasCurrent, err = iterator.MoveNext(); err != nil {
			t.Fatalf("MoveNext: %v", err)
		}
	}
	return seen
}

// TestPackageManagerFindPackagesLive drives Windows.Management.Deployment's
// PackageManager: direct activation, the current-user query (empty security
// id = the caller — no elevation needed), and iteration through the
// monomorphized IIterable<Package>/IIterator<Package> pair. The all-users
// FindPackages needs admin rights on most hosts, so a well-formed
// access-denied HRESULT is an accepted (and logged) outcome there.
func TestPackageManagerFindPackagesLive(t *testing.T) {
	manager, err := deployment.NewPackageManager()
	if err != nil {
		t.Fatalf("NewPackageManager: %v", err)
	}
	defer manager.Release()

	// Current user: must succeed unelevated, and every Windows session has
	// at least one package registered for its user.
	packages, err := manager.FindPackagesByUserSecurityId("")
	if err != nil {
		t.Fatalf("FindPackagesByUserSecurityId(current user): %v", err)
	}
	defer packages.Release()
	if seen := iteratePackages(t, packages, 5); seen == 0 {
		t.Error("current user has zero packages")
	} else {
		t.Logf("current-user query: iterated %d package(s)", seen)
	}

	// All users: elevation-dependent — assert the error shape when denied.
	allPackages, err := manager.FindPackages()
	if err != nil {
		hr := requireHResult(t, err, "FindPackages(all users)")
		t.Logf("all-users query denied well-formed with HRESULT 0x%08x: %v", uint32(hr), err)
		return
	}
	defer allPackages.Release()
	t.Logf("all-users query: iterated %d package(s) (elevated or default-permitted)",
		iteratePackages(t, allPackages, 5))
}

// TestMdmAllowPolicyLive reads Windows.Management.Workplace's MDM allow
// policies — pure statics, no enrollment required: on an unmanaged host they
// report the "allowed" defaults; on a managed host whatever policy applies.
// Either way the calls must succeed with readable booleans.
func TestMdmAllowPolicyLive(t *testing.T) {
	statics, err := workplace.MdmAllowPolicyStatics()
	if err != nil {
		t.Fatalf("MdmAllowPolicyStatics: %v", err)
	}
	defer statics.Release()

	browser, err := statics.IsBrowserAllowed()
	if err != nil {
		t.Fatalf("IsBrowserAllowed: %v", err)
	}
	camera, err := statics.IsCameraAllowed()
	if err != nil {
		t.Fatalf("IsCameraAllowed: %v", err)
	}
	store, err := statics.IsStoreAllowed()
	if err != nil {
		t.Fatalf("IsStoreAllowed: %v", err)
	}
	t.Logf("MDM allow policies: browser=%v camera=%v store=%v", browser, camera, store)
}
