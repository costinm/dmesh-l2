package l2

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"log"
	"net"
	"time"

	"github.com/costinm/dmesh-l2/pkg/l2/wifi"
	"github.com/costinm/dmesh-l2/pkg/l2api"
	"github.com/google/gopacket"
	"github.com/google/gopacket/layers"

	//	"golang.org/x/sys/unix"
	//	"golang.org/x/net/bpf"
	//"github.com/google/gopacket/afpacket"
	//"github.com/google/gopacket/layers"
	"github.com/google/gopacket/pcapgo"
	"golang.org/x/net/bpf"
)

// control - show control frames ( ? )
// otherbss - to see NAN BSS
//
//
// iw phy phy0 interface add mon0 type monitor flags control otherbss
// ifconfig mon0 up

// Handling of the 'mon' interface.
// Receives raw frames, send raw frames.
// EBPF for filtering

//filter action frame packets
//Equivalent for tcp dump :
//type 0 subtype 0xd0 and wlan[24:4]=0x7f18fe34 and wlan[32]=221 and wlan[33:4]&0xffffff = 0x18fe34 and wlan[37]=0x4
//NB : There is no filter on source or destination addresses, so this code will 'receive' the action frames sent by this computer...

// afpacketComputeSize computes the block_size and the num_blocks in such a way that the
// allocated mmap buffer is close to but smaller than target_size_mb.
// The restriction is that the block_size must be divisible by both the
// frame size and page size.
func afpacketComputeSize(targetSizeMb int, snaplen int, pageSize int) (
	frameSize int, blockSize int, numBlocks int, err error) {

	if snaplen < pageSize {
		frameSize = pageSize / (pageSize / snaplen)
	} else {
		frameSize = (snaplen/pageSize + 1) * pageSize
	}

	// 128 is the default from the gopacket library so just use that
	blockSize = frameSize * 128
	numBlocks = (targetSizeMb * 1024 * 1024) / blockSize

	if numBlocks == 0 {
		return 0, 0, 0,
			fmt.Errorf("Interface buffersize is too small %d %d", frameSize, blockSize)
	}

	return frameSize, blockSize, numBlocks, nil
}

func (l2 *L2) InitMon(iface *wifi.Interface) error {
	// boilerplate.
	//szFrame, szBlock, numBlocks, err := afpacketComputeSize(8, /*MB*/
	//	4096, os.Getpagesize())
	//if err != nil {
	//	log.Fatal(err)
	//}

	//https://github.com/google/gopacket/issues/652
	// - doesn't compile on ARM ( or MIPS )
	//
	// Not using "C" (so far), pure Go
	//tp, err := afpacket.NewTPacket(
	//	afpacket.OptInterface(iface.Name),
	//	afpacket.OptFrameSize(szFrame),
	//	afpacket.OptBlockSize(szBlock),
	//	afpacket.OptNumBlocks(numBlocks),
	//	afpacket.SocketRaw,
	//	afpacket.TPacketVersion3)
	//if err != nil {
	//	return err
	//}

	// pcap depends on "C" pcap.h
	bpfIns := []bpf.RawInstruction{
		bpf.RawInstruction{Op: 0x30, K: 3}, // 0: ldb [3]
		bpf.RawInstruction{Op: 0x64, K: 8}, // 1: lsh #8
		bpf.RawInstruction{Op: 0x07},       // 2: tax
		bpf.RawInstruction{Op: 0x30, K: 2}, // 3: ldb [2]
		bpf.RawInstruction{Op: 0x4C, K: 2}, // 4: or x // header len in A
		bpf.RawInstruction{Op: 0x07},       // 5: tax

		bpf.RawInstruction{Op: 0x40, K: 16},                       // 6: ld [x+4+6+6] // BSSID first word
		bpf.RawInstruction{Op: 0x15, Jt: 0, Jf: 1, K: 0x506F9A01}, // 7: jeq # jt 8 jf 9

		bpf.RawInstruction{Op: 6, K: 0x00040000}, // ret true

		bpf.RawInstruction{Op: 6, K: 0}, // ret false
	}

	eh,err  := pcapgo.NewEthernetHandle(iface.Name)
	if err != nil {
		log.Println("Failed to set BPF", err)
		return err
	}
	err = eh.SetBPF(bpfIns)
	if err != nil {
		log.Println("Failed to set BPF", err)
		return err
	}


	//if tp.SetBPF(bpfIns); err != nil {
	//	log.Println("Failed to set BPF")
	//	//return err
	//}

	if true {
		for {
			// 12 bytes header + raw 802.11 packet

			// https://www.kernel.org/doc/Documentation/networking/radiotap-headers.txt
			//    0x00, 0x00, // <-- radiotap version + pad byte
			//		0x0b, 0x00, // <- radiotap header length
			//		0x04, 0x0c, 0x00, 0x00, // <-- bitmap
			//		0x6c, // <-- rate (in 500kHz units)
			//		0x0c, //<-- tx power
			//		0x01 //<-- antenna
			//
			d, ci, err := eh.ReadPacketData()
			if err != nil {
				return err
			}
			now := time.Now()

			//{Contents=[..56..] Payload=[..52..] Version=0 Length=56
			// Present=2688565295 TSFT=1140624841
			// Flags= Rate=1 Mb/s
			// ChannelFrequency=2437 MHz
			// ChannelFlags=CCK,Ghz2
			// FHSS=0
			// DBMAntennaSignal=-26
			// DBMAntennaNoise=0
			// LockQuality=0
			// TxAttenuation=0
			// DBTxAttenuation=0
			// DBMTxPower=0
			// Antenna=0
			// DBAntennaSignal=0 DBAntennaNoise=0
			// RxFlags= TxFlags= RtsRetries=0 DataRetries=0
			// MCS= AMPDUStatus=ref#0 VHT=}
			// It doesn't seem to get the channel if the packet is going out

			p := gopacket.NewPacket(d, layers.LayerTypeRadioTap, gopacket.Default)

			pls := p.Layers()
			rtap := pls[0].(*layers.RadioTap)

			// should be len = 3
			// layer[1] should be Dot11
			dot1l := pls[1]
			d11 := dot1l.(*layers.Dot11)
			//ma := pls[2].(*layers.Dot11MgmtAction)

			if bytes.Equal(d11.Address2, iface.HardwareAddr) {
				continue
			}

			// Note: the decoded type is data[0]>>2,
			if d11.Type == layers.Dot11TypeMgmtAction {
				d := pls[2].LayerContents()
				if len(d) > 6 &&
					d[0] == 4 && // Action
					d[1] == 9 && // vendor
					d[2] == 0x50 && d[3] == 0x6F && d[4] == 0x9A && // WifiAll
					d[5] == 0x13 { // NAN

					ies, err := wifi.ParseATTs(d[6:])
					if err != nil {
						log.Println(err)
						log.Println(pls[2].LayerContents())
						//log.Println(p.Dump())
						continue
					}

					for _, ie := range ies {
						log.Println("IE:", ie.ID, d11.Address2,
							now.Unix(), ci.InterfaceIndex, "\n", hex.Dump(ie.Data))
					}

					continue
				}
			} else if d11.Type == layers.Dot11TypeMgmtBeacon {
				b := pls[2].(*layers.Dot11MgmtBeacon)

				l2.m.Lock()

				key := Uint64(d11.Address2)
				node := l2.devByL2Id[key]
				if node == nil {
					node = &l2api.MeshDevice{}
					l2.devByL2Id[key] = node
					log.Println("Beacon:", iface.Name,
						d11.Address2,
						b.Interval, b.Timestamp,
						now.Unix())
				}

				l2.m.Unlock()

				// Has decoded layers for IE, timestamp, etc
				continue
			}

			// AncillaryData typically empty
			// CaptureLength -
			log.Println("MON Read", ci.AncillaryData, ci.InterfaceIndex,
				d11.Address2, rtap)
			log.Println(p.Dump())
		}
	}

	return nil
}

func Uint64(b net.HardwareAddr) uint64 {
	return uint64(b[0])<<16 | uint64(b[1])<<24 |
		uint64(b[2])<<32 | uint64(b[3])<<40 | uint64(b[4])<<48 | uint64(b[5])<<56
}
