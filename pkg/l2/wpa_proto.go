package l2

import (
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"strconv"
	"strings"
	"time"

	mesh "github.com/costinm/dmesh-l2/pkg/l2api"

	//"github.com/costinm/dmesh/dm/mesh"
	"github.com/costinm/wpgate/pkg/msgs"
)

// Wifi network info - list of networks registered.
// See wifiManager.getConfiguredNetworks(), plus get current wifi.
type WPANetwork struct {
	Id    int
	SSID  string
	ESSID string
	Flags string // [CURRENT]
}

// Async events from wpa socket
func (c *WifiInterface) onEvent(cmd string, data []byte, isp2pif bool) {
	msg := string(data)
	p2pif := ""
	if isp2pif {
		p2pif = "p2p"
	}

	parts := strings.Split(msg, " ")
	msg = msg[3:]

	args := map[string]string{}
	for _, m := range parts[1:] {
		mp := strings.Split(m, "=")
		if len(mp) == 1 {
			args[mp[0]] = ""
		} else {
			args[mp[0]] = mp[1]
		}
	}

	eventType := parts[0][3:] // <3>P2P...
	eventType = strings.Trim(eventType, "\n")

	switch eventType {
	case "P2P-DEVICE-LIST": // ignore, happens when find stops

	case "CTRL-EVENT-CONNECTED":
		// SME: Trying to authenticate with 32:76:6f:f2:27:da (SSID='DIRECT-Hc-Android_da85' freq=5745 MHz
		//CTRL-EVENT-CONNECTED - Connection to 32:76:6f:f2:27:da completed [id=135 id_str=]]
		log.Println("WPA_IN: ", p2pif, parts)
		c.Status()

	case "CTRL-EVENT-DISCONNECTED":
		//bssid=70:3a:cb:02:2b:3a reason=3 locally_generated=1
		log.Println("WPA_IN: ", p2pif, parts)
		c.Status()

	case "CTRL-EVENT-SIGNAL-CHANGE":
		// above=1 signal=-70 noise=9999 txrate=36000"
		log.Println("WPA_IN: ", p2pif, parts)
	case "CTRL-EVENT-BSS-ADDED":
		// 34 00:11:22:33:44:55
		// entryId MAC
		//log.Println("WPA_IN: ", p2pif, parts)

	case "CTRL-EVENT-BSS-REMOVED":
		//log.Println("WPA_IN: ", p2pif, parts)

	case "P2P-GROUP-REMOVED":
		log.Println("WPA_IN: ", p2pif, parts)
		c.OnP2PGroupStop(parts)

	case "P2P-GROUP-STARTED":
		log.Println("WPA_IN: ", p2pif, parts)
		c.OnP2PGroupStart(parts)
	case "Associated":
		// with 70:3a:cb:02:2b:3a"
		log.Println("WPA_IN: ", p2pif, parts)
	case "Trying":
		// to associate with 94:44:52:14:2e:b1 (SSID='costin' freq=2437 MHz)
		log.Println("WPA_IN: ", p2pif, parts)
	case "P2P-FIND-STOPPED":
		c.scanning = false
		// Other apps may set different service discovery params. Leaving one active
		// results in dups.
		log.Println("WPA_IN: ", p2pif, parts)
		c.SendCommand("P2P_SERV_DISC_CANCEL_REQ " + c.pendingDisc)
		go c.SendCommand("SCAN")

	case "CTRL-EVENT-SCAN-STARTED":
		// Sent at start of both scan and find ( find does a scan first )
		//c.scanning = true
	case "CTRL-EVENT-SCAN-RESULTS":
		go c.sendScanResults()

	case "P2P-DEVICE-LOST":
		log.Println("WPA_IN: ", p2pif, parts)
		// p2p_dev_addr=42:4e:36:8e:5d:e1"

	case "P2P-DEVICE-FOUND":
		log.Println("WPA_IN: ", p2pif, parts)
		c.onP2PDeviceFound(parts)

	case "P2P-SERV-DISC-REQ":
		//[<3>P2P-SERV-DISC-REQ 2412 7e:d9:5c:b4:9b:9d 0 1 0200010102000102]
		// freq, MAC, ID
		log.Println("DISCOVERY REQUEST ", parts)
		c.SendCommand("P2P_SERV_DISC_RESP " + parts[1] + " " + parts[2] + " " + parts[3] + " " +
			hex.EncodeToString(packDns(parts[5], map[string]string{
				"s": c.ssid,
				"p": c.psk,
			})))

	case "P2P-SERV-DISC-RESP":
		//P2P_SERV_DISC_RESP 5785 ae:37:43:df:1b:a5 0 02646d035f646dc01c001015733d4449524543542d69322d444d4553482d5750410a703d337333597a4d7478 -> OK
		mac := parts[1]
		old := c.P2PByMAC[mac]
		if old == nil {
			old = &mesh.MeshDevice{MAC: mac}
			c.P2PByMAC[mac] = old
		}

		b := parseDisc(parts, old)
		if b {
			c.P2PBySSID[old.SSID] = old

			h := c.wpa.mux
			if h != nil {
				c.wpa.mux.SendMessage(msgs.NewMessage("/wifi/discovered", nil).SetDataJSON(old))
			}
			log.Println("DISC: ", p2pif, parts, old)
		}
	default:
		log.Println("WPA_IN_UNKNOWN: ", p2pif, parts)
	}
}

func (wpa *WifiInterface) startWPAStream(c io.ReadWriter) error {
	c.Write([]byte("ATTACH"))
	buf := make([]byte, 2048)
	br, _ := c.Read(buf)
	ar := string(buf[0:br])
	if "OK\n" != ar {
		log.Println("Error attaching to WifiInterface ", ar)
		return errors.New("Attaching WifiInterface " + ar)
	}

	//0 = MSGDUMP
	//1 = DEBUG
	//2 = INFO - very verbose
	//3 = WARNING
	//4 = ERROR
	c.Write([]byte("LEVEL 3\n"))
	br, _ = c.Read(buf)
	ar = string(buf[0:br])
	if "OK\n" != ar {
		log.Println("Error attaching to WifiInterface ", ar)
		return errors.New("Attaching WifiInterface " + ar)
	}
	return nil
}

func partsToMap(parts []string, out map[string]string) {
	for _, record := range parts[2:] {
		if strings.Index(record, "=") != -1 {
			nvs := strings.SplitN(record, "=", 2)
			if len(nvs) < 2 {
				continue
			}
			k := nvs[0]

			v := nvs[1]
			if v[0] == '"' {
				v = v[1 : len(v)-1]
			}
			out[k] = v
		}
	}
}

// Peer found - just the 'name' and MAC
//
// <3>P2P-DEVICE-FOUND 42:4e:36:8e:5d:e1 p2p_dev_addr=42:4e:36:8e:5d:e1 pri_dev_type=10-0050F204-5
//       name='Android_656a'
//       config_methods=0x188 dev_capab=0x25 group_capab=0x2b
//       new=1
//
// Will save the name and MAC, no further events.
//
func (c *WifiInterface) onP2PDeviceFound(parts []string) {
	mac := parts[1]
	for _, record := range parts[2:] {
		if strings.HasPrefix(record, "p2p_dev_addr=") {
			k := record[13:]
			if mac != k {
				//log.Println("Mac and p2p no match ", parts)
				mac = k
			}
		}
	}

	d := c.P2PByMAC[mac]
	if d == nil {
		d = &mesh.MeshDevice{MAC: mac}
		c.P2PByMAC[mac] = d
	}
	d.LastSeen = time.Now()
	d.MAC = parts[1]

	meta := map[string]string{}
	partsToMap(parts, meta)
	d.Name = meta["name"]
}

//<3>P2P-GROUP-REMOVED p2p-wlp2s0-4 GO reason=REQUESTED
func (c *WifiInterface) OnP2PGroupStop(parts []string) {
	// TODO: if GO in parts, set as GO (may be client)
	out := map[string]string{}
	out["intf"] = c.p2pGroupInterface
	c.p2pGroupInterface = ""

	c.wpa.mux.SendMessage(msgs.NewMessage("/wifi/AP/STOP", out))
}

// Called as result of ...
// 	P2P-GROUP-STARTED p2p-wlp2s0-0 GO ssid="DIRECT-JF" freq=2437 passphrase="DKAcUzpO" go_dev_addr=38:ba:f8:49:d3:c0
func (c *WifiInterface) OnP2PGroupStart(parts []string) {
	c.p2pGroupInterface = parts[1]

	// TODO: if GO in parts, set as GO (may be client)
	out := map[string]string{}
	partsToMap(parts, out)

	out["intf"] = c.p2pGroupInterface
	c.psk = out["passphrase"]
	c.ssid = out["ssid"]

	c.SendCommand("P2P_SERVICE_FLUSH")

	time.Sleep(500 * time.Millisecond)

	c.SendCommand("P2P_SERVICE_ADD bonjour 02646d035f646dc01c001001 " + packTxt(map[string]string{"p": c.psk, "s": c.ssid}))

	// DNS NAME, C0 1C 00 10
	//c.SendCommand("P2P_SERVICE_ADD bonjour 02646d035f646dc01c001001 09747874766572733d311a70646c3d6170706c69636174696f6e2f706f7374736372797074")
	// IP Printing over TCP (TXT) (RDATA=txtvers=1,pdl=application/postscript)
	// DNS NAME, C0 0C 00 10 01
	//c.SendCommand("P2P_SERVICE_ADD bonjour 096d797072696e746572045f697070c00c001001 09747874766572733d311a70646c3d6170706c69636174696f6e2f706f7374736372797074")

	time.AfterFunc(1*time.Second, func() {
		if c.p2pGroupInterface != "" {
			go func() {
				err := DhcpServer(c.p2pGroupInterface)
				if err != nil {
					log.Println("Failed to start dhcp, need root or NET_ADMIN ", c.p2pGroupInterface, err)
				} else {
					log.Println("DHCP start ", c.p2pGroupInterface)
				}
			}()
		}
	})

	c.wpa.mux.SendMessage(msgs.NewMessage("/wifi/AP/START", out))

	// Advertise the ssid/password
}

func (c *WifiInterface) AddNetwork() (int, error) {
	return c.SendCommandInt("ADD_NETWORK")
}

func (c *WifiInterface) SetNetworkSettingRaw(networkId int, variable string, value string) error {
	return c.SendCommandBool(fmt.Sprintf("SET_NETWORK %d %s %s", networkId, variable, value))
}

func (c *WifiInterface) SetNetworkSettingString(networkId int, variable string, value string) error {
	return c.SetNetworkSettingRaw(networkId, variable, fmt.Sprintf("\"%s\"", value))
}

func (c *WifiInterface) GetNetworkSetting(networkId int, variable string) (string, error) {
	return c.SendCommand(fmt.Sprintf("GET_NETWORK %d %s", networkId, variable))
}

func (c *WifiInterface) SelectNetwork(networkId int) error {
	return c.SendCommandBool(fmt.Sprintf("SELECT_NETWORK %d", networkId))
}

func (c *WifiInterface) EnableNetwork(networkId int) error {
	return c.SendCommandBool(fmt.Sprintf("ENABLE_NETWORK %d", networkId))
}

func (c *WifiInterface) DisableNetwork(networkId int) error {
	return c.SendCommandBool(fmt.Sprintf("DISABLE_NETWORK %d", networkId))
}

func (c *WifiInterface) RemoveNetwork(networkId int) error {
	return c.SendCommandBool(fmt.Sprintf("REMOVE_NETWORK %d", networkId))
}

func (c *WifiInterface) ReloadConfiguration() error {
	return c.SendCommandBool(fmt.Sprintf("RECONFIGURE"))
}

func (c *WifiInterface) SaveConfiguration() error {
	return c.SendCommandBool(fmt.Sprintf("SAVE_CONFIG"))
}

func (c *WifiInterface) Connect(meta map[string]string, ssid, pass string) error {
	i, _ := c.AddNetwork()
	c.SetNetworkSettingString(i, "ssid", ssid)

	if strings.HasPrefix(ssid, "DIRECT-DM-ESH") || strings.HasPrefix(ssid, "DM-") {
		pass = "12345678"
	}

	//c.SetNetworkSettingRaw(i, "scan_ssid", "1")
	c.SetNetworkSettingRaw(i, "key_mgmt", "WPA-PSK")
	c.SetNetworkSettingString(i, "psk", pass)
	c.SelectNetwork(i)
	//for {
	//	event := <- wpa_ctl.EventChannel
	//	//log.Println(event)
	//	switch event.name {
	//	case "CTRL-EVENT-DISCONNECTED":
	//		log.Println("Disconnected")
	//	case "CTRL-EVENT-CONNECTED":
	//		log.Println("Connected")
	//	case "CTRL-EVENT-SSID-TEMP-DISABLED":
	//		log.Println("InvalidKey")
	//	}
	//}
	return nil
}

func (c *WifiInterface) ListNetworks() ([]*WPANetwork, error) {
	resp, err := c.SendCommand("LIST_NETWORKS")
	if err != nil {
		return nil, err
	}

	lines := strings.Split(resp, "\n")

	num_networks := len(lines) - 1
	networks := make([]*WPANetwork, num_networks)
	valid_networks := 0

	for _, line := range lines[1:] {
		fields := strings.Split(line, "\t")
		id, err := strconv.Atoi(fields[0])
		if err != nil || len(fields) != 4 {
			continue
		}
		networks[valid_networks].Id = id
		networks[valid_networks].SSID = fields[1]
		networks[valid_networks].ESSID = fields[2]
		networks[valid_networks].Flags = fields[3]
		valid_networks += 1
	}

	return networks[:valid_networks], nil
}

// start peer discovery with DNSSD. Events will be sent, discovery will auto-terminate
func (c *WifiInterface) P2PDiscover() {
	//wpa.ListNetworks()
	// list all discovery protocols
	//res, err := c.SendCommandP2P("P2P_SERV_DISC_REQ 00:00:00:00:00:00 02000001")
	// Bonjour only
	res, err := c.SendCommandP2P("P2P_SERV_DISC_REQ 00:00:00:00:00:00 02000101")
	if err != nil {
		log.Println("Error P2P_SERV_DISC_REQ", err, res)
		return
	}
	c.pendingDisc = res

	res, err = c.SendCommandP2P("P2P_FIND 6")
	if err != nil {
		log.Println("Error P2P_SERV_DISC_REQ", err, res)
	}
}

func (c *WifiInterface) Scan() {
	res, err := c.SendCommand("SCAN")
	if err != nil {
		log.Println("SCAN", err, res)
		return
	}
}

func (c *WifiInterface) APStop() {
	res, err := c.SendCommandP2P("P2P_GROUP_REMOVE *") // + c.p2pGroupInterface)
	if err != nil {
		log.Println("Error P2P_SERV_DISC_REQ", err, res)
		return
	}
}

// Attempt to start P2P AP, using channel 6 if possible so NAN can work
func (c *WifiInterface) APStart() {
	res, err := c.SendCommandP2P("P2P_SET ssid_postfix -DMESH-WPA")
	if err != nil {
		log.Println("Error P2P_SET postfix", err, res)
		return
	}
	res, err = c.SendCommandP2P("P2P_GROUP_ADD persistent freq=2437")
	if err == nil {
		return
	}
	res, err = c.SendCommandP2P("P2P_GROUP_ADD persistent")
	if err != nil {
		res, err = c.SendCommandP2P("P2P_GROUP_ADD")
		if err != nil {
			log.Println("Error P2P_GROUP_ADD", err, res)
			return
		}
	}

}

func (c *WifiInterface) Status() map[string]string {
	s, err := c.SendCommand("STATUS")
	if err != nil {
		s, err = c.SendCommand("STATUS")
		if err != nil {
			return nil
		}
	}

	res := map[string]string{}
	lines := strings.Split(s, "\n")
	for _, l := range lines {
		kv := strings.Split(l, "=")
		if len(kv) == 2 {
			res[kv[0]] = kv[1]
		}
	}
	log.Println("STATUS->", res)
	h := c.wpa.mux
	if h != nil {
		if "" != res["ssid"] {
			c.wpa.mux.Send("/wifi/constatus", nil, "ssid", res["ssid"], "status", fmt.Sprintf("%v", res))
			//Send(h, "event", nil, []byte("c:CON\nt:START\nssid:"+res["ssid"]))
		} else {
			c.wpa.mux.Send("/wifi/constatus", nil, "status", fmt.Sprintf("%v", res))
			//Send(h, "event", nil, []byte(fmt.Sprintf("c:CON\nt:STOP\nm:%v", res)))
		}
	}
	return res
}

func (c *WifiInterface) SendCommandBool(command string) error {
	resp, err := c.SendCommand(command)
	if err != nil {
		return err
	}
	if resp != "OK\n" {
		return errors.New(resp)
	}
	return nil
}

func (c *WifiInterface) SendCommandInt(command string) (int, error) {
	resp, err := c.SendCommand(command)
	if err != nil {
		return 0, err
	}
	i, err := strconv.Atoi(strings.TrimSpace(resp))
	if err != nil {
		return 0, err
	}
	return i, nil
}

func (c *WifiInterface) SendCommandP2P(command string) (string, error) {
	return c.sendCommand(command, true)
}

//
//PING
//STATUS: address, p2p_device_address
//RECONNECT,REASSOCIATE
// p2p_group_add
//OK
//<3>P2P-GROUP-STARTED p2p-wlp2s0-0 GO ssid="DIRECT-OF" freq=2437 passphrase="5BqiXAgj" go_dev_addr=cc:2f:71:c8:f3:99
//<3>P2P-SERV-DISC-REQ 2437 da:50:e6:91:5b:cb 0 22 02000117
// P2P_SERV_DISC_EXTERNAL 1 -> 0 means to reject disc req, 1 to respond with ...
//
func (c *WifiInterface) SendCommand(command string) (string, error) {
	return c.sendCommand(command, false)
}

func (c *WifiInterface) sendCommand(command string, p2p bool) (string, error) {
	c.cmdMutex.Lock()
	defer c.cmdMutex.Unlock()

	con := c.conn
	if p2p && c.connp2p != nil {
		con = c.connp2p
	}
	if con == nil {
		err := c.Redial()
		con = c.conn
		if err != nil || con == nil {
			return "", err
		}
	}
	c.currentCommandResponse = make(chan string, 5)
	t1 := time.Now()
	_, err := con.Write([]byte(command))
	if err != nil {
		c.conn = nil
		c.Redial()

		return "", err
	}

	a := time.After(5 * time.Second)
	select {
	case resp := <-c.currentCommandResponse:
		t2 := time.Now()
		if command != "SCAN_RESULTS" {
			log.Println("WPA_CMD: ", t2.Sub(t1), command, "->", strings.Trim(resp, "\n"))
		}
		return resp, nil
	case <-a:
		log.Println("WPA_TIMEOUT", command)
		return "", errors.New("Timeout")
	}
}
