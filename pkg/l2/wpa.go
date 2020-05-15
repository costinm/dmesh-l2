package l2

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	mesh "github.com/costinm/dmesh-l2/pkg/l2api"
	"github.com/costinm/wpgate/pkg/msgs"
)

// Implements the interface with wpa_supplicant on Linux devices, mirroring the Android protocol and features.
// Will send JSON messages over a UDS socket, and accept simple commands over UDS.
// Message structure is defined in the wifi package.

// Based on:
// WifiP2pServiceImpl.java
// https://android.googlesource.com/platform/frameworks/base/+/56a2301/wifi/java/android/net/wifi/p2p/nsd/WifiP2pDnsSdServiceResponse.java

// For P2P_FIND to work: iw list must show P2P-device. P2P-client and P2P-go are not sufficient

// Other caps:
// 								 * IBSS
//                 * managed
//                 * AP
//                 * P2P-client
//                 * P2P-GO
//                 * P2P-device
// AP-VLAN -
// WDS
// monitor
// mesh point

type WPA struct {
	// P2P and normal interface are separated.
	// Multiple wifi interfaces supported.
	// TODO: ESP32 or equivalent modems connected via serial or BLE, supporting
	// other frequencies.
	Interfaces map[string]*WifiInterface

	mux *msgs.Mux `json:"-"`
	l2  *L2
}

type WifiInterface struct {
	wpa *WPA
	// Connections to wpa_supplicant control socket
	conn    io.ReadWriteCloser
	connp2p io.ReadWriteCloser

	// Primary interface name.
	Interface string
	baseDir   string

	// All messages will be written to this Handler if set.

	lsockname string

	// Used internally to match commands with responses.
	currentCommandResponse chan string

	// Held while a command is active. Usually commands return immediately.
	cmdMutex sync.Mutex

	// P2PFind in progress.
	scanning bool

	// ID of the pending discovery
	pendingDisc string

	// Last scan results (raw). Includes known and DIRECT networks.
	LastScan *mesh.L2NetStatus
	ScanTime time.Time

	// Keep track of discovered P2P devices.
	P2PByMAC  map[string]*mesh.MeshDevice
	P2PBySSID map[string]*mesh.MeshDevice

	// interface of p2p group
	p2pGroupInterface string
	ssid              string
	psk               string
}

// baseDir defaults to /var/run/wpa_supplicant
//
func (l2 *L2) NewWPA(baseDir string, refresh int, ap string) (*WPA, error) {
	// TODO: list interfaces in /run/wpa
	if baseDir == "" {
		baseDir = "/var/run/wpa_supplicant/"
	}

	f, err := os.Open(baseDir)
	if err != nil {
		return nil, err
	}

	names, err := f.Readdirnames(0)
	if err != nil {
		return nil, err
	}

	res := &WPA{
		Interfaces: map[string]*WifiInterface{},
		mux:        l2.mux,
		l2:         l2,
	}

	for _, n := range names {
		if strings.HasPrefix(n, "p2p-") {
			continue
		}
		wpa, err := DialWPA(res, baseDir, n, refresh, ap)
		if err != nil {
			log.Println("Error opening WifiInterface", err)
			continue
		}
		res.Interfaces[n] = wpa
	}

	return res, nil
}

func (i *WifiInterface) Redial() error {
	i.cmdMutex.Lock()
	if i.conn != nil {
		i.conn.Close()
		i.conn = nil
	}
	if i.connp2p != nil {
		i.connp2p.Close()
		i.connp2p = nil
	}
	i.cmdMutex.Unlock()
	var err error
	ch := []io.ReadWriter{}
	i.connp2p, err = connectWPA(i.baseDir, "p2p-dev-"+i.Interface)
	if err != nil {
		log.Println("Failed to connect to p2p interface")
		i.connp2p = nil
	} else {
		ch = append(ch, i.connp2p)
	}
	i.conn, err = connectWPA(i.baseDir, i.Interface)
	if err != nil {
		return err
	}
	ch = append(ch, i.conn)
	for n, c := range ch {
		c := c
		if c == nil {
			continue
		}
		log.Println("Starting ", n, c)
		n := n
		err := i.startWPAStream(c)
		if err != nil {
			log.Println(err)
			return err
		}

		go i.handleWPAStream(c, n == 0)
	}

	return nil
}

func connectWPA(baseDir, intf string) (*net.UnixConn, error) {
	addr, err := net.ResolveUnixAddr("unixgram", baseDir+intf)
	if err != nil {
		return nil, err
	}

	lsockname := fmt.Sprintf("/tmp/wpa_ctrl_%d_%s", os.Getpid(), intf)

	laddr, err := net.ResolveUnixAddr("unixgram", lsockname)
	if err != nil {
		return nil, err
	}

	conn, err := net.DialUnix("unixgram", laddr, addr)
	if err != nil {
		return nil, err
	}

	log.Println("Local addr: ", conn.LocalAddr())
	return conn, nil
}

func (wpa *WifiInterface) handleWPAStream(c io.ReadWriter, p2pif bool) {
	buf := make([]byte, 4096)

	for {
		// single message
		bytesRead, err := c.Read(buf[4:])
		if err != nil {
			log.Println("WPA handleWPAStream error:", err)
			return
		} else {
			msg := buf[4 : bytesRead+4]
			if msg[0] == '<' {
				if bytes.Contains(msg, []byte("CTRL-EVENT-SCAN-STARTED")) {
					continue
				}
				wpa.onEvent("", msg, p2pif)
			} else {
				go wpa.onCommandResponse("", buf[4:bytesRead+4])
			}
		}
	}
}

// DialWPA connects to a real interface. Requires root or NET_ADMIN
func DialWPA(wpap *WPA, base, ifname string, refreshSeconds int, ap string) (*WifiInterface, error) {
	if len(ifname) == 0 {
		return nil, nil
	}

	//conn, err := connectWPA(base, ifname)
	//if err != nil {
	//	log.Println("Failed to connect to WifiInterface ", err)
	//	return nil, err
	//}
	//conn1, err := connectWPA(base, "p2p-dev-"+ifname)
	//if err != nil {
	//	log.Println("Failed to connect to WifiInterface p2p ", err)
	//	conn1 = conn
	//}

	wpa := &WifiInterface{
		wpa:                    wpap,
		baseDir:                base,
		Interface:              ifname,
		P2PByMAC:               map[string]*mesh.MeshDevice{},
		P2PBySSID:              map[string]*mesh.MeshDevice{},
		currentCommandResponse: make(chan string, 10),
	}

	err := wpa.Redial()
	if err != nil {
		log.Println("Failed to dial ", err)
	}

	if refreshSeconds > 0 {
		wpa.Status()      // initial status ?
		wpa.P2PDiscover() //
		go wpa.UpdateLoop(refreshSeconds)
	}

	if ap != "" {
		wpa.APStop()
		time.Sleep(500 * time.Millisecond)
		wpa.APStart()
	}

	return wpa, nil
}

// Messages on "wifi" topic, for this node.
//
// scan
// disc
// con ssid psk
// p2p
// wpa - low level wpa command, "i" and "c" params
//
func (c *WPA) HandleMessage(ctx context.Context, cmd string, meta map[string]string, data []byte) {
	log.Printf("WPA/MSG/HANDLE %s %v", cmd, meta)

	parts := strings.Split(cmd, "/")
	if len(parts) < 2 || parts[1] != "wifi" {
		return
	}

	// Not supported:
	// nan
	// adv (explicit advertisment control)
	//

	switch parts[2] {
	case "scan":
		for _, i := range c.Interfaces {
			i.Scan()
		}
	case "disc":
		for _, i := range c.Interfaces {
			i.P2PDiscover()
		}
	case "con":
		switch parts[3] {
		case "start":
		case "stop":
		case "peer":
			for _, i := range c.Interfaces {
				i.Connect(meta, parts[4], parts[5])
			}

		case "cancel":
		}

	case "p2p":
		apOn := "1" == meta["ap"]
		if apOn {
			for _, i := range c.Interfaces {
				i.APStart()
			}
		}
		apOff := "0" == meta["ap"]
		if apOff {
			for _, i := range c.Interfaces {
				i.APStop()
			}
		}
		disc := meta["disc"]
		if disc != "" {
			if "1" == disc {
				for _, i := range c.Interfaces {
					i.Scan()
					i.P2PDiscover()
				}
			}
		}
		con := meta["con"]
		if con != "" {
			if meta["mode"] == "" || meta["mode"] == "REFLECT" {
				for _, i := range c.Interfaces {
					i.Connect(meta, meta["s"], meta["p"])
				}
			}
		}

	case "wpa":
		i := meta["i"]
		q := meta["c"]
		n := meta["n"]
		for k, wpa := range c.Interfaces {
			if i != "" && i != k {
				continue
			}
			res, err := wpa.SendCommand(q)
			if err != nil {
				log.Println("Error ", err)
			} else {
				c.mux.SendMessage(msgs.NewMessage("/wifi/wpares", map[string]string{
					"c": q,
					"r": res,
					"i": i,
					"n": n,
				}))
			}
		}

	}
	return
}

func (c *WifiInterface) UpdateLoop(refresh int) {
	if refresh == 0 {
		return
	}
	time.Sleep(time.Second * time.Duration(refresh))
	for {
		c.P2PDiscover()
		time.Sleep(time.Second * time.Duration(refresh))
	}
}

func (c *WifiInterface) sendScanResults() {
	res, _ := c.SendCommand("SCAN_RESULTS")
	lines := strings.Split(res, "\n")
	if len(lines) < 2 {
		log.Println("Scan results none: ", lines)
		return
	}
	s := &mesh.L2NetStatus{}

	for i := 1; i < len(lines); i++ {
		parts := strings.Split(lines[i], "\t")
		if len(parts) < 4 {
			continue
		}
		ssid := parts[4]
		if strings.HasPrefix(ssid, "DIRECT-") ||
			strings.HasPrefix(ssid, "DM-") ||
			ssid == "costin" {
			//log.Println("SCAN", ssid, parts)
		} else {
			continue
		}
		if len(ssid) == 0 {
			continue
		}

		var sc *mesh.MeshDevice

		if p2p, f := c.P2PBySSID[ssid]; !f {
			sc = &mesh.MeshDevice{}
		} else {
			sc = p2p
		}

		sc.SSID = ssid
		sc.BSSID = parts[0]
		sc.Freq, _ = strconv.Atoi(parts[1])
		sc.Level, _ = strconv.Atoi(parts[2])
		sc.Cap = parts[3]

		s.Scan = append(s.Scan, sc)

	}

	c.LastScan = s
	c.ScanTime = time.Now()

	log.Println("Scan results: ", len(lines), len(s.Scan))

	c.wpa.mux.SendMessage(msgs.NewMessage("/net/status", nil).SetDataJSON(c.LastScan))
}

// onCommandResponse is called to process messages from the WifiInterface daemon or equivalent (android proxy)
func (c *WifiInterface) onCommandResponse(cmd string, data []byte) {
	msg := string(data)
	select {
	case c.currentCommandResponse <- msg:
	default:
		log.Println("Response no command", msg)
	}
}
