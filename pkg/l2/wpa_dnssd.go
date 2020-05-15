package l2

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"log"
	"strconv"
	"strings"

	mesh "github.com/costinm/dmesh-l2/pkg/l2api"
	//"github.com/costinm/dmesh/dm/mesh"
)

// Minimal DNS-SD support, used to parse WPA "P2P-SERV-DISC-RESP" for older Android versions.
// From Android Q, the P2P uses a fixed prefix (DIRECT-DM-ESH-) and fixed PSK - encryption is at L6,
// we don't rely on link-local encryption.
// Older Android devices use a special TXT record to advertise the PSK and SSID (and few other things)

var (
	dnsErr = errors.New("DNS")
)

// dm._dm._udp.local. TXT 01
const nameTxt = "02646d035f646dc01c001001"

/*
 * Protocol format is as follows.<br>
 * See the Table.62 in the WiFi Direct specification for the detail.
 * ______________________________________________________________
 * |           Length(2byte)     | Type(1byte) | TransId(1byte)}|
 * ______________________________________________________________
 * | status(1byte)  |            vendor specific(variable)      |
 *
 * P2P-SERV-DISC-RESP 42:fc:89:e1:e2:27 1 0300000101
 * length=3, service type=0(ALL Service), transaction id=1,

 * status=1(service protocol type not available)<br>
 *
 * P2P-SERV-DISC-RESP 42:fc:89:e1:e2:27 1 0300020201
 * length=3, service type=2(UPnP), transaction id=2,
 * status=1(service protocol type not available)
...
* UPnP Protocol format is as follows.
 * ______________________________________________________
 * |  Version (1)  |          USN (Variable)            |
 *
 * version=0x10(UPnP1.0) data=usn:uuid:1122de4e-8574-59ab-9322-33345678
 * 9044::urn:schemas-upnp-org:service:ContentDirectory:2,usn:uuid:6859d
 * ede-8574-59ab-9332-123456789012::upnp:rootdevice
 *
 * P2P-SERV-DISC-RESP 58:17:0c:bc:dd:ca 21 1900010200045f6970
 * 70.c00c000c01094d795072696e746572c027
 * PREFIX DNS_NAME C0 0C 00 01 01 TXT_RECORDS
 * length=25, type=1(Bonjour),transaction id=2,status=0
 *
 * Bonjour Protocol format is as follows.
 * __________________________________________________________
 * |DNS Name(Variable)|DNS Type(1)|Version(1)|RDATA(Variable)|
 *
 * DNS Name=_ipp._tcp.local.,DNS type=12(PTR), Version=1,
 * RDATA=MyPrinter._ipp._tcp.local.
*/

func parseDisc(msg []string, d *mesh.MeshDevice) bool {
	d.MAC = msg[1]
	d.ServiceUpdateInd, _ = strconv.Atoi(msg[2])

	data := make([]byte, len(msg[3])/2)
	n, _ := hex.Decode(data, []byte(msg[3]))
	if n != len(msg[3])/2 {
		return false
	}
	off := 0
	mlen := binary.LittleEndian.Uint16(data[off:])
	off += 2

	if data[off] != 1 {
		return false
	}
	off++

	off++ // transaction id

	if data[off] != 0 {
		log.Println("Invalid status ", data[off], msg)
		return false
	}
	off++

	dnsData := data[off : mlen+2]

	// Vendor specific: DNS Name, DNS Type, Version, RDATA
	name, end, err := unpackDomainName(dnsData, 0)
	if err != nil {
		log.Println(string(data))
		return false
	}
	if name != "dm._dm._udp.local." {
		log.Println("Unexpected name", name)
		return false
	}
	off = end
	off++ // 0

	if dnsData[off] != 0x10 { // TXT
		return false
	}
	// 12 == PTR, 16 = TXT
	// 1 - version

	off += 2

	txtData := dnsData[off : mlen+2]
	meta := parseDns(txtData)

	d.PSK = meta["p"]
	d.SSID = meta["s"]
	d.Net = meta["c"]

	log.Println("WPA/DNS: ", meta)
	return true
}

func packDns(req string, rec map[string]string) []byte {
	reqB, _ := hex.DecodeString(req)

	bb := bytes.Buffer{}

	bb.WriteByte(0)
	bb.WriteByte(0)

	bb.WriteByte(1) // dnssd
	// TODO: need the trans id of the 01 query - 02000111 02000112
	bb.WriteByte(reqB[3]) // transid.
	bb.WriteByte(0)       // status

	txtb, _ := hex.DecodeString(nameTxt)
	bb.Write(txtb)

	packTxtBytes(&bb, rec)

	bba := bb.Bytes()
	bbal := len(bba) - 2
	bba[0] = byte(bbal % 256)
	bba[1] = byte(bbal / 256)
	return bba
}

func packTxt(rec map[string]string) string {

	bb := bytes.Buffer{}
	packTxtBytes(&bb, rec)

	bba := bb.Bytes()
	return hex.EncodeToString(bba)
}

func packTxtBytes(bb *bytes.Buffer, rec map[string]string) {
	for k, v := range rec {
		rec := k + "=" + v
		bb.WriteByte(byte(len(rec)))
		bb.Write([]byte(rec))
		//bb.WriteByte(byte(len(v)))
		//bb.Write([]byte(v))
	}
}

func parseDns(dnsData []byte) map[string]string {
	off := 0
	res := map[string]string{}
	for {
		if off >= len(dnsData) {
			break
		}
		slen := int(dnsData[off])
		if slen == 0 || (int(off+slen) > int(len(dnsData))) {
			break
		}
		off++
		kv := string(dnsData[off : off+int(slen)])
		kvp := strings.Split(kv, "=")
		if len(kvp) == 2 {
			k := kvp[0]
			res[k] = kvp[1]
		}
		off += int(slen)
	}
	return res
}

// unpackDomainName unpacks a domain name into a string.
// Code from miekg, modified for WifiDirect.
// TODO: for dmesh we don't reammy need this, TXT record prefix is constant
// dm._dm._udp.local or '\02dm\03_dm\c0\1c' plus
func unpackDomainName(msg []byte, off int) (string, int, error) {
	s := make([]byte, 0, 64)
	off1 := 0
	lenmsg := len(msg)
	maxLen := 255
	ptr := 0 // number of pointers followed
Loop:
	for {
		if off >= lenmsg {
			return "", lenmsg, dnsErr
		}
		c := int(msg[off])
		off++
		switch c & 0xC0 {
		case 0x00:
			if c == 0x00 {
				// end of name
				break Loop
			}
			// literal string
			if off+c > lenmsg {
				return "", lenmsg, dnsErr
			}
			for j := off; j < off+c; j++ {
				switch b := msg[j]; b {
				case '.', '(', ')', ';', ' ', '@':
					fallthrough
				case '"', '\\':
					s = append(s, '\\', b)
					// presentation-format \X escapes add an extra byte
					maxLen++
				default:
					if b < 32 || b >= 127 { // unprintable, use \DDD
						var buf [3]byte
						bufs := strconv.AppendInt(buf[:0], int64(b), 10)
						s = append(s, '\\')
						for i := 0; i < 3-len(bufs); i++ {
							s = append(s, '0')
						}
						for _, r := range bufs {
							s = append(s, r)
						}
						// presentation-format \DDD escapes add 3 extra bytes
						maxLen += 3
					} else {
						s = append(s, b)
					}
				}
			}
			s = append(s, '.')
			off += c
		case 0xC0:
			// pointer to somewhere else in msg.
			// remember location after first ptr,
			// since that's how many bytes we consumed.
			// also, don't follow too many pointers --
			// maybe there's a loop.
			if off >= lenmsg {
				return "", lenmsg, dnsErr
			}
			c1 := msg[off]
			off++
			if ptr == 0 {
				off1 = off
			}
			if ptr++; ptr > 10 {
				return "", lenmsg, dnsErr
			}
			// pointer should guarantee that it advances and points forwards at least
			// but the condition on previous three lines guarantees that it's
			// at least loop-free
			off = (c^0xC0)<<8 | int(c1)
			if off == 0x27 {
				// add dns query - only used for PTR record responses.
				return string(s) + ".QRY.", off1, nil
			}
			if off == 0x0c {
				return string(s) + "_tcp.local.", off1, nil
			}
			if off == 0x11 {
				return string(s) + "local.", off1, nil
			}
			if off == 0x1c {
				return string(s) + "_udp.local.", off1, nil
			}

		default:
			// 0x80 and 0x40 are reserved
			return "", lenmsg, dnsErr
		}
	}
	if ptr == 0 {
		off1 = off
	}
	if len(s) == 0 {
		s = []byte(".")
	} else if len(s) >= maxLen {
		// error if the name is too long, but don't throw it away
		return string(s), lenmsg, dnsErr
	}
	return string(s), off1, nil
}
