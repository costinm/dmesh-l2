package l2

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/costinm/wpgate/pkg/msgs"

	"github.com/go-ble/ble"
	"github.com/go-ble/ble/linux"
)

// BLE provides L2 connectivity with other nodes supporting BLE.
// This uses an extension of EddyStone beacon - advertising the DMesh ID.
// The extension consists on using the proxy characteristics (from BLE Mesh service)
// to also allow packet exchange.
//
// Has a matching Android implementation as well as ESP32.
// When running on Android, the BLE is implemented by the Android process using the core libraries -
// this is used on linux hosts.
type BLE struct {
	device *linux.Device
	nodes  map[string]*BLENode
	mutex  sync.Mutex
	l2     *L2
	mux    *msgs.Mux
}

// Tracks a BLE peer.
type BLENode struct {
	msgs.MsgConnection

	// ble interface
	ble *BLE

	// Received advertisment
	adv ble.Advertisement

	Addr ble.Addr
	Name string

	Last time.Time
	con  ble.Client
	tx   *ble.Characteristic
}

func (b *BLENode) String() string {
	return fmt.Sprintf("BLEN: %v %v", b.Addr, b.con)
}

var (
	EDDYSTONE16    = []byte{0xAA, 0xFE}
	CH_PROXY_WRITE = []byte{0xDD, 0x2A}
	CH_PROXY_NOTIF = []byte{0xDE, 0x2A}
)

var (
	connectError     = errors.New("Connect error")
	AlreadyConnected = errors.New("Already connected")

	useDiscoverProfile = false
)

// Connect to a discovered node.
// Connect won't work if scan is in progress
func (b *BLE) maybeConnect(n *BLENode) error {
	// sd[0].Data - info about the device

	if n.con != nil {
		return AlreadyConnected
	}

	tc := time.Now()
	cl, err := ble.Dial(context.Background(), n.Addr)
	if err != nil {
		return err
	}

	n.con = cl

	rx := n.con.Conn().RxMTU()
	tx := n.con.Conn().TxMTU()
	mtu, err := n.con.ExchangeMTU(2048)
	if err != nil {
		mtu, err = n.con.ExchangeMTU(1024)
	}
	// Works !
	if err != nil {
		mtu, err = n.con.ExchangeMTU(512)
	}
	if err != nil {
		log.Println("MTU error ", err)
		mtu, err = n.con.ExchangeMTU(32)
	}
	log.Println("BLE: Connected", n.Name, n.Addr, "MTU", mtu, rx, tx,
		n.con.Conn().RxMTU(), "RSSI", n.con.ReadRSSI())

	var p *ble.Profile
	t0 := time.Now()
	if useDiscoverProfile {
		// 156ms
		p, err = n.con.DiscoverProfile(true)
		if err != nil {
			log.Println("Disc services error ", err)
			return err
		}
		log.Println("Profile", time.Since(t0), p)
	}

	t1 := time.Now()
	svc, err := n.con.DiscoverServices([]ble.UUID{EDDYSTONE16})
	if useDiscoverProfile {
		svc = p.Services
	}

	for _, s := range svc { // svc
		if s.UUID.Equal(EDDYSTONE16) {
			if !useDiscoverProfile {
				chr, _ := n.con.DiscoverCharacteristics(nil, s)
				s.Characteristics = chr
			}
			for _, c := range s.Characteristics {
				//	for _, c := range chr {
				if c.Property == ble.CharWriteNR { // c.UUID.Equal(CH_PROXY_WRITE) {
					n.tx = c
				}
				if c.Property == ble.CharNotify { // c.UUID.Equal(CH_PROXY_NOTIF) {
					if !useDiscoverProfile {
						// 80..119 ms - saving 35ms (fewer RT).
						// Required to the the 'CCCD' - char descriptor for subscribing
						n.con.DiscoverDescriptors(nil, c)
						log.Println("Found DMesh device ", time.Since(tc), time.Since(t1), svc, c, s) // 59 ms vs 156 for DiscoveryProfile
					}
					err = n.con.Subscribe(c, false, func(req []byte) {
						log.Println("BLE IN: ", len(req), string(req))
						n.ble.mux.SendMessage(msgs.NewMessage("/raw",
							map[string]string{
								"from": n.Addr.String(),
							}).SetDataJSON(req))
					})
					if err != nil {
						log.Println("BLE SUB", err)
					}

				}
			}
		}
	}

	n.MsgConnection.SubscriptionsToSend = []string{"lora", "ble"}

	n.SendMessageToRemote = func(ev *msgs.Message) error {
		if n.tx != nil {
			err := n.con.WriteCharacteristic(n.tx, ev.Binary(), true)
			if err != nil {
				log.Println("BLE Write", err)
				return err
			}
		}
		return nil
	}
	n.ble.mux.AddConnection(n.Addr.String(), &n.MsgConnection)

	go n.handleCon()

	return nil
}

// Connection to a BLE peer (android, ESP).
// Will forward all received packets to L3 using messages.
// Will register for "ble" topic, used by L3 to send messages to the device.
// This is also used with ESP32+Lora to send messages to the lora driver.
func (n *BLENode) handleCon() {
	t0 := time.Now()
	defer func() {
		n.ble.mux.Gate.RemoveConnection(n.Addr.String(), &n.MsgConnection)
		n.con = nil
		log.Println("BLE close connection ", n.Addr.String(), time.Since(t0))
	}()

	ch := n.con.Disconnected()
	select {
	case <-ch:
		return
	}
}

// Requires capabilities on the binary or root
// Uses a AF_BLUETOOTH socket and HCI.
//
// Creates a 'serial' connection over BLE.
func (l2 *L2) InitBLE() (*BLE, error) {
	d, err := linux.NewDevice(ble.OptDialerTimeout(2 * time.Second))
	if err != nil {
		return nil, err
	}
	b := &BLE{
		device: d,
		mux:    l2.mux,
	}
	b.nodes = map[string]*BLENode{}

	ble.SetDefaultDevice(d)

	return b, nil
}

func (b *BLE) CleanOlder(d time.Duration) {
	old := []*BLENode{}
	for _, v := range b.nodes {
		if time.Since(v.Last) > d {
			old = append(old, v)
			continue
		}
	}
	for _, v := range old {
		delete(b.nodes, v.Addr.String())
	}
}

// Discover BLE devices of the right type.
// If found, attempt to connect to create mesh links.
// TODO: select strongest signal, limit the numbers.
func (b *BLE) Scan(d time.Duration) error {

	// Scan time should account for sleepy devices.
	// The API uses ctx.Done to StopScanning.
	// Stop scanning is required to allow radio to switch to other freq.
	// TODO: passive scan vs active ?
	// TODO: find if BLE scan and Wifi scan can happen at the same time.

	fnd := 0
	c, _ := context.WithTimeout(context.Background(), d)
	err := ble.Scan(c, false, /*dup*/
		func(a ble.Advertisement) {
			b.mutex.Lock()
			fnd++
			// TODO: cleanup old
			// TODO: send status message if new device nearby found
			n, f := b.nodes[a.Addr().String()]
			if !f {
				n = &BLENode{
					ble:  b,
					adv:  a,
					Addr: a.Addr(),
				}
				b.nodes[a.Addr().String()] = n

				// local name not used in the beacon

			} else {
				n.adv = a
			}
			b.mutex.Unlock()
			n.Last = time.Now()
		}, func(a ble.Advertisement) bool {
			svcs := a.Services()
			if len(svcs) != 1 || !svcs[0].Equal(EDDYSTONE16) {
				return false
			}

			if !a.Connectable() {
				return false
			}
			sd := a.ServiceData()
			if len(sd) != 1 {
				return false
			}
			return true
		})

	if err != nil && err != context.DeadlineExceeded {
		// Typically means no HCI support
		log.Println("BLE scan err", err)
		return err
	}

	log.Println("BLE Scan done", fnd)

	return nil
}
