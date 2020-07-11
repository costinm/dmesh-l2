package udp

import (
	"encoding/binary"
	"fmt"
	"log"
	"net"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/ipv4"
)

type DMClient struct {
	c *net.UDPConn

	m *net.UDPConn

	closedCh chan struct{}

	closeLock sync.Mutex
}

type Nat struct {
	c *net.UDPConn

	privAddr net.IP
	privPort uint16

	via *net.UDPAddr
	// timestamp
}


var (
	iface = "br0"

	DM = &DMClient{}

	udpNat = map[uint64]*Nat{}
)

// NewUdpServer starts a dmesh udp server.
func NewUdpServer() (*DMClient, error) {

	d := DMClient{}

	return &d, nil
}


func (d *DMClient) Listen() error {
	c, err := net.ListenUDP("udp", &net.UDPAddr{Port: 5228})
	if err != nil {
		return err
	}
	d.c = c
	d.closedCh = make(chan struct{})

	// TODO: find ipv6 local address, listen on multicast
	ifaces, err := net.Interfaces()
	// handle err
	for _, i := range ifaces {
		addrs, err := i.Addrs()
		if len(iface) > 0 && i.Name != iface {
			continue
		}
		if err != nil {
			log.Println("Error getting local address", err)
		}
		// handle err
		for _, addr := range addrs {
			if ipnet, ok := addr.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
				if ipnet.IP.To4() == nil && ipnet.IP.IsLinkLocalUnicast() {
					b := []byte(ipnet.IP.To16())
					b[0] = 0xFF
					b[1] = 2
					ip := net.IP(b)

					fmt.Println(ip)

					m, err := net.ListenMulticastUDP("udp6", nil,
						&net.UDPAddr{
							IP:   ip,
							Port: 5229,
						})
					if err != nil {
						log.Println("Failed to start multicast6", err)
					} else {
						d.m = m
						go d.readUdp(d.m, 5229)
					}
				}
			}
		}
	}

	go d.readUdp(c, 5228)

	return nil
}

func (d *DMClient) readUdp(c *net.UDPConn, port int) {
	defer c.Close()

	buf := make([]byte, 2048)
	for {
		n, addr, err := c.ReadFromUDP(buf)

		ip4, _ := ipv4.ParseHeader(buf[0:20])

		s := string(buf[0:n])
		s = strings.Replace(s, "\n", ", ", -1)

		log.Printf("U, %v, r:%v, p:%x, s:%v, d:%v, id:%v, l:%v\n", port, addr,
			ip4.Protocol, ip4.Src, ip4.Dst, ip4.ID, n)

		if err != nil {
			fmt.Println("Error: ", err)
		}

		if ip4.Protocol == 17 {
			port := binary.BigEndian.Uint16(buf[22:24])
			sport := binary.BigEndian.Uint16(buf[20:22])
			// TODO: decrypt, encrypt (unless the remote indicates e2e encryption, or
			// is DNS, etc). Most likely e2e will be handled by using port number.
			log.Println("Sending to ", port, " ", ip4.Dst)
			// src IP + port - 6 bytes
			key := uint64(binary.BigEndian.Uint32([]byte(ip4.Src))) + (uint64(sport) << 32)
			nat, f := udpNat[key]
			if !f {
				nat = &Nat{
					via: addr, // send back response to same DMesh GW
					privPort: sport,
					privAddr: ip4.Src,
				}
				nat.listenUdp()
				udpNat[key] = nat
				// TODO: remove old entries or timeout
			}
			nat.via =addr
			dst := ip4.Dst
			if dst.Equal(net.IPv4(8,8,8,8)) {
				//dst = net.IPv4(10,1,10,1)
			}
			nat.c.WriteToUDP(buf[28:n], &net.UDPAddr{IP:dst, Port:int(port)})
		}
		//d.SendAddr(addr, []byte("/ACK"))

		//processHTTP(addr, buf[0:n])
		/*
			select {
			case <-d.closedCh:
				log.Println("Done ")
				return
			}
		*/
	}
}


func (d *DMClient) Close() error {
	d.closeLock.Lock()
	defer d.closeLock.Unlock()

	d.c.Close()

	close(d.closedCh)

	return nil
}

func (d *DMClient) Send(addr string, data []byte) {
	d.c.WriteToUDP(data, &net.UDPAddr{IP: net.ParseIP(addr), Port: 5228})
}

func (d *DMClient) SendAddr(addr *net.UDPAddr, data []byte) {
	d.c.WriteToUDP(data, addr)
}

// PingMaster attempts to locate a master.
// Will send UDP packets - multicast and unicast to well-known addresses.
func (d *DMClient) PingMaster(data []byte) {

	//d.c.WriteToUDP(data, &net.UDPAddr{IP: net.ParseIP("10.1.10.2"), Port: 2009})

	d.c.WriteToUDP(data, mc6UDPAddr)

}

// Sends a packet to dmesh routers. This is an IP6 local net multicast, to ff02:5223 on
// port 5223. The content is an IP header, with protocol = 0xc0,
// destination set to 'all routers' multicast, 224.0.0.2.
// Src is the 3 bytes local mesh address.
// The payload is not ready - will include 65B public key, signature and some metadata.
// It may also include stats.
func (d *DMClient) Subscribe(addr string, data []byte) {
	h := ipv4.Header{
		Protocol: 0xc0,
		Dst: net.IPv4(224, 0, 0, 2),
		TTL: 1,
		Version:  ipv4.Version,
		Len:      ipv4.HeaderLen,
		TOS:      0xc0, // DSCP CS6
		TotalLen: ipv4.HeaderLen + len(data),
	}
	b, _ := h.Marshal()
	d.c.WriteToUDP(b, mc6UDPAddr)
}

func (n *Nat) listenUdp() error {
	c, err := net.ListenUDP("udp", &net.UDPAddr{Port: 0})
	if err != nil {
		return err
	}
	n.c = c
	go n.readNat()
	return nil
}

func (n *Nat) readNat() {
	defer n.c.Close()

	buf := make([]byte, 2048)
	for {
		cnt, raddr, _ := n.c.ReadFromUDP(buf[28:])
		h := ipv4.Header{
			Protocol: 0x11,
			Src: raddr.IP,
			Dst: n.privAddr,
			TTL: 8,
			Version:  ipv4.Version,
			Len:      ipv4.HeaderLen,
			TotalLen: ipv4.HeaderLen + 8 + cnt,
		}
		b, _ := h.Marshal()
		copy(buf[0:], b)
		// source port (from WAN)
		binary.BigEndian.PutUint16(buf[20:], uint16(raddr.Port))
		// Original port that opened the NAT
		binary.BigEndian.PutUint16(buf[22:], n.privPort)
		binary.BigEndian.PutUint16(buf[24:], uint16(cnt + 8)) // redundant with ip len
		buf[26] = 0
		buf[27] = 0
		// TODO: update CRC
		fmt.Println("O ", "len:", h.TotalLen, " via", n.via)
		DM.c.WriteToUDP(buf[0: 28 + cnt], n.via)
	}
}

var (
	mc6UDPAddr = &net.UDPAddr{
		IP:   net.ParseIP("FF02::5223"),
		Port: 5223,
	}
)

type DMNode struct {
	Addr     net.UDPAddr
	LastSeen time.Time
}

type DMMaster6 struct {
	m      *net.UDPConn
	closed bool

	clientLock  sync.Mutex
	clientsByIP map[string]DMNode
}

func (d *DMMaster6) Start() error {
	ifis, err := net.Interfaces()
	if err != nil {
		log.Println("failed to get intefaces", err)
		return err
	}
	for _, ifi := range ifis {
		if ifi.Flags&net.FlagUp == 0 || ifi.Flags&net.FlagMulticast == 0 {
			continue
		}
		if len(iface) > 0 && ifi.Name != iface {
			continue
		}
		m, err := net.ListenMulticastUDP("udp6", &ifi, mc6UDPAddr)
		if err != nil {
			log.Println("Failed to start multicast6", err)
		} else {
			d.m = m
			d.closed = false
			fmt.Println("Listenting on ", mc6UDPAddr, ifi)
			go d.readUdp(d.m, ifi.Name)
		}
	}
	return nil
}

func (d *DMMaster6) readUdp(c *net.UDPConn, name string) {
	defer c.Close()

	buf := make([]byte, 2048) // 512 should be sufficient (UDP max size)
	for {
		n, addr, err := c.ReadFromUDP(buf)
		if err != nil {
			log.Println("dmesh receive error: ", err)
		}
		if d.closed {
			return
		}
		ip4, _ := ipv4.ParseHeader(buf[0:20])

		log.Printf("M %s %v p:%x s:%v d:%v id:%v l:%v\n", name, addr, ip4.Protocol, ip4.Src, ip4.Dst,
			ip4.ID, n)
	}
}

func ips() []net.IP {
	ips := []net.IP{}

	ifaces, err := net.Interfaces()
	if err != nil {
		return ips
	}
	for _, i := range ifaces {
		addrs, err := i.Addrs()
		if err == nil {
			for _, addr := range addrs {
				var ip net.IP
				switch v := addr.(type) {
				case *net.IPNet:
					ip = v.IP
				case *net.IPAddr:
					ip = v.IP
				}
				ips = append(ips, ip)
				// process IP address
			}
		}
	}
	return ips
}
