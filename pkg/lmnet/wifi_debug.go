package lmnet

import (
	"encoding/json"
	"fmt"

	"github.com/costinm/wpgate/pkg/mesh"
	"github.com/costinm/wpgate/pkg/msgs"

	"log"
	"net/http"
	"sort"
	"strings"
	"time"
)

// Return the raw result of the last Wifi SCAN. /debug/scan
func (wifi *Wifi) JsonScan(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	s := r.Form.Get("s")
	if s != "" {
		if wifi.SendMessage != nil {
			wifi.SendMessage.SendMessageToRemote(msgs.NewMessage("/wifi/scan", nil))
			time.Sleep(3 * time.Second)
		}
	}

	if wifi.Scan != nil {
		je := json.NewEncoder(w)
		je.SetIndent(" ", " ")
		je.Encode(wifi.Scan)
	}
}

// Connect to strongest signal - /wifi/con
//
func (wifi *Wifi) HTTPCon(w http.ResponseWriter, r *http.Request) {
	candidates := ByLevel{}
	candidatesR := ByLevel{}

	wifi.VisibleDevices = map[string]*mesh.MeshDevice{}

	for k, v := range wifi.Scan {
		if md, f := wifi.MeshDevices[k]; f {
			md.Level = v.Level
			md.LastSeen = wifi.ScanTime
			// TODO: check wifi connection, initiate connection if needed.
			candidates = append(candidates, md)
			wifi.VisibleDevices[k] = md
		}
		if !strings.HasPrefix(k, "DIRECT") {
			candidatesR = append(candidatesR, &mesh.MeshDevice{SSID: k, PSK: "0", Level: v.Level})
		}
	}
	sort.Sort(candidates)
	sort.Sort(candidatesR)
	log.Println("Wifi: CONN: ", wifi.Mesh.WifiInfo.Net, "CONNECTABLE: ", candidates, "R:", candidatesR)

	if wifi.SendMessage != nil && wifi.Mesh.WifiInfo.Net == "" {
		// XXX Experiment
		if len(candidatesR) > 0 {
			s := fmt.Sprintf("con %s %s", candidatesR[0].SSID, candidatesR[0].PSK)
			wifi.SendMessage.SendMessageToRemote(msgs.NewMessage(s, nil))

		} else if len(candidates) > 0 {
			s := fmt.Sprintf("con %s %s", candidates[0].SSID, candidates[0].PSK)
			wifi.SendMessage.SendMessageToRemote(msgs.NewMessage(s, nil))
		}
	}
}
