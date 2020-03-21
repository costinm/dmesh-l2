package iptables

// WIP: using iptables to capture traffic and send it to the L2 mesh.
//
// This is used for routing packets from the mesh and node.

import (
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"syscall"

	"github.com/costinm/dmesh-l2/pkg/l2"
)

type DMRoot struct {
	IFname string

	// TUN device - for Android compat. If running as root, use TPROXY mode, lower
	// overhead ?
	//vpnFD    *os.File

	TProxyFD *os.File

	vpnStarted bool

	WPA []*l2.WifiInterface

	mutex sync.Mutex

	// Configure iptables when a user-space app connects
	AutoIptables bool
}

// Todo: download for arch

func (dmr *DMRoot) StartDmesh() {
	// TODO: if $FILES/dmesh.new exists, delete old one and replace

	cmd := exec.Command("bin/dmesh")
	cmd.SysProcAttr = &syscall.SysProcAttr{}
	cmd.SysProcAttr.Credential = &syscall.Credential{Uid: 1337, Gid: 1337}
	cmd.Run()
}

// Iptables / TUN commands
func (dmr *DMRoot) handleCommand(cc int, s string, uid uint32) error {
	var err error
	switch cc {
	case 'w': // VPN - request capture via VPN.
		if dmr.AutoIptables {
			c(routeOff)
			c(route6Off)

			//for x := range routeOnCmds {
			//	c(x)
			//}

			if err = c(fmt.Sprintf(routeOn, uid)); err != nil {
				log.Print("Failed to enable iptables ", err)
			} else {
				if err = c(routeOn1); err != nil {
					log.Print("Failed to enable iptables ", err)
				}
				c(routeOn2)
			}
			if err = c(fmt.Sprintf(route6On, uid)); err != nil {
				log.Print("Failed to enable iptables ", err)
			} else {
				if err = c(route6On1); err != nil {
					log.Print("Failed to enable iptables ", err)
				}
				c(route6On2)
			}
		}
	case 'R': // Init TUN
	case 'I': // Init TUN
		//copy(meshID, data[2:10])
		//if dm.vpnFD != nil {
		//	err = dm.startTun(meshID)
		//	if err != nil {
		//		log.Println(err)
		//		os.Exit(1)
		//	}
		//	conn.WriteFD([]byte("V\n"), dm.vpnFD)
		//
		//}
	default: // WifiInterface
	}
	return err
}

var (
	// IPv6 net address of the mesh.
	// TODO: env variable
	MESH_NETWORK = []byte{0x20, 0x01, 0x04, 0x70, 0x1f, 0x04, 4, 0x29}
)

var (
	initCmds = []string{
		"ip tuntap add dev dmesh1 mode tun user 1337",
		"ip addr add 10.12.0.5 dev dmesh1",
		"ip link set dmesh1 up",

		"iptables -t filter -N DMESH_FILTER_IN",
		"ip6tables -t filter -N DMESH_FILTER_IN",
		"iptables -t filter -A INPUT -j DMESH_FILTER_IN",
		"ip6tables -t filter -A INPUT -j DMESH_FILTER_IN",
		"iptables -t filter -A DMESH_FILTER_IN -i dmesh1 -j ACCEPT",
		"ip6tables -t filter -A DMESH_FILTER_IN -i dmesh1 -j ACCEPT",

		"iptables -t mangle -N DMESH_MANGLE_PRE",
		"ip6tables -t mangle -N DMESH_MANGLE_PRE",
		"iptables -t mangle -A PREROUTING -j DMESH_MANGLE_PRE",
		"ip6tables -t mangle -A PREROUTING -j DMESH_MANGLE_PRE",
		"iptables -t mangle -A DMESH_MANGLE_PRE -i dmesh1 -j MARK --set-mark 1337",
		"ip6tables -t mangle -A DMESH_MANGLE_PRE -i dmesh1 -j MARK --set-mark 1337",

		"iptables -t mangle -N DMESH_MANGLE_OUT",
		"ip6tables -t mangle -N DMESH_MANGLE_OUT",
		"iptables -t mangle -A OUTPUT -j DMESH_MANGLE_OUT",
		"ip6tables -t mangle -A OUTPUT -j DMESH_MANGLE_OUT",

		"iptables -t mangle -N DMESH",
		"ip6tables -t mangle -N DMESH",

		//echo 2 > /proc/sys/net/ipv4/conf/dmesh1/rp_filter

		"ip rule add fwmark 1338 lookup 1338",
		"ip route add ::/0 dev dmesh1 src 2001:470:1f04:429::3 table 1338",
		"ip route add 0.0.0.0/0 dev dmesh1 src 10.12.0.5 table 1338",

		"ip rule add fwmark 1337 lookup 1337",
		"ip route add local 0.0.0.0/0 dev lo table 1337",
		"ip rule add iif dmesh1 lookup 1337",
	}

	// Doesn't work on regular linux, only android.
	//routeOn = "/sbin/iptables -t mangle -A DMESH_MANGLE_OUT -m owner --pid-owner %d -j RETURN"

	// Commands executed when 'dmesh' is started, providing TUN VPN
	// Will set 1338 mark on all packets except local and used by local server
	routeOn = `iptables -t mangle -A DMESH_MANGLE_OUT -m owner --uid-owner %d -j RETURN`
	// TODO: skip multiple destinations, including VPN IP and unroutable.
	routeOn1 = "iptables -t mangle -A DMESH_MANGLE_OUT -d 127.0.0.1/32 -j RETURN"
	routeOn2 = "iptables -t mangle -A DMESH_MANGLE_OUT -j MARK --set-mark 1338"

	route6On  = `ip6tables -t mangle -A DMESH_MANGLE_OUT -m owner --uid-owner %d -j RETURN`
	route6On1 = "ip6tables -t mangle -A DMESH_MANGLE_OUT -d ::1/128 -j RETURN"
	route6On2 = "ip6tables -t mangle -A DMESH_MANGLE_OUT -j MARK --set-mark 1338"

	routeOff  = `iptables -t mangle -F DMESH_MANGLE_OUT`
	route6Off = `ip6tables -t mangle -F DMESH_MANGLE_OUT`
)

// Setup the dmesh interface with routes and rules.
// Will configure the IP address on the TUN interface and route the mesh range to it.
// It will also set masquerade for packet coming from the mesh range.
//
// The shell script is more up-to-date
//
// ifname - name of the interface to configure, for example 'dmesh'
// base - base IP4 - for example 10.12
// id - 8 byte local ID. Last 2 bytes will be used as ipv4. MESH_NETWORK is the first part.
func setupIf(ifname, base string, id []byte) error {
	/* Alternative:
	sudo iptables -t nat -N DMESH
	sudo iptables -t nat  -A DMESH -s 10.10.0.0/16 -j MASQUERADE
	sudo iptables -t nat  -A POSTROUTING -s 10.10.0.0/16 -j DMESH
	*/

	// Also: sudo iptables -t nat -A DMESH -s 10.1.10.0/24 -j RETURN
	if err := cmd("ip", "link", "set", ifname, "up"); err != nil {
		log.Println("Attempting to start error", err)
		return err
	}
	ip4 := base + "." + strconv.Itoa(int(id[6])) + "." + strconv.Itoa(int(id[7]))
	ip6 := make([]byte, 16)
	// 2001:470:1f04:429
	copy(ip6[0:], MESH_NETWORK)
	copy(ip6[8:], id)
	ip6Addr := net.IP(ip6)
	ip6AddrS := ip6Addr.String()
	for i := 8; i < 16; i++ {
		ip6[i] = 0
	}
	ip6Net := net.IP(ip6)
	ip6NetS := ip6Net.String()
	if err := cmd("ip", "addr", "add", ip4+"/16", "dev", ifname); err != nil {
		log.Println("Attempting to set address error", err)
		return err
	}
	if err := cmd("ip", "addr", "add", ip6AddrS+"/64", "dev", ifname); err != nil {
		log.Println("Attempting to set address error", err)
		return err
	}
	if err := cmd("ip", "route", "add", base+".0.0/16", "dev", ifname); err != nil {
		log.Println("Attempting to set route error", err)
		//return err
	}
	if err := cmd("ip", "route", "add", ip6NetS+"/64", "dev", ifname); err != nil {
		log.Println("Attempting to set route error", err)
		//return err
	}
	if err := cmd("iptables", "-t", "nat", "-X", ifname); err != nil {
		//return err
	}
	if err := cmd("iptables", "-t", "nat", "-N", ifname); err != nil {
		//return err
	}
	if err := cmd("iptables", "-t", "nat", "-A", ifname, "-s", base+".0.0/16", "-j", "MASQUERADE"); err != nil {
		//return err
	}
	if err := cmd("iptables", "-t", "nat", "-D", "POSTROUTING", "-s", base+".0.0/16", "-j", ifname); err != nil {
		//return err
	}
	if err := cmd("iptables", "-t", "nat", "-A", "POSTROUTING", "-s", base+".0.0/16", "-j", ifname); err != nil {
		//return err
	}
	return nil
}

func cmd(name string, arg ...string) error {
	log.Println(name, arg)
	return exec.Command(name, arg...).Run()
}

func c(name string) error {
	args := strings.Split(name, " ")
	log.Println(args)
	c := exec.Command(args[0], args[1:]...)
	c.Stderr = os.Stderr
	c.Stdout = os.Stdout
	return c.Run()
}

// If kernel ipsec is used, enable UDP encapsulation

//sudo setcap cap_net_admin+ep sopolicyd

// IPSec handling
// For debugging as user, it is easier to use the ip command.
// In prod, the netlink interface should be used, with NET_ADMIN caps.
