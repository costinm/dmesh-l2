package l2

import (
	"context"
	"encoding/hex"
	"log"
	"testing"
	"time"

	"github.com/costinm/wpgate/pkg/msgs"
)

// Some machines have BLE, other WifiDirect - but it seems emulated WifiAware is more common on
// older devices !

var (
	mux = msgs.DefaultMux
	l2  = NewL2(mux)
)

func init() {
	mux.AddHandler("*", msgs.HandlerCallbackFunc(func(ctx context.Context, cmdS string, meta map[string]string,
		data []byte) {
		log.Println("RECEIVED ", cmdS, data)
	}))

}

// Send and receive L2 frames using NAN/WifiAware encapsulation
//
// Should interoperate with Android and ESP32 if the interface is on channel 6
//
// Will work with ESP32 if the interface is not on channel 6 - but not android.
func TestWifiAware(t *testing.T) {
	err := l2.InitWifi()
	if err != nil {
		t.Fatal("No wifi", err)
	}

	if len(l2.actWifi) == 0 {
		t.Fatal("No active wifi")
	}
	if len(l2.physMon) == 0 {
		t.Fatal("No active mon")
	}

	// Now we should have a connection, send and receive a message
	time.Sleep(10 * time.Second)

}

// Send and receive L2 frames using a WifiDirect/P2P connection.
// Devices are both assumed to be in link local mode.
//
func TestWifiDirect(t *testing.T) {
	wpa, err := l2.NewWPA("", 0, "")
	if err != nil {
		t.Fatal(err)
	}

	log.Println("WPA", wpa.Interfaces)

	// WPA is controlled via messages.

	for _, wi := range wpa.Interfaces {
		wi.P2PDiscover()
	}
	time.Sleep(10 * time.Second)

}

// Send and receive L2 frames over BLE GATT.
// Uses UUID advertisments (eddystone), with Proxy characteristics.
//
// Requires BLE support on the device.
func TestBle(t *testing.T) {
	// Will start scanning automatically (b.Scan)
	ble, err := l2.InitBLE()
	if err != nil {
		t.Fatal(err)
	}

	err = ble.Scan(3 * time.Second)
	if err != nil {
		// No HCI support - report
		t.Log("No BLE support ", err)
		return
	}

	ble.CleanOlder(10 * time.Second)

	log.Println(ble.nodes)

	for _, v := range ble.nodes {
		a := v.adv
		log.Println("ADV: ",
			a.Addr(),
			"rssi:", a.RSSI(),
			"tx", a.TxPowerLevel(), //- not included
			//a.ManufacturerData(), // additional info - for example watch
			//a.OverflowService(),  // ex: fee7, fe9f, fe50. For dmesh - [feaa]
			//a.Services(),         // ex: fee7 - For dmesh - [feaa]
			//a.SolicitedService(), // []
			// a.ServiceData(), // feaa [...]
			string(a.ServiceData()[0].Data),
			hex.EncodeToString(a.ServiceData()[0].Data)) // ex: feaa: [...],

		err = ble.maybeConnect(v)
		if err != nil {
			t.Error("Failed to connect ", v.Addr, err)
			continue
		}

		err := v.SendMessageToRemote(&msgs.Message{
			Data: []byte("/PINGBLE"),
		})
		if err != nil {
			t.Error("Failed to send", err)
			continue
		}

		// Now we should have a connection, send and receive a message
		time.Sleep(10 * time.Second)
	}

}
