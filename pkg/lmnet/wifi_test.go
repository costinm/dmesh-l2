package lmnet

import (
	"encoding/json"
	"log"
	"testing"

	"github.com/costinm/wpgate/pkg/mesh"
	"github.com/costinm/wpgate/pkg/msgs"
	uds2 "github.com/costinm/wpgate/pkg/transport/uds"
)

// Assumes the devices have a stable connection to the mesh - primary Wifi for Q or devices allowing client P2P,
// USB/ethernet or other.
//
// Devices are reached using their mesh LL addresse. Assume a dmesh node on localhost.
//
func TestRemote(t *testing.T) {
	// q devices

	//

}

// Requires dmroot to be running as root, and allow all or this user to connect
// See uds_test for general uds testing

func TestP2P(t *testing.T) {

	msg := msgs.NewChannelHandler()

	msgs.DefaultMux.AddHandler("", msg)

	u, err := uds2.Dial("dmesh", msgs.DefaultMux, map[string]string{})
	if err != nil {
		t.Fatal("Can't connect to dmroot or android, make sure it's running as root/netcap ", err)
	}

	go u.HandleStream()

	scan := &mesh.ScanResults{}
	for i := 0; i < 4; i++ {
		u.SendMessageDirect("/wifi/scan", nil, nil)

		m := msg.WaitEvent("/wifi/status")
		log.Print(m)
		if m.Data != nil {
			json.Unmarshal(m.Data.([]byte), &scan)
			if len(scan.Scan) > 0 {
				break
			}
		}
	}
	if len(scan.Scan) == 0 {
		t.Fatal("Expecting DIRECT- dmesh device in range")
	}

	u.SendMessageDirect("/wifi/disc", nil, nil)

	m := msg.WaitEvent("/wifi/discovered")
	log.Print(m)

	m = msg.WaitEvent("/wifi/status")
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

	var md *mesh.MeshDevice
	for _, s := range scan.Scan {
		if s.PSK != "" {
			md = s
			break
		}
	}
	if md == nil {
		t.Fatal("No PSK")
	}

	u.SendMessageDirect("/wifi/con/peer/"+md.SSID+"/"+md.PSK, nil, nil)

	m = msg.WaitEvent("/wifi/con")

}
