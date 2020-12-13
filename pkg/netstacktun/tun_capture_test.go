package netstacktun

import (
	"github.com/costinm/wpgate/pkg/tests"
	"github.com/costinm/wpgate/pkg/h2"
	"github.com/costinm/wpgate/pkg/mesh"

	"github.com/google/netstack/tcpip"
	"github.com/google/netstack/tcpip/adapters/gonet"
	"github.com/google/netstack/tcpip/buffer"
	"github.com/google/netstack/tcpip/header"
	"github.com/google/netstack/tcpip/network/ipv4"
	"github.com/google/netstack/tcpip/network/ipv6"
	"github.com/google/netstack/tcpip/transport/udp"

	"log"
	"net"
	"os"
	"testing"
)

var (
	fakes = false
)

var (
	// Used for packets going io/out.
	linkr *os.File
	linkw *os.File
	// stack - local testing
	link tcpip.LinkEndpointID
)

func initFakes() {
	if !fakes {
		fakes = true
		go tests.InitEchoServer(":3000")
		go tests.InitEchoServer(":2000")
	}
}

func TestTcpCapture(t *testing.T) {
	//initTestServerLocal(t)

	initPipeLink()
	initFakes()

	Dump = true
	h21, _ := h2.NewH2("")
	tp := mesh.New(h21.Certs, nil)

	tun := NewTunCapture(&link, tp, nil, true)

	t.Run("v4", func(t *testing.T) {
		testTcpEcho(t, tun, "127.0.0.1", 3000)
		testTcpEcho(t, tun, "127.0.0.1", 2000)
	})

	t.Run("v6", func(t *testing.T) {
		testTcpEcho6(t, tun, 3000)
	})
}

func TestUdpCapture(t *testing.T) {
	err := tests.InitEchoUdp(2000)
	if err != nil {
		t.Fatal("UDP proxy", err)
	}

	// init link, lr, lw
	initPipeLink()
	tc := NewTunCapture(&link, nil, nil, true)

	h21, _ := h2.NewH2("")
	tp := mesh.New(h21.Certs, nil)

	tc.Handler = tp

	//tp.UDPWriter = tc

	//addr1 := make([]byte, 16)
	//addr1[15] = 1
	//addr2 := make([]byte, 16)
	//addr2[15] = 2

	t.Run("v4", func(t *testing.T) {
		srcAddr := []byte{10, 0, 0, 1}
		dstAddr := []byte{127, 0, 0, 1}

		ip62 := makeV4UDP([]byte("Hello"),
			tcpip.Address(srcAddr),
			tcpip.Address(dstAddr), 1000, 2000)

		go linkw.Write(ip62)

		data := make([]byte, 2048)
		n, err1 := linkr.Read(data)
		log.Println("received 3", n, err1)
	})
	t.Run("v6", func(t *testing.T) {
		srcAddr := net.IPv6loopback
		dstAddr := net.IPv6loopback

		ip62 := makeV6UDP([]byte("Hello"),
			tcpip.Address(srcAddr),
			tcpip.Address(dstAddr), 1000, 2000)

		go linkw.Write(ip62)

		data := make([]byte, 2048)
		n, err1 := linkr.Read(data)
		log.Println("received 3", n, err1)
	})
}

// init a network interface backed by 2 os pipes, linkr and linkw.
// 'link' is the netstack link
func initPipeLink() {
	if linkr != nil {
		return
	}
	lr, stw, _ := os.Pipe()
	pr, lw, _ := os.Pipe()
	linkr = lr
	linkw = lw

	link = NewReaderWriterLink(stw, pr, &Options{MTU: 1600})
}

// Format a UDP packet, with IP6 header.
// This is an example of how to create UDP and IP packets using the stack.
// In practical use, the gonet interface is much easier.
func makeV4UDP(payload []byte, src, dst tcpip.Address, srcport, dstport uint16) []byte {
	// Allocate a buffer for data and headers.
	buf := buffer.NewView(header.UDPMinimumSize + header.IPv4MinimumSize + len(payload))

	// payload at the end
	copy(buf[len(buf)-len(payload):], payload)

	// Initialize the IP header.
	ip := header.IPv4(buf)
	ip.Encode(&header.IPv4Fields{
		TotalLength: uint16(len(buf)),
		Protocol:    uint8(udp.ProtocolNumber),
		TTL:         65,
		SrcAddr:     src,
		DstAddr:     dst,
		IHL:         header.IPv4MinimumSize,
	})

	// Initialize the UDP header.
	u := header.UDP(buf[header.IPv4MinimumSize:])
	u.Encode(&header.UDPFields{
		SrcPort: srcport,
		DstPort: dstport,
		Length:  uint16(header.UDPMinimumSize + len(payload)),
	})

	// Calculate the UDP pseudo-header checksum.
	xsum := header.Checksum([]byte(src), 0)
	xsum = header.Checksum([]byte(dst), xsum)
	xsum = header.Checksum([]byte{0, uint8(udp.ProtocolNumber)}, xsum)

	// Calculate the UDP checksum and set it.
	length := uint16(header.UDPMinimumSize + len(payload))
	xsum = header.Checksum(payload, xsum)
	u.SetChecksum(^u.CalculateChecksum(xsum, length))

	return buf
}

func makeV6UDP(payload []byte, src, dst tcpip.Address, srcport, dstport uint16) []byte {
	// Allocate a buffer for data and headers.
	buf := buffer.NewView(header.UDPMinimumSize + header.IPv6MinimumSize + len(payload))

	// payload at the end
	copy(buf[len(buf)-len(payload):], payload)

	// Initialize the IP header.
	ip := header.IPv6(buf)
	ip.Encode(&header.IPv6Fields{
		PayloadLength: uint16(header.UDPMinimumSize + len(payload)),
		NextHeader:    uint8(udp.ProtocolNumber),
		HopLimit:      65,
		SrcAddr:       src,
		DstAddr:       dst,
	})

	// Initialize the UDP header.
	u := header.UDP(buf[header.IPv6MinimumSize:])
	u.Encode(&header.UDPFields{
		SrcPort: srcport,
		DstPort: dstport,
		Length:  uint16(header.UDPMinimumSize + len(payload)),
	})

	// Calculate the UDP pseudo-header checksum.
	xsum := header.Checksum([]byte(src), 0)
	xsum = header.Checksum([]byte(dst), xsum)
	xsum = header.Checksum([]byte{0, uint8(udp.ProtocolNumber)}, xsum)

	// Calculate the UDP checksum and set it.
	length := uint16(header.UDPMinimumSize + len(payload))
	xsum = header.Checksum(payload, xsum)
	u.SetChecksum(^u.CalculateChecksum(xsum, length))

	return buf
}

// Using the gonet stack for testing. This doesn't require a TUN device.
// gonet implements the normal go.net interfaces, but using the soft stack.
func testTcpEcho(t *testing.T, tn *NetstackTun, addr string, port uint16) {
	ip4, err := net.ResolveIPAddr("ip", addr)
	if err != nil {
		t.Fatal("Can't resolve ", err)
	}

	c1, err := gonet.DialTCP(tn.IPStack, tcpip.FullAddress{
		// Doesn't seem to work - regardless of routes.
		//Addr: tcpip.Address(net.IPv4(10, 12, 0, 2).To4()),
		Addr: tcpip.Address(ip4.IP.To4()),
		Port: port,
	}, ipv4.ProtocolNumber)
	if err != nil {
		t.Fatal("Failed to dial ", err)
	}

	tests.TcpEchoTest(c1)
}

func testTcpEcho6(t *testing.T, tn *NetstackTun, port uint16) {
	ip6 := net.IPv6loopback

	c1, err := gonet.DialTCP(tn.IPStack, tcpip.FullAddress{
		// Doesn't seem to work - regardless of routes.
		//Addr: tcpip.Address(net.IPv4(10, 12, 0, 2).To4()),
		Addr: tcpip.Address(ip6),
		Port: port,
		NIC:  3,
	}, ipv6.ProtocolNumber)

	if err != nil {
		t.Fatal("Failed to dial ", err)
	}

	c1.Write([]byte("GET / HTTP/1.1\n\n"))

	data := make([]byte, 1024)
	n, _ := c1.Read(data[0:])
	log.Println("Recv: ", string(data[:n]))
	c1.Close()

	//tcpClient(t)
}

//func TestUdpEcho(t *testing.T) {
//	l, err := net.ListenUDP("udp4", &net.UDPAddr{Port: 1999})
//	if err != nil {
//		t.Fatal(err)
//	}
//	ip, err := net.ResolveIPAddr("ip", "h.webinf.info")
//	if err != nil {
//		t.Fatal(err)
//	}
//	go func() {
//		for {
//			l.WriteToUDP([]byte("Hi1"), &net.UDPAddr{Port: 5228, IP: ip.IP})
//			time.Sleep(4 * time.Second)
//		}
//	}()
//	for {
//		b := make([]byte, 1600)
//		n, addr, _ := l.ReadFromUDP(b)
//		log.Println("RCV: ", addr, string(b[0:n]))
//	}
//
//}
