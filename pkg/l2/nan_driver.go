package l2

import (
	"context"
	"log"
	"strconv"
	"time"

	"github.com/costinm/dmesh-l2/pkg/l2/wifi"
	"github.com/jsimonetti/rtnetlink/rtnl"
)

/*
	https://mdlayher.com/blog/linux-netlink-and-go-part-1-netlink/

 - socket AF_NETLINK, SOCK_RAW or DGRAM, family
 - sendto( SockaddrNetlink),
 - recvfrom
 - multi-part messages
 - multicast - SetSockopt joinLeave, group

 Attributes: LTV, 16bit l and tag

  Generic:
  - 8bit cmd and version
  - "nlctl" - list of netlinks
  - TASKSTATS
  - nl80211
  - acpievents
  - tcp_metrics
  - devlink

  Tools: nlmon

    genl ctrl list


  - groups: nan, mlme, scan, config


  github.com/aporeto-inc/conntrack
	github.com/mdlayher/kobject - USB insertion, etc

	github.com/awilliams/homenet/pkg/wifi - for AP (larger dep)


	Reference for cmd_frame: https://w1.fi/cgit/hostap/tree/src/drivers/driver_nl80211.c


modprobe nlmon

ip link add type nlmon
ip link set nlmon0 up


NLDBG=2
iw dev msta mgmt dump frame D0 04:09:50:6f:9A:13


tshark -i wlan0 -I -J IEE802_11_RADIO -n -s0 -f "wlan type mgt subtype 0xd0 and wlan[16:2] - 0x506F" -P

*/

// Problems with mdlayher netlink package:
// - Execute assumes all received packets are for itself.
// - Receive assumes sequence of messages for same request.

// vishvananda: focused on routing, supports ns, checks seq and pid !
// fork of docker/libcontainer, which now uses it (opencontainers/runc/libcontainer)
//

var (
	w *wifi.Client
)

// Make sure each Phy device has a mon interface.
// Open for read each mon interface.
func (l2 *L2) setupMonInterfaces(client *wifi.Client) error {
	// Find phy - we'll use a mon for each
	phys, err := client.Phys()
	if err != nil {
		return err
	}
	log.Println(phys)

	phyMap := map[int]*wifi.Phy{}
	for _, p := range phys {
		phyMap[p.PHY] = p
	}

	ifis, err := client.Interfaces()
	if err != nil {
		return err
	}

	l2.actWifi = []*wifi.Interface{}

	physMon := map[int]*wifi.Interface{}
	mons := 0

	for _, ifi := range ifis {
		if ifi.Type == wifi.InterfaceTypeMonitor {
			physMon[ifi.PHY] = ifi
		} else {
			l2.actWifi = append(l2.actWifi, ifi)
		}
	}
	for id, p := range phyMap {
		if physMon[id] == nil {
			err = client.NewMon(p.PHY)
			if err != nil {
				log.Println("Failed to create mon ", p, err)
			} else {
				mons++
			}
		}
	}
	rtcon, err := rtnl.Dial(nil)
	if err != nil {
		return err
	}
	ifl, err := rtcon.Links()
	if err != nil {
		return err
	}
	for _, l := range ifl {
		if l.Name == "dmeshmon" {
			rtcon.LinkUp(l)
		}
	}
	log.Println(ifl)

	if mons > 0 {
		ifis, err = client.Interfaces()
		if err != nil {
			return err
		}
		for _, ifi := range ifis {
			if ifi.Type == wifi.InterfaceTypeMonitor {
				physMon[ifi.PHY] = ifi
			}
		}
	}

	for _, ifi := range physMon {
		go func() {
			log.Println("Initialized mon ", ifi.Name)
			err := l2.InitMon(ifi)
			if err != nil {
				log.Println("MON ERR: ", err)
			}
		}()
	}
	l2.physMon = physMon

	return nil
}

// Low level Wifi, using monitor interfaces and netlink.
// Supports a subset of WifiAware, as well as extensions to handle
// the lack of low-level support.
func (l2 *L2) InitWifi() error {
	client, err := wifi.New()
	if err != nil {
		log.Println("Error initializing wifi ", err)
		return err
	}
	w = client

	err = l2.setupMonInterfaces(client)
	if err != nil {
		log.Println("Error initializing wifi ", err)
		return err
	}
	cnt := 0

	for _, ifi := range l2.actWifi {
		nanc := wifi.NewNan(client, ifi)
		// For more information about what a "BSS" is, see:
		// https://en.wikipedia.org/wiki/Service_set_(802.11_network).
		//bss, err := client.BSS(ifi)
		//if err != nil {
		//	log.Println("BSS err", ifi,  err)
		//} else {
		//	//active = ifi
		//	log.Println("BSS: ", bss)
		//}
		//fmt.Printf("%s: %q\n", ifi.Name, bss, ifi)
		log.Println(ifi.Name, ifi.Type, ifi.Device, ifi.Frequency,
			ifi.HardwareAddr, ifi.PHY, ifi)

		if ifi.Type != wifi.InterfaceTypeMonitor && ifi.Name != "" {
			// Works only if started before WPA, and only if dst address is my addr.
			// No way to register the SSID
			a := ifi
			// TODO: we can use only the monitor interface...
			// This is mainly to test if we can skip the monitor for active frames - and
			// enable it only for beacon/discovery/data frames.
			// Sometimes it doesn't work well with wpa_supplicant
			client.RegisterFrame(a, 0xd0, []byte{0x04, 0x09, 0x50, 0x6f, 0x9A, 0x13})

			// invalid arg when wpa
			//client.RegisterFrame(ifi, 0xd0, []byte{0x04, 0x09})
			//client.RegisterFrame(a, 0xd0, []byte{0x04})
		}

		//a := ifi
		// mon0 - not supported ( kernel checks interface, p2p requires freq)
		// Encryption can't be disabled with SEND_FRAME. See hostapd driver
		//

		// ifi.Name == "wlp2s0"
		// It seems to work with p2p go interface.
		// Also with the non-nl p2p device interface

		if true { // ifi.Type != wifi.InterfaceTypeMonitor {// ifi.Name == "wlx4494fce48415" || ifi.Name == "wlp2s0" {
			go func() {
				go ScheduleBeacon(nanc)

				if false {
					for {
						//client.RemainOnChannel(a, 2437, 1000);
						time.Sleep(2000 * time.Millisecond)
						err := nanc.SendFollowup([]byte{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF},
							0x80,
							2437,
							[]byte("PING"+ifi.Name+"-"+strconv.Itoa(cnt)))
						cnt++
						if err != nil {
							log.Println("XXXXXXXXXXXXXXXXX Error sending frame", err)
							//return
						}
						//log.Println("Send another frame")
					}
				}
			}()
		}

		// No such device - for P2P device

		//if ifi.Type == wifi.InterfaceTypeP2PDevice { //"wlp2s0" { // "msta" {
		//	active = a
		//}

		//BSS:  &{costin 70:3a:cb:02:2b:36 5745 102.4ms 3h47m25.716s associated}
		// 2020/02/13 22:32:51 wlp2s0 station 1 5745 38:ba:f8:49:d3:bf 0 &{2 wlp2s0 38:ba:f8:49:d3:bf 0 1 station 5745}
		//si, err := client.StationInfo(ifi)
		//if err != nil {
		//	log.Println("SI err", err)
		//} else {
		//	for sii := range si {
		//		log.Println("Station Info: ", sii)
		//	}
		//}
	}

	go client.StartReceive()

	return nil
}

func ScheduleBeacon(nanc *wifi.Nan) {
	tick := time.NewTicker(512 * 1024 * time.Microsecond)
	for {
		select {
		case _ = <-tick.C:
			nanc.SendBeacon(true)
		}
	}
}

// Schedule calls function `f` with a period `p` offsetted by `o`.
// Aligned with the duration (fixed)
func Schedule(ctx context.Context, p time.Duration,
	o time.Duration, f func(time.Time)) {
	// Position the first execution
	first := time.Now().Truncate(p).Add(o)
	if first.Before(time.Now()) {
		first = first.Add(p)
	}
	firstC := time.After(first.Sub(time.Now()))

	// Receiving from a nil channel blocks forever
	t := &time.Ticker{C: nil}

	for {
		select {
		case v := <-firstC:
			// The ticker has to be started before f as it can take some time to finish
			t = time.NewTicker(p)
			f(v)
		case v := <-t.C:
			f(v)
		case <-ctx.Done():
			t.Stop()
			return
		}
	}

}

//nlh, err := vnetlink.NewHandle()
//if err != nil {
//	return nil, err
//}
//fn, err := nlh.GenlFamilyGet(nl80211.GenlName)
//if err != nil {
//	return nil, err
//}
//
//gid := []uint{}
//for _,g := range fn.Groups {
//	gid = append(gid, uint(g.ID))
//}
//vns, err := vnl.Subscribe(16) //int(fn.ID), gid...)
//if err != nil {
//	log.Println("Subscribe failed ",gid)
//	//return nil, err
//} else {
//	log.Println("VNETLINK GOT ", fn, vns)
//	go func() {
//		for {
//			msgs, from, err := vns.Receive()
//			log.Println("vnlReceived: ", msgs, from, err)
//		}
//	}()
//
//
//	// This seems to be the main method for sending with the package
//
//	// Notice the use of the standard go unix pacakge !!
//	//vnetlink.
//	//vns.Send(&vnl.NetlinkRequest{
//	//	NlMsghdr: unix.NlMsghdr{
//	//		Type: fn.ID,
//	//		Flags: unix.NLM_F_REQUEST,
//	//	},
//	//	Sockets: nlh
//	//})
//}
