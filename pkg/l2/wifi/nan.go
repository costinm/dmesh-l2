package wifi

import (
	"encoding/binary"
	"encoding/hex"
	"log"
	"net"
	"strconv"
	"syscall"
	"time"

	"github.com/costinm/dmesh-l2/pkg/l2/nl80211"
	"github.com/mdlayher/genetlink"
	"github.com/mdlayher/netlink"
	"github.com/mdlayher/netlink/nlenc"

)

// Netlink calls to support subst of NAN and Android+ESP8266/ESP32 interop

// hostap/wpa_supplicant
// drivers: send_mlme, send_action,set_freq (CMD_SET_CHANNEL/CMD_SET_WIPHY),
// remain_on_channel,

// Monitor mode: wpa_supplicant_event(.. EVENT_RX_MGMT),
// from_unknown_sta(CTLR,data)
// IEEE80211_RADIOTAP_TX_FLAGS for tx status
// nl80211_send_monitor - creates radiotap header first !
//  "p2p interface name is p2p-%s-%d - monitor has same name with mon prefix".
//  "mon-ifname"

type NanClient struct {
	cr *genetlink.Conn
}

func (c *NanClient) Close() {
	c.cr.Close()
}

// Separate client thread receiving frames and messages.
// Send and 'stay on channel' using the primary channel, to avoid locking issues.
func (c *Client) StartReceive() {

	nlrc := c.cr

	for _, f := range c.family.Groups {
		//log.Println("Joining ", f.Name)
		nlrc.JoinGroup(f.ID)
	}

	nlrc.SetReadBuffer(40960)
	for {
		nlrc.SetReadDeadline(time.Now().Add(5 * time.Second))
		msgs, nmsgs, err := nlrc.Receive()
		if len(nmsgs) == 0 {
			continue
		}
		if err != nil {
			log.Println("NLReceive error", err)
			continue
		}
		sinceStart := time.Now().UnixNano()/1000000 - startTime

		// First array has 'unpacked' - i.e. parsed - messages
		// Second has 'packed' - i.e. just []byte

		m0 := nmsgs[0]
		if m0.Header.Type == netlink.Error {
			if code := nlenc.Int32(m0.Data[0:4]); code != 0 {
				log.Println("Receive NL err ", c, syscall.Errno(-1*int(code)),
					nmsgs[0].Header.Sequence)
			}
		} else {

			for _, m := range msgs {
				att, err := netlink.UnmarshalAttributes(m.Data)
				if err != nil {
					log.Println("Error parsing attributes ", err)
					continue
				}

				if m.Header.Command == nl80211.CmdNotifyCqm {
					continue
				}

				c := cmdTable[uint16(m.Header.Command)]
				if c == "" {
					c = strconv.Itoa(int(m.Header.Command))
				}
				var intf uint32
				wiphy := 0
				wdev := 0
				freq := 0
				rxSig := 0
				var duration uint32
				var framePL []byte
				for _, a := range att {
					aname := attrTable[a.Type]
					switch a.Type {
					case nl80211.AttrFrame:
						framePL = a.Data
					case nl80211.AttrIfindex:
						intf = binary.LittleEndian.Uint32(a.Data)
					case nl80211.AttrWdev:
						wdev = int(binary.LittleEndian.Uint64(a.Data))
					case nl80211.AttrWiphy:
						wiphy = int(binary.LittleEndian.Uint32(a.Data))
					case nl80211.AttrWiphyFreq: // 38
						freq = int(binary.LittleEndian.Uint32(a.Data))
					case nl80211.AttrRxSignalDbm:
						rxSig = int(int32(binary.LittleEndian.Uint32(a.Data)))
					case nl80211.AttrCookie:
					case nl80211.AttrWiphyChannelType: //39:
						// int32, 0
					case nl80211.AttrDuration: // 87:
						duration = binary.LittleEndian.Uint32(a.Data)
					case nl80211.AttrAck:
						// bool - for send frame
						continue
					default:
						log.Println("Received ", c, aname, a.Type, a.Data, freq,
							sinceStart)
					}
				}
				// 1 (wiphy), 3 (ifidx), 153 (wdev), 38(freq), 151(rxsignal)
				switch m.Header.Command {
				case nl80211.CmdFrameTxStatus:
					nani := NanClients[intf]
					if nani != nil {
						// Typical when connected on different band:
						//  <2m sent, ~50ms to ack (likely due to switching band
						// at the right time). 200ms is common too.
						sinceSent := time.Now().Sub(nani.LastSent)
						if nani.LastSentTime.Milliseconds() > 5 ||
							sinceSent.Milliseconds() > 300 {
							log.Println("TX: ", nani.IFace.Name,
								sinceStart, "tAck", sinceSent,
								"tSend", nani.LastSentTime)
						}

						nani.LastSent = time.Time{}
					} else {
						log.Println("TX: no client", intf, sinceStart)
					}
					continue

				}

				if m.Header.Command == nl80211.CmdRemainOnChannel {
					// in wpa_supplicant: wpas_p2p_remain_on_channel_cb, offchannel_remain_on_channel_cb
					rocStartEv = time.Now()
					rocFreq = freq
					rocDuration = duration
					continue
				}
				if m.Header.Command == nl80211.CmdCancelRemainOnChannel {
					log.Println("ROC: if ", intf, "freq", freq, "duration", duration,
						"ts", sinceStart, "startTime",
						rocStartEv.Sub(rocStart), "sinceStart", time.Since(rocStartEv))

					continue
				}
				if m.Header.Command == nl80211.CmdFrame {
					if intf != 0 {
						log.Println("IN FRAME: ts=", sinceStart,
							"if=", intf, wiphy, wdev,
							"f=", freq, rxSig, "\n"+
								hex.Dump(framePL))
					}
					// 0 - just the echo
				} else if (m.Header.Command == nl80211.CmdFrameWaitCancel) {
					nani := NanClients[intf]
						log.Println("TXE: ", nani.IFace.Name,
							sinceStart,
							"tSend", nani.LastSentTime)

				} else {
					log.Println("CMD: ", c, intf, wiphy, wdev, sinceStart)
				}
			}
		}
	}
}

// RegisterFrame will ask netlink to deliver frames matching a pattern.
// Note that wpa_supplicant also registers for frames - and may prevent
// us from getting registered.
//
// This must be called on the receiving connection (cr), before the read
// loop.
func (c *Client) RegisterFrame(ifi *Interface, t uint16, match []byte) error {
	b, err := netlink.MarshalAttributes([]netlink.Attribute{
		{
			Type: nl80211.AttrIfindex,
			Data: nlenc.Uint32Bytes(uint32(ifi.Index)),
		},
		{
			Type: nl80211.AttrWdev,
			Data: nlenc.Uint64Bytes(uint64(ifi.Device)),
		},
		{ // Default: MGMT | ACTION
			Type: nl80211.AttrFrameType,
			Data: nlenc.Uint16Bytes(t),
		},
		{
			Type: nl80211.AttrFrameMatch, // 5B
			Data: match,
			// Operation alredy in progress without 0x13
			//Data: []byte{0x04},
		},
	})

	//\x3a\x00\x00\x00\
	// x08\x00\
	// \x03\x00
	// \x02\x00\
	// /x00\x00

	// \x06\x00
	// \x65\x00\x40\x00\x00\x00
	// \x06\x00
	// \x5b\x00\x01\x02\x00\x00
	req := genetlink.Message{
		Header: genetlink.Header{
			Command: nl80211.CmdRegisterFrame,
			Version: c.familyVersion,
		},
		Data: b,
	}

	flags := netlink.Request
	msg, err := c.cr.Send(req, c.familyID, flags)
	if err != nil {
		log.Println("Execute error", err)
		return err
	}
	log.Println("Register mgmt ", ifi.Name, msg.Header.Sequence, t, match)

	return nil
}

// Tracks 'remain on channel' timing.
var rocStart time.Time
var rocStartEv time.Time
var rocDuration uint32
var rocFreq int

func (c *Client) RemainOnChannel(ifi *Interface, freq, dur int) error {
	b, err := netlink.MarshalAttributes([]netlink.Attribute{
		{
			Type: nl80211.AttrIfindex,
			Data: nlenc.Uint32Bytes(uint32(ifi.Index)),
		},
		{
			Type: nl80211.AttrWdev,
			Data: nlenc.Uint64Bytes(uint64(ifi.Device)),
		},
		{Type: nl80211.AttrWiphyFreq,
			Data: nlenc.Uint32Bytes(uint32(freq)),
		},
		{
			Type: nl80211.AttrDuration,
			Data: nlenc.Uint32Bytes(uint32(dur)), // ms
		},
		// It means: NL should not send ACK, not that wifi shouldn't.
		{
			Type: nl80211.AttrDontWaitForAck, // flag
		},
	})

	req := genetlink.Message{
		Header: genetlink.Header{
			Command: nl80211.CmdRemainOnChannel,
			Version: c.familyVersion,
		},
		Data: b,
	}

	flags := netlink.Request | netlink.Echo
	msgs, err := c.c.Send(req, c.familyID, flags)
	if err != nil {
		log.Println("Execute error", err, ifi.Name)
		return err
	}
	rocStart = time.Now()
	if false {
		log.Println("RemainOnChannel", ifi.Name, msgs.Header.Sequence)
	}

	return nil
}

var outBuf = make([]byte, 4096)

// SendFrameRaw sends a raw frame, starting with 802.11 type/subtype
// Will fill in this station hardware address
func (c *Nan) SendFrameRaw(ifi *Interface, outBuf []byte,
	freq, dwelltime int) error {
	copy(outBuf[10:], ifi.HardwareAddr)

	b, err := netlink.MarshalAttributes([]netlink.Attribute{
		{
			Type: nl80211.AttrIfindex,
			Data: nlenc.Uint32Bytes(uint32(ifi.Index)),
		},
		{
			Type: nl80211.AttrWdev,
			Data: nlenc.Uint64Bytes(uint64(ifi.Device)),
		},
		{ // if not set, use the sta freq. Must set the next one as well
			// ch 6: 2437
			// 44: 5220
			// 149 (if possible): 5745
			Type: nl80211.AttrWiphyFreq,
			Data: nlenc.Uint32Bytes(uint32(freq)),
		},
		{ // checks OFFCHAN_TX flag of the interface
			Type: nl80211.AttrOffchannelTxOk, // flag
		},
		{
			Type: nl80211.AttrDuration,
			Data: nlenc.Uint32Bytes(uint32(dwelltime)), // ms
		},
		// It means: NL should not send ACK, not that wifi shouldn't.
		//{
		//	Type: nl80211.AttrDontWaitForAck, // flag
		//},

		// nocckrate, csacoff
		{
			Type: nl80211.AttrFrame,
			Data: outBuf,
		}})

	req := genetlink.Message{
		Header: genetlink.Header{
			Command: nl80211.CmdFrame,
			Version: c.c.familyVersion,
		},
		Data: b,
	}

	// If removing 'AttrDontWaitForAck':
	//2020/02/17 17:44:12 Received  60 Wiphy 1 [0 0 0 0]
	//2020/02/17 17:44:12 Received  60 Ifindex 3 [2 0 0 0]
	//2020/02/17 17:44:12 Received  60 Wdev 153 [1 0 0 0 0 0 0 0]
	//2020/02/17 17:44:12 Received  60 Frame 51 [208 0 0 0 255 255 255 255 255 255 56 186 248 73 211 191 80 111 154 1 217 73 0 0 4 9 80 111 154 19 15 9 1 2 3 4 5 6 7 8 9]
	//2020/02/17 17:44:12 Received  60 Cookie 88 [100 0 0 0 0 0 0 0]
	//2020/02/17 17:44:12 Received  60 Ack 92 []

	t0 := time.Now()
	if !c.LastSent.IsZero() && time.Since(c.LastSent) < 100*time.Millisecond {
		return nil
	}
	c.LastSent = t0
	flags := netlink.Request | netlink.Echo
	msgs, err := c.c.c.Send(req, c.c.familyID, flags)
	if err != nil {
		log.Println("Execute error", err, ifi.Name)
		c.SendErrors++
		return err
	}
	c.LastSentTime = time.Since(t0)
	c.LastSentSeq = msgs.Header.Sequence

	return nil
}

var (
	// Not clear how android handles cluster merging - they seem to converge.
	// I see D9:49 as ID

	// Beacon header
	// 24-bytes MAC header
	// 12B fixed elements
	// 6B NAN IE (vendor IE)

	beaconHead = [72]byte{
		0x80, 0x00, // type/sub = mgmt,beacon
		0x00, 0x00, // duration
		/* 4: DST */
		0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF,
		/* 10: SRC - will be set by SendFrameRaw to this device addr */
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		/* 16 BSSID */
		0x50, 0x6F, 0x9A, 0x01, 0xd9, 0x49, // NAN BSSID
		/* 22: SEQ, FRAG */
		0, 0,

		// FRAME_DATA
		// 24: fixed params. TS - SET TO ZERO on ESP32
		0xcc, 0, 0xc0, 0xb3, 1, 2, 3, 4,
		// 32: beacon interval
		0, 2, // beacon interval = 512
		// 34: capabilities
		0x20, 0x04,

		// 36: tagged info
		0xdd, // vendor specific
		// 37: tag length
		34, // tag length
		0x50, 0x6F, 0x9A,
		0x13, // NAN

		// Fixed beacon IEs for DMesh

		// 42: Master attribute - android uses 1, ESP will use 81 (infra, high)
		// to save android bat life.
		// Linux will use 0xF0 - very high since it can't act as a non-master
		// if it's also in STA mode, no way to listen at fixed intervals.
		0, 2, 0,
		140, 0xFE,

		// 47: Cluster attribute
		1, 0x0d, 0,
		// 50 - master MAC, rand, pref
		0, 0, 0, 0, 0, 0, 0xFE, 140,
		// 58 - hops to master
		0,
		// 59 - time delta
		0, 0, 0, 0,

		// 63: Service ID list
		2, 6, 0,
		0x75, 0x94, 0x31, 0x93, 0xea, 0xc9,
		// 72
	}

	// Frame header for NAN discovery - 24 bytes MAC plus 6 nan tag
	nanHead = []byte{
		0xD0, 0x00, // type/sub = mgmt, action
		0x00, 0x00, // duration
		/* 4: DST - will be filled with specific MAC in some cases */
		//0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF,
		// According to std - should be this, it seems broadcast works too
		// According to std - should be this, it seems broadcast works too
		0x51, 0x6F, 0x9A, 0x01, 0x00, 0x00,
		/* 10: SRC */
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, // will be set to this device addr
		/* 16 BSSID */
		0x50, 0x6F, 0x9A, 0x01, 0xd9, 0x49, // NAN BSSID
		/* 22: SEQ, FRAG */
		0, 0,

		// 24: DATA, Action (vendor, NAN, Discovery)
		0x04, 0x09, // action, vendor // 26
		// 26: Wifi vendor ID, NAN frame tag
		0x50, 0x6F, 0x9A, 0x13,
		// 30:
	}

	nanDeviceCap = []byte{
		0x0F, 0x09, 0x00,
		0, 1, 0, // only 2.5GHz
		// bands
		0x04, 1, 0, 0, 0x14, 0,
	}

	nanAvail = []byte{
		0x12, 0x1b, 0x00,
		0x0b, 0x01, 0x00, 0x16, 0x00, 0x1a, 0x10, 0x18, 0x00, 0x04, 0xfe,
		0xff, 0xff, 0x3f, 0x31, 0x51, 0xff, 0x07, 0x00, 0x80, 0x20, 0x00, 0x0f, 0x80, 0x01, 0x00, 0x0f,
	}

	myNanSvcId = byte(1)

	nanServiceExtension = []byte{
		0x0e, 0x04, 0x00,
		myNanSvcId, 0x00, 0x02, 0x02,
	}

	nanServiceDescriptor = []byte{
		// 0:
		0x03,
		0x1A, 0x00,
		// 3: DMesh Service ID
		0x75, 0x94, 0x31, 0x93, 0xea, 0xc9,
		// 9: InstanceID
		myNanSvcId,
		// 10: requestor ID
		0, // extracted from Sub request
		// 91: control, SI present
		0x10, // 0x10 for publish, 0x11 for subscribe type
		// 12: len
		0x00,
		// 13: Data, max 255 bytes

	}
)

// Send NAN beacon frame
func (c *Nan) SendBeacon(syncFrame bool) error {
	c.m.Lock()
	defer c.m.Unlock()
	if !syncFrame {
		beaconHead[32] = 128
		beaconHead[33] = 0
	}
	// Timestamp, ms
	binary.LittleEndian.PutUint64(beaconHead[24:], uint64(time.Now().Unix()))
	// Master MAC = self
	copy(beaconHead[50:], c.IFace.HardwareAddr)

	freq := 2437
	c.SendFrameRaw(c.IFace, beaconHead[:], freq, 20)
	c.SendDiscovery(c.IFace, []byte{1}, 20)
	return nil
}

// Send NAN Publish or Subscribe frame
func (c *Nan) SendDiscovery(ifi *Interface, sdu []byte, dwelltime int) error {

	off := 0
	copy(outBuf, nanHead)

	off += len(nanHead)

	copy(outBuf[off:], nanDeviceCap)
	off += len(nanDeviceCap)

	copy(outBuf[off:], nanAvail)
	off += len(nanAvail)

	copy(outBuf[off:], nanServiceExtension)
	off += len(nanServiceExtension)

	copy(outBuf[off:], nanServiceDescriptor)

	tl := len(sdu)
	// Service data len
	outBuf[off+12] = byte(tl)
	totalAtt := tl + len(nanServiceDescriptor) - 3
	outBuf[off+1] = byte(totalAtt % 256)
	outBuf[off+2] = byte(totalAtt / 256)

	off += len(nanServiceDescriptor)

	copy(outBuf[off:], sdu)
	off += len(sdu)

	freq := 2437

	c.SendFrameRaw(ifi, outBuf[0:off], freq, dwelltime)
	return nil
}

// Send data using NAN "FollowUp" function, in a SDF
//
func (c *Nan) SendFollowup(to []byte, toPort byte, freq int, sdu []byte) error {
	data := []byte{
		0xD0, 0x00, // type/sub
		0x00, 0x00, // duration
		/* 4: DST */
		0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF,
		//0x51, 0x6F, 0x9A, 0x01, 0x00, 0x00,
		/* 10: SRC */
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, // will be set to this device addr
		/* 16 BSSID */
		0x50, 0x6F, 0x9A, 0x01, 0xd9, 0x49, // NAN BSSID
		/* 22: SEQ, FRAG */
		0, 0,

		// 24: DATA, Action (vendor, NAN, Discovery)
		0x04, 0x09, // action, vendor // 26
		0x50, 0x6F, 0x9A, 0x13, // NAN // 30

		// 30: NAN ATT - follow up
		0x03, 0x0E, 0x00,
		// 33: payload
		// DMesh Service ID
		0x75, 0x94, 0x31, 0x93, 0xea, 0xc9,
		// 39 Instance ID
		0x80,
		// 40 Requestor Instance ID
		0x80,
		// 41 control
		0x12,
		// 42 len - short
		0x04,
	}
	copy(outBuf, data)

	tl := len(sdu) + 10 // 6B service id, 2B iid, 1B control, 1B len
	outBuf[31] = byte(tl % 256)
	outBuf[32] = byte(tl / 256)

	outBuf[42] = byte(len(sdu))
	copy(outBuf[43:], sdu)

	c.SendFrameRaw(c.IFace, outBuf[0:len(sdu)+43], freq, dwelltime)
	return nil
}

// WIP
func (c *Client) NewMon(phy int) error {
	//%NL80211_ATTR_WIPHY, %NL80211_ATTR_IFTYPE and
	//*	%NL80211_ATTR_IFNAME.
	b, err := netlink.MarshalAttributes([]netlink.Attribute{
		{
			Type: nl80211.AttrIfname,
			Data: []byte("dmeshmon\x00"),
		},
		{
			Type: nl80211.AttrWiphy,
			Data: nlenc.Uint32Bytes(uint32(phy)),
		},
		{
			Type: nl80211.AttrIftype,
			Data: nlenc.Uint32Bytes(uint32(InterfaceTypeMonitor)),
		},
	})

	// Ask nl80211 to dump a list of all WiFi interfaces
	req := genetlink.Message{
		Header: genetlink.Header{
			Command: nl80211.CmdNewInterface,
			Version: c.familyVersion,
		},
		Data: b,
	}
	//Has %NL80211_ATTR_IFINDEX,
	//		*	%NL80211_ATTR_WIPHY and %NL80211_ATTR_IFTYPE attributes. Can also
	//*	be sent from userspace to request creation of a new virtual interface,
	//*	then requires attributes %NL80211_ATTR_WIPHY, %NL80211_ATTR_IFTYPE and
	//*	%NL80211_ATTR_IFNAME.
	//
	flags := netlink.Request
	msgs, err := c.c.Execute(req, c.familyID, flags)
	if err != nil {
		return err
	}

	if err := c.checkMessages(msgs, nl80211.CmdNewInterface); err != nil {
		return err
	}

	log.Println("NEW INTERFACE ", msgs)

	return nil
}

type Phy struct {
	// The index of the interface.
	Index int

	// The name of the interface.
	Name string

	// The hardware address of the interface.
	HardwareAddr net.HardwareAddr

	// The physical device that this interface belongs to.
	PHY int

	// The virtual device number of this interface within a PHY.
	Device int

	// The operating mode of the interface.
	Type InterfaceType

	// The interface's wireless frequency in MHz.
	Frequency int
}

// Call 'CMD_GET_WIPHY' to list phy interfaces.
func (c *Client) Phys() ([]*Phy, error) {
	// Ask nl80211 to dump a list of all WiFi interfaces
	req := genetlink.Message{
		Header: genetlink.Header{
			Command: nl80211.CmdGetWiphy,
			Version: c.familyVersion,
		},
	}

	flags := netlink.Request | netlink.Dump
	msgs, err := c.c.Execute(req, c.familyID, flags)
	if err != nil {
		return nil, err
	}

	if err := c.checkMessages(msgs, nl80211.CmdNewWiphy); err != nil {
		return nil, err
	}

	phys := []*Phy{}
	for _, m := range msgs {
		attrs, err := netlink.UnmarshalAttributes(m.Data)
		if err != nil {
			return nil, err
		}

		var ifi Phy
		if err := (&ifi).parsePhys(attrs); err != nil {
			return nil, err
		}

		phys = append(phys, &ifi)

		log.Println("PHY: ", attrs)
	}

	return phys, nil
}

func ParseATTs(b []byte) ([]IE, error) {
	var ies []IE
	var i int
	for {
		if len(b[i:]) == 0 {
			break
		}
		if len(b[i:]) < 3 {
			return nil, errInvalidIE
		}

		id := b[i]
		i++
		l := int(uint8(b[i]))
		i++
		l = l + 256*int(uint8(b[i]))
		i++

		if len(b[i:]) < l {
			return nil, errInvalidIE
		}

		ies = append(ies, IE{
			ID:   id,
			Data: b[i : i+l],
		})

		i += l
	}

	return ies, nil
}
