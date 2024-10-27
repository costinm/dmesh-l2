package lmnet

import (
	context2 "context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	mesh "github.com/costinm/dmesh-l2/pkg/l2api"
	"github.com/costinm/ugate/pkg/local"
	msgs "github.com/costinm/ugate/webpush"
)

// Wifi package interacts with platform or remote Wifi interfaces, keeps track of visible AP and
// wifi-related discovery.

// It has not external dependencies, communicates using Messages.

// Persisted device info.
type MeshInfo struct {
	SSID     string    `json:"s,omitempty"`
	PSK      string    `json:"p,omitempty"`
	LastSeen time.Time `json:"-"`
	Name     string    `json:"N,omitempty"`
}

// Wifi controls the Wifi interface - DMesh currently uses P2P and APs with DM- names and fixed PSK.
// The actual low level implementation is part of the native android app, or a separate process
// running as root (or net_admin CAP) and using WPA directly. Messages are used to communicate with the
// real wifi implementation.
type Wifi struct {
	Mesh *local.LLDiscovery

	mutex sync.RWMutex

	// Full database of found devices (by anyone). May be trimmed periodically
	// ( x hours ). Will be saved to speed up discovery. Will be sent to parent
	// and children.
	MeshInfo map[string]*MeshInfo

	// Current visible wifi devices, with last level (from this device perspective)
	MeshDevices map[string]*mesh.MeshDevice

	// Mesh devices visible from this device.
	VisibleDevices map[string]*mesh.MeshDevice

	// Connection to the master. Only used on non-android
	SendMessage *msgs.MsgConnection

	// Last scan results (raw). Includes known and DIRECT networks.
	Scan     map[string]*mesh.MeshDevice
	ScanTime time.Time

	DiscoveryTime time.Time
	DiscoveryCnt  int

	// True if the database needs to be saved, for fast reload.
	// Periodic thread will save.
	NeedsSave bool
	cb        WifiCallbacks
}

type WifiCallbacks interface {
	// Called when a network event is received that would require network refresh.
	RefreshNetworks()
}

type ByLevel []*mesh.MeshDevice

func (a ByLevel) Len() int           { return len(a) }
func (a ByLevel) Swap(i, j int)      { a[i], a[j] = a[j], a[i] }
func (a ByLevel) Less(i, j int) bool { return a[i].Level < a[j].Level }

// Message from the wpa layer:
// /wifi/status
func (wifi *Wifi) HandleMessage(ctx context2.Context, cmdS string, meta map[string]string, data []byte) {
	cmd := strings.Split(cmdS, "/")
	if len(cmd) < 2 || (cmd[1] != "wifi" && cmd[1] != "net") {
		return
	}
	switch cmd[2] {
	case "status":
		// visible,event, data JSON { scan:[], ...}
		jsonData := meta["data"]
		if jsonData != "" {
			scan := &mesh.ScanResults{}
			json.Unmarshal([]byte(jsonData), &scan)
			log.Println("WIFI STATUS ", meta, scan)
			wifi.OnNetStatus(meta, scan)
		} else {
			scan := &mesh.ScanResults{}
			json.Unmarshal(data, &scan)
			wifi.OnNetStatus(meta, scan)
			log.Println("WIFI STATUS2 ", meta, scan)

		}
	}
}

// OnNetStatus is called when the WPA or Android layer gets a Wifi update - changed
// list of visible Mesh nodes, or periodic update, or changed signal.
//
// Will decide if a connection should be started or changed.
// This moved to native side, since it has better visibility of the mesh and can
// avoid discovery.
func (wifi *Wifi) OnNetStatus(meta map[string]string, scanR *mesh.ScanResults) {
	scan := map[string]*mesh.MeshDevice{}

	for _, s := range scanR.Scan {
		old, f := scan[s.SSID]
		if !f || old.Level < s.Level {
			scan[s.SSID] = s
		}
	}

	newWifi := meta["w"]
	if newWifi != wifi.Mesh.ConnectedWifi {
		wifi.cb.RefreshNetworks()
		//wifi.
		if newWifi != "" {
			wifi.OnConnect(newWifi)
		}
	}
	wifi.Mesh.ConnectedWifi = newWifi

	wifi.Mesh.WifiFreq = meta["f"]
	wifi.Mesh.WifiLevel = meta["l"]
	if meta["s"] != "" {
		wifi.Mesh.AP = meta["s"]
	}
	if meta["p"] != "" {
		wifi.Mesh.PSK = meta["p"]
	}

	wifi.Mesh.APRunning = meta["ap"] == "1"
	wifi.Scan = scan
	wifi.ScanTime = time.Now()

	wifi.VisibleDevices = map[string]*mesh.MeshDevice{}

	for ssid, v := range wifi.Scan {
		// Known networks (pre-Q or with whitelisting), DM- AP
		//if !strings.HasPrefix(ssid, "DIRECT-") ||
		//	strings.HasPrefix(ssid, "DIRECT-DM-ESH") {
		//	wifi.VisibleDevices[ssid] = v
		//	// &MeshDevice{Level: v.Level, SSID: ssid, Freq: v.Freq}
		//	continue
		//}
		// DIRECT
		if md, f := wifi.MeshInfo[ssid]; f {
			// TODO: check wifi connection, initiate connection if needed.
			if md.PSK != "" {
				if v.PSK == "" {
					v.PSK = md.PSK
				} else if v.PSK != md.PSK {
					md.PSK = v.PSK
					wifi.NeedsSave = true
					log.Println("PSK change ", v.SSID, md.PSK, v.PSK)
				}
			}
		} else if v.PSK != "" {
			md = &MeshInfo{
				SSID: v.SSID,
				PSK:  v.PSK,
				Name: v.Name,
			}
			wifi.NeedsSave = true
			wifi.MeshInfo[ssid] = md
		}

		wifi.VisibleDevices[ssid] = v
	}

}

// Notification that the device SSID changed. Empty if no wifi connection.
func (wifi *Wifi) OnConnect(ssid string) {
	wifi.Mesh.WifiInfo.Net = ssid
}

// TODO: json, updates
func (wifi *Wifi) ScanStatus() string {
	w := []string{}
	if wifi == nil {
		return "-"
	}
	for k, v := range wifi.Scan {
		p2p, f := wifi.MeshDevices[k]
		if f {
			w = append(w, fmt.Sprintf("%s/%s/%d/%d", k, p2p.PSK, v.Freq, v.Level))
		} else {
			w = append(w, fmt.Sprintf("%s/%d/%d", k, v.Freq, v.Level))
		}
	}
	return fmt.Sprintf("%d/%d %s", len(wifi.Scan), len(wifi.MeshDevices), strings.Join(w, ", "))
}

type WifiCtl interface {
	Scan()
	Connect(ssid, pass string)
	StartAP(t time.Duration)
}

// Create the Wifi object, using the UDS channel for communication.
func NewWifi(cb WifiCallbacks, u *msgs.MsgConnection,
	msh *local.LLDiscovery) *Wifi {
	c := &Wifi{
		Scan:           map[string]*mesh.MeshDevice{},
		MeshInfo:       map[string]*MeshInfo{},
		MeshDevices:    map[string]*mesh.MeshDevice{},
		VisibleDevices: map[string]*mesh.MeshDevice{},
		Mesh:           msh,
		cb:             cb,
	}
	c.SendMessage = u

	// /wifi/status is the main message sent when the low level Wifi layer has changed.
	// "data" field has the discovery data.
	msgs.DefaultMux.AddHandler("wifi", c)
	return c // ok
}
