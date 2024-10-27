package l2

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"log"
	"strings"
	"testing"
	"time"

	"github.com/costinm/dmesh-l2/pkg/l2api"
	msgs "github.com/costinm/ugate/webpush"
)

// Some machines have BLE, other WifiDirect - but it seems emulated WifiAware is more common on
// older devices !

var (
	mux   = msgs.DefaultMux
	l2    = NewL2(mux)
	msgCh = msgs.NewChannelHandler()
)

func init() {

	mux.AddHandler("*", msgs.HandlerCallbackFunc(func(ctx context.Context, cmdS string, meta map[string]string,
		data []byte) {
		log.Println("RECEIVED ", cmdS, data)
	}))

	mux.AddHandler("*", msgCh)

}

// WIP: connect to a different process or host, running a Mux.
// The tests can send command and receive messages from the remote host - can run as regular
// user.
func connectL2Host(path string) {
	// For now connect via UDS to the dml2 process running as  root.
	//
	u, err := uds2.Dial("dmesh", msgs.DefaultMux, map[string]string{})
	if err != nil {
		log.Println("Can't connect to dmroot or android, make sure it's running as root/netcap ", err)
	}

	// Will register the connection to the mux, with wifi, net, etc
	go u.HandleStream()

}

// Send and receive L2NetStatus frames using NAN/WifiAware encapsulation
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

func disc(t *testing.T) {
}

// Send and receive L2NetStatus frames using a WifiDirect/P2P connection.
// Devices are both assumed to be in link local mode.
//
func TestWifiDirect(t *testing.T) {
	wpa, err := l2.NewWPA("", 0, "")
	if err != nil {
		t.Fatal(err)
	}
	mux.AddHandler("wifi", wpa)

	log.Println("WPA", wpa.Interfaces)

	// WPA is controlled via messages.
	scan := &l2api.L2NetStatus{}
	t.Run("scan", func(t *testing.T) {
		for i := 0; i < 4; i++ {
			log.Println("Send scan request ")
			mux.Send("/wifi/scan", nil)

			// About 7 seconds
			m := msgCh.WaitEvent("/net/status")

			if m != nil && m.Data != nil {
				log.Println("Got scan result ")
				json.Unmarshal(m.Data.([]byte), &scan)
				if len(scan.Scan) > 0 {
					break
				}
			}
		}
		log.Println(scan)

		if len(scan.Scan) == 0 {
			t.Fatal("Expecting DIRECT- dmesh device in range")
		}
	})

	t.Run("disc", func(t *testing.T) {
		mux.Send("/wifi/disc", nil)

		m := msgCh.WaitEvent("/wifi/discovered")
		log.Print(m)

		m = msgCh.WaitEvent("/net/status")
		if m == nil {
			t.Fatal("Failed to discover")
		}
		if m.Data != nil {
			json.Unmarshal(m.Data.([]byte), &scan)
			log.Print(scan, string(m.Data.([]byte)))
		} else {
			t.Fatal("Failed to discover")
		}

		if len(scan.Scan) == 0 {
			t.Fatal("No discovered devices")
		}

		var md *l2api.MeshDevice
		for _, s := range scan.Scan {
			if strings.HasPrefix(s.SSID, "DIRECT-DM-ESH") {
				md = s
				md.PSK = "12345678"
				break
			}
			if s.PSK != "" {
				md = s
				break
			}
		}
		if md == nil {
			t.Fatal("No DMESH network")
		}

		mux.Send("/wifi/con/peer/"+md.SSID+"/"+md.PSK, nil)

		m = msgCh.WaitEvent("/wifi/constatus")

		if m == nil {
			t.Fatal("Failed to connect")
		}

		conSsid := m.Meta["ssid"]
		if conSsid == "" {
			t.Error("Failed to connect to P2P test device")
		}
	})

	t.Run("startAP", func(t *testing.T) {

	})
}

// Send and receive L2NetStatus frames over BLE GATT.
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
