//go:build windows && (amd64 || arm64)

// Command bluetooth reads the default Bluetooth adapter's capabilities and —
// when the host has a powered-on BLE-capable radio — runs a ~3 second BLE
// advertisement scan, printing each advertiser's address, signal strength,
// and local name through the typed Received event handler.
//
// Every hardware-dependent step degrades gracefully: a host with no adapter,
// a radio switched off, or a scan the OS refuses prints the situation and
// exits cleanly instead of crashing.
package main

import (
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/deploymenttheory/go-bindings-winrt/bindings/winrt/devices/bluetooth"
	"github.com/deploymenttheory/go-bindings-winrt/bindings/winrt/devices/bluetooth/advertisement"
	"github.com/deploymenttheory/go-bindings-winrt/bindings/winrt/devices/radios"
)

func main() {
	adapter := defaultAdapter()
	if adapter == nil {
		return // situation already printed
	}
	defer adapter.Release()

	// Capability properties.
	address, err := adapter.BluetoothAddress()
	if err != nil {
		log.Fatalf("BluetoothAddress: %v", err)
	}
	lowEnergy, err := adapter.IsLowEnergySupported()
	if err != nil {
		log.Fatalf("IsLowEnergySupported: %v", err)
	}
	classic, err := adapter.IsClassicSupported()
	if err != nil {
		log.Fatalf("IsClassicSupported: %v", err)
	}
	central, err := adapter.IsCentralRoleSupported()
	if err != nil {
		log.Fatalf("IsCentralRoleSupported: %v", err)
	}
	peripheral, err := adapter.IsPeripheralRoleSupported()
	if err != nil {
		log.Fatalf("IsPeripheralRoleSupported: %v", err)
	}
	fmt.Printf("adapter %012x: lowEnergy=%v classic=%v central=%v peripheral=%v\n",
		address, lowEnergy, classic, central, peripheral)

	if !lowEnergy || !central {
		fmt.Println("adapter cannot scan for BLE advertisements (needs low-energy + central role); done")
		return
	}

	// The radio's power state gates a scan: Start on a switched-off radio
	// (airplane mode, the Settings toggle) fails with E_ILLEGAL_METHOD_CALL.
	radioOperation, err := adapter.GetRadioAsync()
	if err != nil {
		log.Fatalf("GetRadioAsync: %v", err)
	}
	defer radioOperation.Release()
	radio, err := radioOperation.Await()
	if err != nil {
		fmt.Printf("radio unavailable: %v; done\n", err)
		return
	}
	if radio == nil {
		fmt.Println("adapter has no radio object; done")
		return
	}
	defer radio.Release()
	state, err := radio.State()
	if err != nil {
		log.Fatalf("Radio.State: %v", err)
	}
	if state != radios.RadioStateOn {
		fmt.Printf("radio is not on (state %d); skipping the scan\n", state)
		return
	}

	scan()
}

// defaultAdapter awaits BluetoothAdapter.GetDefaultAsync. A host without
// Bluetooth completes the operation with a nil adapter (or a well-formed
// error) — both print the situation and return nil.
func defaultAdapter() *bluetooth.IBluetoothAdapter {
	statics, err := bluetooth.BluetoothAdapterStatics()
	if err != nil {
		log.Fatalf("BluetoothAdapterStatics: %v", err)
	}
	defer statics.Release()
	operation, err := statics.GetDefaultAsync()
	if err != nil {
		log.Fatalf("GetDefaultAsync: %v", err)
	}
	defer operation.Release()
	adapter, err := operation.Await()
	if err != nil {
		fmt.Printf("no Bluetooth adapter: %v\n", err)
		return nil
	}
	if adapter == nil {
		fmt.Println("no Bluetooth adapter on this host")
		return nil
	}
	return adapter
}

// scan runs a BLE advertisement watcher for ~3 seconds, printing each
// distinct advertiser once through the typed Received handler.
func scan() {
	watcher, err := advertisement.NewBluetoothLEAdvertisementWatcher()
	if err != nil {
		log.Fatalf("NewBluetoothLEAdvertisementWatcher: %v", err)
	}
	defer watcher.Release()

	// The handler runs on a fresh goroutine per advertisement, concurrently
	// with main — guard shared state. Its pointer arguments are borrowed:
	// read what you need inside the callback, never retain them.
	var mu sync.Mutex
	seen := map[uint64]bool{}
	handler, err := advertisement.NewTypedEventHandlerOfBluetoothLEAdvertisementWatcherAndBluetoothLEAdvertisementReceivedEventArgs(
		func(sender *advertisement.IBluetoothLEAdvertisementWatcher, args *advertisement.IBluetoothLEAdvertisementReceivedEventArgs) {
			address, err := args.BluetoothAddress()
			if err != nil {
				return
			}
			rssi, err := args.RawSignalStrengthInDBm()
			if err != nil {
				return
			}
			name := ""
			if adv, err := args.Advertisement(); err == nil && adv != nil {
				name, _ = adv.LocalName()
				adv.Release() // Advertisement() returned a new reference we own
			}
			mu.Lock()
			defer mu.Unlock()
			if seen[address] {
				return
			}
			seen[address] = true
			fmt.Printf("  %012x  %4d dBm  %s\n", address, rssi, name)
		})
	if err != nil {
		log.Fatalf("NewTypedEventHandlerOf...ReceivedEventArgs: %v", err)
	}
	defer handler.Close()

	// The OS refuses Start on a watcher with no Received subscription
	// (E_ILLEGAL_METHOD_CALL), so register before starting.
	token, err := watcher.AddReceived(handler)
	if err != nil {
		log.Fatalf("AddReceived: %v", err)
	}
	defer func() {
		if err := watcher.RemoveReceived(token); err != nil {
			log.Fatalf("RemoveReceived: %v", err)
		}
	}()

	if err := watcher.Start(); err != nil {
		fmt.Printf("scan refused: %v; done\n", err)
		return
	}
	fmt.Println("scanning for BLE advertisements (~3s):")
	time.Sleep(3 * time.Second)
	if err := watcher.Stop(); err != nil {
		log.Fatalf("Stop: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	fmt.Printf("scan complete: %d distinct advertiser(s)\n", len(seen))
}
