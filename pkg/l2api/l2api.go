package l2api

import (
	"fmt"
	"time"
)

// JSON structures used for broadcasting P2P / mesh device information
// Save to copy, to avoid a direct dependency on this package.

// The information is extracted from WPA, nan, BLE discovery.
// May also aggregate LoRA, via ESP32/Lora driver - and in future BT classic and other protocols.

// This uses 'wpgate' package, implementing a webpush encryption and model - as well as an abstraction
// for message mux and integration with message brokers.




// TODO: proto file


// L2 device status and discovery info.
type L2 struct {
	// Visible devices at this moment, according to last scan results.
	// This only includes discovered P2P devices.
	Scan []*MeshDevice `json:"scan,omitempty"`

	Stats string `json:"stat,omitempty"`

	// Visible wifi networks (all kinds), on last scan result - including non-dmesh
	Visible int `json:"visible,omitempty"`

	// My SSID and PSK, when acting as a P2P or AP server
	SSID string `json:"s,omitempty"`
	PSK  string `json:"p,omitempty"`

	// AP we are connected to, if operating as STA or P2P client.
	ConnectedWifi string `json:"w,omitempty"`

	// Frequency on the STA connection, from wpa_cli if available
	Freq int `json:"f,omitempty"`
	// Last level of AP signal, from wpa_cli
	Level int `json:"l,omitempty"`
}

// Info about a L2-connected device. Originally used for Android P2P L2 connections.
type MeshDevice struct {

	// If the device has a P2P or AP interface, the SSID and PSK of the device,
	// They may be obtained via P2P discovery or other means (nan, etc)
	SSID string `json:"s,omitempty"`
	// PSK - not included if it is the default or open network. To distinguish, use info from the beacon.
	PSK  string `json:"p,omitempty"`

	// MAC is used with explicit P2P connect ( i.e. no hacks )
	// User input required on the receiving end ( PBC ).
	// This is the MAC of the P2P interface, as reported.
	// For BLE/NAN, it is the current MAC of the device.
	MAC string `json:"d,omitempty"`

	// Name, from the discovery.
	// Deprecated - should be at L6
	Name string `json:"N,omitempty"`

	// Set only if the device is currently visible in a recent scan.
	// Equivalent RSSI for BLE, Nan, etc - based on last packet
	Level int `json:"l,omitempty"`

	// Freq the device is listening on
	Freq  int `json:"f,omitempty"`

	// Metadata extracted from DIRECT DNSSD
	// Deprecated: L6/control
	UserAgent string `json:"ua,omitempty"`

	//
	Net       string `json:"n,omitempty"`

	// Capabilities and BSSID
	Cap   string `json:"c,omitempty"`
	BSSID string `json:"b,omitempty"`

	LastSeen time.Time `json:"lastSeen,omitempty"`

	//Self int `json:"self,omitempty"`

	// Only on supplicant, not on android. Will change when the DNS-SD data changes.
	ServiceUpdateInd int `json:"sui,omitempty"`
}

func (md *MeshDevice) String() string { return fmt.Sprintf("%s/%d", md.SSID, md.Level) }
