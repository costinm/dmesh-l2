package netstacktun

import (
	"errors"
	"io"
	"log"
	"net"
	"strings"
	"time"

	"github.com/google/netstack/tcpip"
	"github.com/google/netstack/tcpip/adapters/gonet"
	"github.com/google/netstack/tcpip/buffer"
	"github.com/google/netstack/tcpip/link/loopback"
	"github.com/google/netstack/tcpip/link/sniffer"
	"github.com/google/netstack/tcpip/network/arp"
	"github.com/google/netstack/tcpip/network/ipv4"
	"github.com/google/netstack/tcpip/network/ipv6"
	"github.com/google/netstack/tcpip/stack"
	"github.com/google/netstack/tcpip/transport/tcp"
	"github.com/google/netstack/tcpip/transport/udp"
	"github.com/google/netstack/waiter"

	"github.com/songgao/water"
)

// Intercept using a TUN and google netstack to parse TCP/UDP into streams.
// The connections are redirected to a capture.ProxyHandler
type NetstackTun struct {
	// The IP stack serving the tun. It intercepts all TCP connections.
	IPStack *stack.Stack

	DefUDP tcpip.Endpoint
	DefTCP tcpip.Endpoint

	Handler Gateway

	udpPacketConn net.PacketConn
}

type StreamProxy interface {
	Dial(dest string, addr *net.TCPAddr) error
	Proxy() error
	Close() error
}

// Interface implemented by Gateway.
type Gateway interface {
	HandleUdp(dstAddr net.IP, dstPort uint16, localAddr net.IP, localPort uint16, data []byte)
	NewStream(addr net.IP, port uint16, ctype string, initialData []byte, clientIn io.ReadCloser, clientOut io.Writer) interface{}
}

/*

 Client:
	- tun app has access to real network - can send/receive to any host directly,
  -- may have real routable IPv4 and/or IPV6 address
  -- may be inside a mesh - only IPv6 link local communication with other nodes
  - regular apps have the default route set to the TUN device (directly or via rule).
	- tun_capture read all packets from regular apps, terminates TCP and receives UDP
  - the TCP can forward to real destination, or tunnel to some other node.
  - or it can tunnel all connections to it's VPN server, using a QUIC forwarder at TCP
   level.


 Server:
  - server operates on L7 streams only - originates TCP and UDP as client
  - client requests are muxed over h2 (or QUIC)
  - no TUN required, no masq !
  - skips the tunneled IP and TCP headers - metadata sent at start of stream
  - only the external IP/UDP/QUIC headers.

 Both:
  - each node can act as a server - forwarding streams either upstream or to nodes in
    same mesh
  - when acting as client, it can operate without TUN - forwarding TCP streams or UDP
   at L7.
  - tun capture requires VPN to be enabled, and transparently captures all TCP

 Alternatives:
  - tun_client captures all ip frames and sends them to VPN server
  - tun_server receives ip frames from clients, injects in local tun which does ipmasq
  - tun_server can also route to other clients directly, based on ip6

*/

/*
 Example android:
10: tun0: <POINTOPOINT,UP,LOWER_UP> mtu 1400 qdisc pfifo_fast state UNKNOWN qlen 500
    link/none
    inet 10.10.154.232/24 scope global tun0
    inet6 2001:470:1f04:429:4a46:48e5:ae34:9ae8/64 scope global
       valid_lft forever preferred_lft forever

ip route list table all

default via 10.1.10.1 dev wlan0  table wlan0  proto static
10.1.10.0/24 dev wlan0  table wlan0  proto static  scope link
default dev tun0  table tun0  proto static  scope link
10.1.10.0/24 dev wlan0  proto kernel  scope link  src 10.1.10.124
10.10.154.0/24 dev tun0  proto kernel  scope link  src 10.10.154.232
2001:470:1f04:429::/64 dev tun0  table tun0  proto kernel  metric 256
fe80::/64 dev tun0  table tun0  proto kernel  metric 256

ip rule show
0:	from all lookup local
10000:	from all fwmark 0xc0000/0xd0000 lookup legacy_system

11000:	from all iif tun0 lookup local_network

12000:	from all fwmark 0xc0066/0xcffff lookup tun0

### EXCLUDED: VPN process
12000:	from all fwmark 0x0/0x20000 uidrange 0-10115 lookup tun0
12000:	from all fwmark 0x0/0x20000 uidrange 10117-99999 lookup tun0

13000:	from all fwmark 0x10063/0x1ffff lookup local_network
13000:	from all fwmark 0x10064/0x1ffff lookup wlan0

13000:	from all fwmark 0x10066/0x1ffff uidrange 0-0 lookup tun0

13000:	from all fwmark 0x10066/0x1ffff uidrange 0-10115 lookup tun0
13000:	from all fwmark 0x10066/0x1ffff uidrange 10117-99999 lookup tun0

14000:	from all oif wlan0 lookup wlan0

14000:	from all oif tun0 uidrange 0-10115 lookup tun0
14000:	from all oif tun0 uidrange 10117-99999 lookup tun0

15000:	from all fwmark 0x0/0x10000 lookup legacy_system
16000:	from all fwmark 0x0/0x10000 lookup legacy_network
17000:	from all fwmark 0x0/0x10000 lookup local_network
19000:	from all fwmark 0x64/0x1ffff lookup wlan0
21000:	from all fwmark 0x66/0x1ffff lookup wlan0
22000:	from all fwmark 0x0/0xffff lookup wlan0
23000:	from all fwmark 0x0/0xffff uidrange 0-0 lookup main

32000:	from all unreachable

*/

/*
google.transport:
- transport_demuxer.go endpoints has a table of ports to endpoints

Life of packet:
-> NIC.DeliverNetworkPacket - will make a route - remote address/link addr, nexthop, netproto
-> ipv6.HandlePacket
-> NIC.DeliverTransportPacket
-- will first attempt nic.demux, then n.stac.demux deliverPacket
-- will look for an endpoint
-- packet added to the rcv linked list
-- waiter.dispatchToChannelHandlers()

RegisterTransportEndpoint -> with the stack transport dispatcher (nic.demux), on NICID
-- takes protocol, id - registers endpoint
-- for each net+transport protocol pair, one map based on 'id'
-- id== local port, remote port, local address, remote address
--


- a NIC is created with ID(int32), [name] and 'link endpoint ID' - which is a uint64 in the 'link endpoints'
static table. The LinkEndpoint if has MTU, caps, LinkAddress(MAC), WritePacket and Attach(NetworkDispatcher)
The NetworkDispatcher.DeliverNetworkPacket is also implemented by NIC

*/

// If NET_CAP or owner, open the tun.
func OpenTun(ifn string) (io.ReadWriteCloser, error) {
	config := water.Config{
		DeviceType: water.TUN,
		PlatformSpecificParams: water.PlatformSpecificParams{
			Persist: true,
		},
	}
	config.Name = ifn
	ifce, err := water.New(config)

	if err != nil {
		return nil, err
	}
	return ifce.ReadWriteCloser, nil
}

// NewTunCapture creates an in-process tcp stack, backed by an tun-like network interface.
// All TCP streams initiated on the tun or localhost will be captured.
func NewTunCapture(ep *tcpip.LinkEndpointID, handler Gateway, snif bool) *NetstackTun {
	t := &NetstackTun{}

	t.Handler = handler

	t.IPStack = stack.New([]string{ipv4.ProtocolName, ipv6.ProtocolName, arp.ProtocolName},
		[]string{tcp.ProtocolName, udp.ProtocolName}, stack.Options{})

	loopbackLinkID := loopback.New()
	if snif {
		loopbackLinkID = sniffer.New(loopbackLinkID)
	}
	t.IPStack.CreateNIC(1, loopbackLinkID)

	addr1 := "\x7f\x00\x00\x01"
	if err := t.IPStack.AddAddress(1, ipv4.ProtocolNumber, tcpip.Address(addr1)); err != nil {
		log.Print("Can't add address", err)
		return t
	}

	ep1 := *ep
	if snif {
		ep1 = sniffer.New(ep1)
	}

	// NIC 2 - IP4

	t.IPStack.CreateNIC(2, ep1)

	addr2 := "\x0a\x0c\x00\x02"
	if err := t.IPStack.AddAddress(2, ipv4.ProtocolNumber, tcpip.Address(addr2)); err != nil {
		log.Print("Can't add address", err)
		return t
	}
	addr3 := net.IPv6loopback
	if err := t.IPStack.AddAddress(2, ipv6.ProtocolNumber, tcpip.Address(addr3)); err != nil {
		log.Print("Can't add address", err)
		return t
	}
	t.IPStack.SetPromiscuousMode(2, true)
	t.IPStack.SetSpoofing(2, true)

	sn, _ := tcpip.NewSubnet(tcpip.Address("\x00"), tcpip.AddressMask("\x00"))
	t.IPStack.AddSubnet(2, ipv4.ProtocolNumber, sn)

	sn, _ = tcpip.NewSubnet(tcpip.Address("\x00"), tcpip.AddressMask("\x00"))
	t.IPStack.AddSubnet(2, ipv6.ProtocolNumber, sn)

	setRouteTable(t.IPStack, ep != nil)

	//epp := newEpProxy()
	DefTcpServer(t, handler) //echo)

	DefTcp6Server(t, handler) //echo)

	t.defUdpServer()
	t.defUdp6Server()

	// Bound to 10.22.0.5, which is routed to dmesh1
	//addrN := tcpip.FullAddress{2, tcpip.Address(net.IPv4(10, 55, 0, 5).To4()), 5228}

	//c1, err := gonet.NewPacketConn(t.IPStack, addrN, ipv4.ProtocolNumber)
	//if err != nil {
	//	log.Println("XXXXXX ", err)
	//}
	//t.udpPacketConn = c1

	//go t.udpPing(2, t.IPStack)

	return t
}

// Debugging reception - send a packet every 5 seconds to port 1999.
// DST IP is the current eth IP
func (nt *NetstackTun) udpPing(NICID tcpip.NICID, stack *stack.Stack) {
	//addr1 := tcpip.FullAddress{NICID, tcpip.Address(net.IPv4(10, 12, 0, 5).To4()), 5228}

	// Works:

	// Doesn't seem to work
	//addr1 := tcpip.FullAddress{NICID, tcpip.Address(net.IPv4(73, 158, 64, 16).To4()), 5228}

	for {
		time.Sleep(15 * time.Second)
		//c1.WriteTo([]byte("Hi1"), &net.UDPAddr{Port: 1999, IP: net.IPv4(10, 12, 0, 5)})
		//nt.udpPacketConn.WriteTo([]byte("Hi2  1234"), &net.UDPAddr{Port: 1999, IP: net.IPv4(10, 10, 201, 200)})
		ip9 := net.ParseIP("2001:470:1f04:428::9")
		nt.udpPacketConn.WriteTo([]byte("Hi2  1234"), &net.UDPAddr{Port: 1999, IP: ip9})
	}
}

func (nt *NetstackTun) WriteTo(data []byte, dst *net.UDPAddr, src *net.UDPAddr) (int, error) {
	addrb := []byte(dst.IP)
	srcaddrb := []byte(src.IP.To4())
	// TODO: how about from ?
	// TODO: do we need to make a copy ? netstack passes ownership, we may reuse buffers
	n, _, err := nt.DefUDP.Write(tcpip.SlicePayload(data), tcpip.WriteOptions{
		To: &tcpip.FullAddress{
			Port: uint16(dst.Port),
			Addr: tcpip.Address(addrb),
		},
		From: &tcpip.FullAddress{
			Port: uint16(src.Port),
			Addr: tcpip.Address(srcaddrb),
		},
	})
	if err != nil {
		return 0, errors.New(err.String())
	}
	return int(n), nil
}

type tcpHandler func(wq *waiter.Queue, ep tcpip.Endpoint)

type UdpLocalReader interface {
	ReadLocal(addr *tcpip.DoubleAddress) (buffer.View, tcpip.ControlMessages, *tcpip.Error)
}

func (nt *NetstackTun) defUdpServer() error {
	// Like a socket
	var wq waiter.Queue
	ep, err := nt.IPStack.NewEndpoint(udp.ProtocolNumber, ipv4.ProtocolNumber, &wq)
	if err != nil {
		return errors.New(err.String())
	}
	nt.DefUDP = ep

	// No address - listen on all
	err = ep.Bind(tcpip.FullAddress{
		//Addr: "\x01", - error
		//Addr: "\x00\x00\x00\x00",
		//Port: 2000,
		Port: 0xffff,
	}, nil)
	if err != nil {
		ep.Close()
		return errors.New(err.String())
	}

	nt.IPStack.Capture(ipv4.ProtocolNumber, udp.ProtocolNumber, ep.(stack.TransportEndpoint))
	we, ch := waiter.NewChannelEntry(nil)
	wq.EventRegister(&we, waiter.EventIn)
	//defer wq.EventUnregister(&we)

	// receive UDP packets on port
	go func() {
		for {
			// Will have the peer address
			add := tcpip.DoubleAddress{}
			//ep.SetSockOpt()
			v, _, err := ep.(UdpLocalReader).ReadLocal(&add)
			if err == tcpip.ErrWouldBlock {
				select {
				case <-ch:
					continue
				}
			}

			la := net.IP([]byte(add.LocalAddr))
			nt.Handler.HandleUdp(la, add.LocalPort,
				net.IP([]byte(add.FullAddress.Addr)), add.FullAddress.Port,
				v)

		}
	}()

	return nil
}

func (nt *NetstackTun) defUdp6Server() error {
	// Like a socket
	var wq waiter.Queue

	ep6, err := nt.IPStack.NewEndpoint(udp.ProtocolNumber, ipv6.ProtocolNumber, &wq)
	if err != nil {
		return errors.New(err.String())
	}
	err = ep6.Bind(tcpip.FullAddress{
		//Addr: "\x01", - error
		Addr: tcpip.Address(net.IPv6loopback),
		//Port: 2000,
		Port: 0xffff,
		NIC:  2,
	}, nil)
	if err != nil {
		ep6.Close()
		return errors.New(err.String())
	}
	nt.IPStack.Capture(ipv6.ProtocolNumber, udp.ProtocolNumber, ep6.(stack.TransportEndpoint))

	we, ch := waiter.NewChannelEntry(nil)
	wq.EventRegister(&we, waiter.EventIn)
	//defer wq.EventUnregister(&we)

	go func() {
		for {
			// Will have the peer address
			add := tcpip.DoubleAddress{}
			//ep.SetSockOpt()
			v, _, err := ep6.(UdpLocalReader).ReadLocal(&add)
			if err == tcpip.ErrWouldBlock {
				select {
				case <-ch:
					continue
				}
			}

			la := net.IP([]byte(add.LocalAddr))
			//if la.To4() == nil {
			//	log.Print("IP6 ", la)
			//}
			if add.LocalAddr[0] == 0xff {
				continue
			}

			nt.Handler.HandleUdp(la, add.LocalPort,
				net.IP([]byte(add.FullAddress.Addr)), add.FullAddress.Port,
				v)

		}
	}()

	return nil
}

var (
	Dump = false
)

func DefTcpServer(nt *NetstackTun, handler Gateway) (tcpip.Endpoint, waiter.Queue, error) {

	var wq waiter.Queue
	ep, err := nt.IPStack.NewEndpoint(tcp.ProtocolNumber, ipv4.ProtocolNumber, &wq)
	if err != nil {
		return nil, wq, errors.New(err.String())
	}

	// No address - listen on all
	err = ep.Bind(tcpip.FullAddress{
		Port: 0xffff,
	}, nil) // reserves port

	if err != nil {
		ep.Close()
		return nil, wq, errors.New(err.String())
	}
	nt.IPStack.Capture(ipv4.ProtocolNumber, tcp.ProtocolNumber, ep.(stack.TransportEndpoint))
	if err := ep.Listen(10); err != nil { // calls Register
		ep.Close()
		return nil, wq, errors.New(err.String())
	}

	we, listenCh := waiter.NewChannelEntry(nil)
	wq.EventRegister(&we, waiter.EventIn)

	// receive TCP packets on port
	go func() {
		defer wq.EventUnregister(&we)
		for {
			epin, wqin, err := ep.Accept()
			if err != nil {
				if err == tcpip.ErrWouldBlock {
					<-listenCh
					continue
				}
				log.Println("Unexpected accept error")
			}
			if Dump {
				add, _ := epin.GetRemoteAddress()
				ladd, _ := epin.GetLocalAddress()
				log.Printf("TUN: Accepted %v %v", ladd, add)
			}

			conn := gonet.NewConn(wqin, epin)
			la, err := epin.GetLocalAddress()
			if err != nil {
				log.Println("Error getting local address ", err)
				continue
			}
			go func() {
				ra, _ := epin.GetRemoteAddress()
				proxy := handler.NewStream(a2na(ra.Addr), ra.Port, "TUN", nil, conn, conn).(StreamProxy)
				defer proxy.Close()
				err := proxy.Dial("", &net.TCPAddr{Port: int(la.Port), IP: net.IP([]byte(la.Addr))})
				if err != nil {
					return
				}
				proxy.Proxy()
			}()

		}
	}()

	return ep, wq, nil
}

func DefTcp6Server(nt *NetstackTun, handler Gateway) (tcpip.Endpoint, waiter.Queue, error) {
	var wq waiter.Queue
	ep, err := nt.IPStack.NewEndpoint(tcp.ProtocolNumber, ipv6.ProtocolNumber, &wq)
	if err != nil {
		return nil, wq, errors.New(err.String())
	}

	// No address - listen on all
	err = ep.Bind(tcpip.FullAddress{
		Addr: tcpip.Address(net.IPv6loopback),
		Port: 0xffff,
		NIC:  2,
	}, nil) // reserves port
	if err != nil {
		ep.Close()
		return nil, wq, errors.New(err.String())
	}
	nt.IPStack.Capture(ipv6.ProtocolNumber, tcp.ProtocolNumber, ep.(stack.TransportEndpoint))

	if err := ep.Listen(10); err != nil { // calls Register
		ep.Close()
		return nil, wq, errors.New(err.String())
	}

	we, listenCh := waiter.NewChannelEntry(nil)
	wq.EventRegister(&we, waiter.EventIn)

	// receive TCP packets on port
	go func() {
		defer wq.EventUnregister(&we)
		for {
			epin, wqin, err := ep.Accept()
			if err != nil {
				if err == tcpip.ErrWouldBlock {
					<-listenCh
					continue
				}
				log.Println("Unexpected accept error")
			}
			if Dump {
				add, _ := epin.GetRemoteAddress()
				ladd, _ := epin.GetLocalAddress()
				log.Printf("TUN: Accepted %v %v", ladd, add)
			}

			conn := gonet.NewConn(wqin, epin)
			la, err := epin.GetLocalAddress()
			if err != nil {
				log.Println("Error getting local address ", err)
				continue
			}
			go func() {
				ra, _ := epin.GetRemoteAddress()
				proxy := handler.NewStream(a2na(ra.Addr), ra.Port, "TUN", nil, conn, conn).(StreamProxy)
				defer proxy.Close()
				err := proxy.Dial("", &net.TCPAddr{Port: int(la.Port), IP: net.IP([]byte(la.Addr))})
				if err != nil {
					return
				}
				proxy.Proxy()

			}()

		}
	}()

	return ep, wq, nil
}

func a2na(address tcpip.Address) net.IP {
	ab := []byte(address)
	return net.IP(ab)
}

func setRouteTable(ipstack *stack.Stack, real bool) {
	ipstack.SetRouteTable([]tcpip.Route{
		{
			Destination: "\x7f\x00\x00\x00",
			Mask:        "\xff\x00\x00\x00",
			Gateway:     "",
			NIC:         1,
		},
		{
			Destination: "\x0a\x0c\x00\x02",
			Mask:        "\xff\xff\xff\xff",
			Gateway:     "",
			NIC:         1,
		},
		{
			Destination: "\x0a\x0c\x00\x00",
			Mask:        "\xff\xff\x00\x00",
			Gateway:     "",
			NIC:         2,
		},
		{
			Destination: "\x00\x00\x00\x00",
			Mask:        "\x00\x00\x00\x00",
			Gateway:     "",
			NIC:         2,
		},
		{
			Destination: tcpip.Address(strings.Repeat("\x00", 16)),
			Mask:        tcpip.AddressMask(strings.Repeat("\x00", 16)),
			Gateway:     "",
			NIC:         2,
		},
	})
}

/*
	Terms:
 - netstack - the network stack implementation
 - nic - virtual interface
 -- route table
 -- address

 - packet injected and sent by link - dmtun (but doesn't work android) or channel based

 - View - slice of buffer, TrimFront, CapLength,
 -
*/
