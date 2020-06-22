package netstacktun

import (
	"github.com/costinm/wpgate/pkg/h2"
	"github.com/costinm/wpgate/pkg/mesh"

	"log"
	"net"
	"testing"
)

// Test for capture using TUN on AMD64.

/*
 sudo ip tuntap add dev dmesh1 mode tun user ${USER-costin}
 sudo ip addr add ${IP4:-10.12.0.1/16} dev dmesh1
 sudo ip link set dmesh1 up

 nc 10.12.123.123 456
*/

// Checks we can open the tun directly. On android, it will be opened by android VPN
// and passed as a FD.
func TestTcpCaptureReal(t *testing.T) {
	// real echo server on 2000, 3000
	initFakes()

	fd, err := OpenTun("dmesh1")
	if err != nil {
		t.Skip("TUN can't be opened, make sure it is setup", err)
		return
	}

	// Currently requires AMD64, fdbased doesn't compile on arm.
	//linkID := fdbased.New(&fdbased.Options{
	//	MTU: 1500,
	//	FD:  fd,
	//})

	h21, _ := h2.NewH2("")
	tp := mesh.New(h21.Certs, nil)

	linkID := NewReaderWriterLink(fd, fd, &Options{MTU: 1600})
	tun := NewTunCapture(&linkID, tp, true)

	t.Run("external", func(t *testing.T) {
		testTcpEchoLocal(t, tun, "www.webinf.info", 5227)
	})
	t.Run("external6", func(t *testing.T) {
		testTcpEchoLocal(t, tun, "ip6.webinf.info", 5227)
	})

	t.Run("local", func(t *testing.T) {
		testTcpEcho(t, tun, "127.0.0.1", 2000)
		testTcpEcho(t, tun, "127.0.0.1", 3000)
	})
}

// Using the net stack for testing.
// Requires an 'ip route add 73.158.64.15/32 dev  dmesh1'
// and ip route add  2001:470:1f04:429::2 dev dmesh1
func testTcpEchoLocal(t *testing.T, tn *NetstackTun, addr string, port uint16) {
	ip4, err := net.ResolveIPAddr("ip", addr)
	if err != nil {
		t.Fatal("Can't resolve ", err)
	}

	c1, err := net.DialTCP("tcp", nil, &net.TCPAddr{IP: ip4.IP, Port: int(port)})

	if err != nil {
		t.Fatal("Failed to dial ", err)
	}

	c1.Write([]byte("GET / HTTP/1.1\nHost:www.webinf.info\n\n"))

	data := make([]byte, 1024)
	n, _ := c1.Read(data[0:])
	log.Println("Recv: ", c1.RemoteAddr(), string(data[:n]))
	c1.Close()

	//tcpClient(t)
}
