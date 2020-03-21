//+build linux

package wifi

import (
	"bytes"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"runtime"
	"strconv"
	"sync"
	"time"
	"unicode/utf8"

	//"github.com/costinm/dmesh-l2/pkg/l2/genetlink"
	//"github.com/costinm/dmesh-l2/pkg/l2/netlink"
	"github.com/costinm/dmesh-l2/pkg/l2/nl80211"
	"github.com/mdlayher/genetlink"
	"github.com/mdlayher/netlink"
	"github.com/mdlayher/netlink/nlenc"
)

//

// Errors which may occur when interacting with generic netlink.
var (
	errInvalidCommand       = errors.New("invalid generic netlink response command")
	errInvalidFamilyVersion = errors.New("invalid generic netlink response family version")
)

var (
	// errUnimplemented is returned by all functions on platforms that
	// do not have package wifi implemented.
	errUnimplemented = fmt.Errorf("package wifi not implemented on %s/%s",
		runtime.GOOS, runtime.GOARCH)
)

var (
	attrTable map[uint16]string
	cmdTable  map[uint16]string
)

var dwelltime = 0

func init() {
	// 148 ms - get 2 messages
	// 150 - gets all
	// TODO: where does it spend 147 ms ?? And is it ms ?
	dwelltime, _ = strconv.Atoi(os.Getenv("NAN_STAY"))
	if dwelltime == 0 {
		// 30 - usually shorter times
		dwelltime = 30
	}

	attrTable = map[uint16]string{}
	attrTable[1] = "Wiphy"
	attrTable[3] = "Ifindex"
	attrTable[153] = "Wdev"
	attrTable[51] = "Frame"
	attrTable[88] = "Cookie"
	attrTable[92] = "Ack"
	attrTable[44] = "ScanFrequencies"
	attrTable[45] = "ScanSsids"

	cmdTable = map[uint16]string{}
	cmdTable[33] = "TriggerScan"
	cmdTable[34] = "NewScanResults"
	cmdTable[55] = "RemainOnChannel"
	cmdTable[56] = "CancelRemainOnChannel"
	cmdTable[59] = "Frame"
}

var (
	startTime = time.Now().UnixNano() / 1000000
)

// New creates a new Client.
func New() (*Client, error) {
	c, err := newClient()
	if err != nil {
		return nil, err
	}

	return c, nil
}

// newClient dials a generic netlink connection and verifies that nl80211
// is available for use by this package.
func newClient() (*Client, error) {

	c, err := genetlink.Dial(&netlink.Config{
		DisableNSLockThread: true,
	})
	if err != nil {
		return nil, err
	}

	family, err := c.GetFamily(nl80211.GenlName)
	if err != nil {
		// Ensure the genl socket is closed on error to avoid leaking file
		// descriptors.
		_ = c.Close()
		return nil, err
	}

	// Client for reading - due to linking
	cr, err := genetlink.Dial(&netlink.Config{
		DisableNSLockThread: true,
	})
	if err != nil {
		return nil, err
	}

	return &Client{
		c: c,
		//cr:             c,
		cr:            cr,
		familyID:      family.ID,
		familyVersion: family.Version,
		family:        family,
	}, nil
}

// Close releases resources used by a Client.
func (c *Client) Scan() error {
	req := genetlink.Message{
		Header: genetlink.Header{
			Command: nl80211.CmdGetScan,
			Version: c.familyVersion,
		},
	}

	flags := netlink.Request | netlink.Dump
	msgs, err := c.c.Execute(req, c.familyID, flags)
	if err != nil {
		return err
	}

	if err := c.checkMessages(msgs, nl80211.CmdNewInterface); err != nil {
		return err
	}

	return nil
}

/*
- RegisterFrame: interface idx, frame type (default: action frame), match
  for first few bytes (1 or cat, 4 for vendor)

*/

// A client is the Linux implementation of osClient, which makes use of
// netlink, generic netlink, and nl80211 to provide access to WiFi device
// actions and statistics.
type Client struct {
	// Old style - doesn't have good reading
	c *genetlink.Conn

	familyID      uint16
	familyVersion uint8
	family        genetlink.Family
	cr            *genetlink.Conn
}

var NanClients = map[uint32]*Nan{}

type Nan struct {
	IFace *Interface
	c     *Client

	m sync.Mutex

	SendErrors   int
	LastSent     time.Time
	LastSentTime time.Duration
	LastSentSeq  uint32
}

func NewNan(c *Client, i *Interface) *Nan {
	n := &Nan{IFace: i, c: c}
	NanClients[uint32(i.Index)] = n
	return n
}

// Close closes the client's generic netlink connection.
func (c *Client) Close() error {
	return c.c.Close()
}

func (c *Client) Conn() *genetlink.Conn {
	return c.c
}

// Interfaces requests that nl80211 return a list of all WiFi interfaces present
// on this system.
func (c *Client) Interfaces() ([]*Interface, error) {
	// Ask nl80211 to dump a list of all WiFi interfaces
	req := genetlink.Message{
		Header: genetlink.Header{
			Command: nl80211.CmdGetInterface,
			Version: c.familyVersion,
		},
	}

	flags := netlink.Request | netlink.Dump
	msgs, err := c.c.Execute(req, c.familyID, flags)
	if err != nil {
		return nil, err
	}

	if err := c.checkMessages(msgs, nl80211.CmdNewInterface); err != nil {
		return nil, err
	}

	ifis := make([]*Interface, 0, len(msgs))
	for _, m := range msgs {
		attrs, err := netlink.UnmarshalAttributes(m.Data)
		if err != nil {
			return nil, err
		}

		var ifi Interface
		if err := (&ifi).parseAttributes(attrs); err != nil {
			return nil, err
		}

		ifis = append(ifis, &ifi)
	}

	return ifis, nil
}

// BSS requests that nl80211 return the BSS for the specified Interface.
func (c *Client) BSS(ifi *Interface) (*BSS, error) {
	b, err := netlink.MarshalAttributes(ifi.idAttrs())
	if err != nil {
		return nil, err
	}

	// Ask nl80211 to retrieve BSS information for the interface specified
	// by its attributes
	req := genetlink.Message{
		Header: genetlink.Header{
			Command: nl80211.CmdGetScan,
			Version: c.familyVersion,
		},
		Data: b,
	}

	flags := netlink.Request | netlink.Dump
	msgs, err := c.c.Execute(req, c.familyID, flags)
	if err != nil {
		return nil, err
	}

	if err := c.checkMessages(msgs, nl80211.CmdNewScanResults); err != nil {
		return nil, err
	}

	return parseBSS(msgs)
}

// StationInfo requests that nl80211 return all station info for the specified
// Interface.
func (c *Client) StationInfo(ifi *Interface) ([]*StationInfo, error) {
	b, err := netlink.MarshalAttributes(ifi.idAttrs())
	if err != nil {
		return nil, err
	}

	// Ask nl80211 to retrieve station info for the interface specified
	// by its attributes
	req := genetlink.Message{
		Header: genetlink.Header{
			// From nl80211.h:
			//  * @NL80211_CMD_GET_STATION: Get station attributes for station identified by
			//  * %NL80211_ATTR_MAC on the interface identified by %NL80211_ATTR_IFINDEX.
			Command: nl80211.CmdGetStation,
			Version: c.familyVersion,
		},
		Data: b,
	}

	flags := netlink.Request | netlink.Dump
	msgs, err := c.c.Execute(req, c.familyID, flags)
	if err != nil {
		return nil, err
	}

	if len(msgs) == 0 {
		return nil, os.ErrNotExist
	}

	stations := make([]*StationInfo, len(msgs))
	for i := range msgs {
		if err := c.checkMessages(msgs, nl80211.CmdNewStation); err != nil {
			return nil, err
		}

		if stations[i], err = parseStationInfo(msgs[i].Data); err != nil {
			return nil, err
		}
	}

	return stations, nil
}

// checkMessages verifies that response messages from generic netlink contain
// the command and family version we expect.
func (c *Client) checkMessages(msgs []genetlink.Message, command uint8) error {
	for _, m := range msgs {
		if m.Header.Command != command {
			return errInvalidCommand
		}

		if m.Header.Version != c.familyVersion {
			return errInvalidFamilyVersion
		}
	}

	return nil
}

// idAttrs returns the netlink attributes required from an Interface to retrieve
// more data about it.
func (ifi *Interface) idAttrs() []netlink.Attribute {
	return []netlink.Attribute{
		{
			Type: nl80211.AttrIfindex,
			Data: nlenc.Uint32Bytes(uint32(ifi.Index)),
		},
		{
			Type: nl80211.AttrMac,
			Data: ifi.HardwareAddr,
		},
	}
}

// parseAttributes parses netlink attributes into an Interface's fields.
func (ifi *Interface) parseAttributes(attrs []netlink.Attribute) error {
	for _, a := range attrs {
		switch a.Type {
		case nl80211.AttrIfindex:
			ifi.Index = int(nlenc.Uint32(a.Data))
		case nl80211.AttrIfname:
			ifi.Name = nlenc.String(a.Data)
		case nl80211.AttrMac:
			ifi.HardwareAddr = net.HardwareAddr(a.Data)
		case nl80211.AttrWiphy:
			ifi.PHY = int(nlenc.Uint32(a.Data))
		case nl80211.AttrIftype:
			// NOTE: InterfaceType copies the ordering of nl80211's interface type
			// constants.  This may not be the case on other operating systems.
			ifi.Type = InterfaceType(nlenc.Uint32(a.Data))
		case nl80211.AttrWdev:
			ifi.Device = int(nlenc.Uint64(a.Data))
		case nl80211.AttrWiphyFreq:
			ifi.Frequency = int(nlenc.Uint32(a.Data))
		case nl80211.AttrWiphyTxPowerLevel:
		case nl80211.AttrGeneration:

		default:
			log.Println("interface attribute ", a.Type, a.Data)
		}

	}

	return nil
}

// parseAttributes parses netlink attributes into an Interface's fields.
func (ifi *Phy) parsePhys(attrs []netlink.Attribute) error {
	/*
		2020/03/17 12:34:39 interface attribute  61 [2]
		2020/03/17 12:34:39 interface attribute  62 [2]
		2020/03/17 12:34:39 interface attribute  63 [255 255 255 255]
		2020/03/17 12:34:39 interface attribute  64 [255 255 255 255]
		2020/03/17 12:34:39 interface attribute  89 [0]
		2020/03/17 12:34:39 interface attribute  43 [4]
		2020/03/17 12:34:39 interface attribute  123 [0]
		2020/03/17 12:34:39 interface attribute  56 [209 8]
		2020/03/17 12:34:39 interface attribute  124 [0 0]
		2020/03/17 12:34:39 interface attribute  133 [0]
		2020/03/17 12:34:39 interface attribute  222 [1 0 0 0]
		2020/03/17 12:34:39 interface attribute  223 [255 255 255 255]
		2020/03/17 12:34:39 interface attribute  224 [0 0 0 0]
		2020/03/17 12:34:39 interface attribute  104 []
		2020/03/17 12:34:39 interface attribute  115 []
		2020/03/17 12:34:39 interface attribute  57 [1 172 15 0 5 172 15 0 2 172 15 0 4 172 15 0 10 172 15 0 8 172 15 0 9 172 15 0]
		2020/03/17 12:34:39 interface attribute  86 [0]
		2020/03/17 12:34:39 interface attribute  102 []
		2020/03/17 12:34:39 interface attribute  113 [0 0 0 0]
		2020/03/17 12:34:39 interface attribute  114 [0 0 0 0]
		2020/03/17 12:34:39 interface attribute  32 [4 0 1 0 4 0 2 0 4 0 3 0 4 0 4 0 4 0 6 0 4 0 7 0]
		2020/03/17 12:34:39 interface attribute  22 [4 2 0 0 20 0 3 0 255 0 0 0 1 0 0 0 0 0 0 0 1 0 0 0 6 0 4 0 126 1 0 0 5 0 5 0 2 0 0 0 5 0 6 0 4 0 0 0 160 0 2 0 12 0 0 0 8 0 1 0 10 0 0 0 16 0 1 0 8 0 1 0 20 0 0 0 4 0 2 0 16 0 2 0 8 0 1 0 55 0 0 0 4 0 2 0 16 0 3 0 8 0 1 0 110 0 0 0 4 0 2 0 12 0 4 0 8 0 1 0 60 0 0 0 12 0 5 0 8 0 1 0 90 0 0 0 12 0 6 0 8 0 1 0 120 0 0 0 12 0 7 0 8 0 1 0 180 0 0 0 12 0 8 0 8 0 1 0 240 0 0 0 12 0 9 0 8 0 1 0 104 1 0 0 12 0 10 0 8 0 1 0 224 1 0 0 12 0 11 0 8 0 1 0 28 2 0 0 52 1 1 0 20 0 0 0 8 0 1 0 108 9 0 0 8 0 6 0 208 7 0 0 20 0 1 0 8 0 1 0 113 9 0 0 8 0 6 0 208 7 0 0 20 0 2 0 8 0 1 0 118 9 0 0 8 0 6 0 208 7 0 0 20 0 3 0 8 0 1 0 123 9 0 0 8 0 6 0 208 7 0 0 20 0 4 0 8 0 1 0 128 9 0 0 8 0 6 0 208 7 0 0 20 0 5 0 8 0 1 0 133 9 0 0 8 0 6 0 208 7 0 0 20 0 6 0 8 0 1 0 138 9 0 0 8 0 6 0 208 7 0 0 20 0 7 0 8 0 1 0 143 9 0 0 8 0 6 0 208 7 0 0 20 0 8 0 8 0 1 0 148 9 0 0 8 0 6 0 208 7 0 0 20 0 9 0 8 0 1 0 153 9 0 0 8 0 6 0 208 7 0 0 20 0 10 0 8 0 1 0 158 9 0 0 8 0 6 0 208 7 0 0 28 0 11 0 8 0 1 0 163 9 0 0 4 0 3 0 4 0 4 0 8 0 6 0 208 7 0 0 28 0 12 0 8 0 1 0 168 9 0 0 4 0 3 0 4 0 4 0 8 0 6 0 208 7 0 0 28 0 13 0 8 0 1 0 180 9 0 0 4 0 3 0 4 0 4 0 8 0 6 0 208 7 0 0]
		2020/03/17 12:34:39 interface attribute  50 [8 0 1 0 7 0 0 0 8 0 2 0 6 0 0 0 8 0 3 0 11 0 0 0 8 0 4 0 15 0 0 0 8 0 5 0 19 0 0 0 8 0 6 0 23 0 0 0 8 0 7 0 29 0 0 0 8 0 8 0 25 0 0 0 8 0 9 0 37 0 0 0 8 0 10 0 38 0 0 0 8 0 11 0 39 0 0 0 8 0 12 0 40 0 0 0 8 0 13 0 43 0 0 0 8 0 14 0 68 0 0 0 8 0 15 0 57 0 0 0 8 0 16 0 59 0 0 0 8 0 17 0 67 0 0 0 8 0 18 0 49 0 0 0 8 0 19 0 65 0 0 0 8 0 20 0 66 0 0 0 8 0 21 0 84 0 0 0 8 0 22 0 87 0 0 0 8 0 23 0 85 0 0 0 8 0 24 0 89 0 0 0 8 0 25 0 92 0 0 0 8 0 26 0 46 0 0 0 8 0 27 0 48 0 0 0]
		2020/03/17 12:34:39 interface attribute  108 []
		2020/03/17 12:34:39 interface attribute  99 [4 0 0 0 132 0 1 0 6 0 101 0 0 0 0 0 6 0 101 0 16 0 0 0 6 0 101 0 32 0 0 0 6 0 101 0 48 0 0 0 6 0 101 0 64 0 0 0 6 0 101 0 80 0 0 0 6 0 101 0 96 0 0 0 6 0 101 0 112 0 0 0 6 0 101 0 128 0 0 0 6 0 101 0 144 0 0 0 6 0 101 0 160 0 0 0 6 0 101 0 176 0 0 0 6 0 101 0 192 0 0 0 6 0 101 0 208 0 0 0 6 0 101 0 224 0 0 0 6 0 101 0 240 0 0 0 132 0 2 0 6 0 101 0 0 0 0 0 6 0 101 0 16 0 0 0 6 0 101 0 32 0 0 0 6 0 101 0 48 0 0 0 6 0 101 0 64 0 0 0 6 0 101 0 80 0 0 0 6 0 101 0 96 0 0 0 6 0 101 0 112 0 0 0 6 0 101 0 128 0 0 0 6 0 101 0 144 0 0 0 6 0 101 0 160 0 0 0 6 0 101 0 176 0 0 0 6 0 101 0 192 0 0 0 6 0 101 0 208 0 0 0 6 0 101 0 224 0 0 0 6 0 101 0 240 0 0 0 132 0 3 0 6 0 101 0 0 0 0 0 6 0 101 0 16 0 0 0 6 0 101 0 32 0 0 0 6 0 101 0 48 0 0 0 6 0 101 0 64 0 0 0 6 0 101 0 80 0 0 0 6 0 101 0 96 0 0 0 6 0 101 0 112 0 0 0 6 0 101 0 128 0 0 0 6 0 101 0 144 0 0 0 6 0 101 0 160 0 0 0 6 0 101 0 176 0 0 0 6 0 101 0 192 0 0 0 6 0 101 0 208 0 0 0 6 0 101 0 224 0 0 0 6 0 101 0 240 0 0 0 132 0 4 0 6 0 101 0 0 0 0 0 6 0 101 0 16 0 0 0 6 0 101 0 32 0 0 0 6 0 101 0 48 0 0 0 6 0 101 0 64 0 0 0 6 0 101 0 80 0 0 0 6 0 101 0 96 0 0 0 6 0 101 0 112 0 0 0 6 0 101 0 128 0 0 0 6 0 101 0 144 0 0 0 6 0 101 0 160 0 0 0 6 0 101 0 176 0 0 0 6 0 101 0 192 0 0 0 6 0 101 0 208 0 0 0 6 0 101 0 224 0 0 0 6 0 101 0 240 0 0 0 4 0 5 0 4 0 6 0 132 0 7 0 6 0 101 0 0 0 0 0 6 0 101 0 16 0 0 0 6 0 101 0 32 0 0 0 6 0 101 0 48 0 0 0 6 0 101 0 64 0 0 0 6 0 101 0 80 0 0 0 6 0 101 0 96 0 0 0 6 0 101 0 112 0 0 0 6 0 101 0 128 0 0 0 6 0 101 0 144 0 0 0 6 0 101 0 160 0 0 0 6 0 101 0 176 0 0 0 6 0 101 0 192 0 0 0 6 0 101 0 208 0 0 0 6 0 101 0 224 0 0 0 6 0 101 0 240 0 0 0 132 0 8 0 6 0 101 0 0 0 0 0 6 0 101 0 16 0 0 0 6 0 101 0 32 0 0 0 6 0 101 0 48 0 0 0 6 0 101 0 64 0 0 0 6 0 101 0 80 0 0 0 6 0 101 0 96 0 0 0 6 0 101 0 112 0 0 0 6 0 101 0 128 0 0 0 6 0 101 0 144 0 0 0 6 0 101 0 160 0 0 0 6 0 101 0 176 0 0 0 6 0 101 0 192 0 0 0 6 0 101 0 208 0 0 0 6 0 101 0 224 0 0 0 6 0 101 0 240 0 0 0 132 0 9 0 6 0 101 0 0 0 0 0 6 0 101 0 16 0 0 0 6 0 101 0 32 0 0 0 6 0 101 0 48 0 0 0 6 0 101 0 64 0 0 0 6 0 101 0 80 0 0 0 6 0 101 0 96 0 0 0 6 0 101 0 112 0 0 0 6 0 101 0 128 0 0 0 6 0 101 0 144 0 0 0 6 0 101 0 160 0 0 0 6 0 101 0 176 0 0 0 6 0 101 0 192 0 0 0 6 0 101 0 208 0 0 0 6 0 101 0 224 0 0 0 6 0 101 0 240 0 0 0 132 0 10 0 6 0 101 0 0 0 0 0 6 0 101 0 16 0 0 0 6 0 101 0 32 0 0 0 6 0 101 0 48 0 0 0 6 0 101 0 64 0 0 0 6 0 101 0 80 0 0 0 6 0 101 0 96 0 0 0 6 0 101 0 112 0 0 0 6 0 101 0 128 0 0 0 6 0 101 0 144 0 0 0 6 0 101 0 160 0 0 0 6 0 101 0 176 0 0 0 6 0 101 0 192 0 0 0 6 0 101 0 208 0 0 0 6 0 101 0 224 0 0 0 6 0 101 0 240 0 0 0 4 0 11 0 4 0 12 0]
		2020/03/17 12:34:39 interface attribute  100 [4 0 0 0 36 0 1 0 6 0 101 0 64 0 0 0 6 0 101 0 176 0 0 0 6 0 101 0 192 0 0 0 6 0 101 0 208 0 0 0 20 0 2 0 6 0 101 0 64 0 0 0 6 0 101 0 208 0 0 0 60 0 3 0 6 0 101 0 0 0 0 0 6 0 101 0 32 0 0 0 6 0 101 0 64 0 0 0 6 0 101 0 160 0 0 0 6 0 101 0 176 0 0 0 6 0 101 0 192 0 0 0 6 0 101 0 208 0 0 0 60 0 4 0 6 0 101 0 0 0 0 0 6 0 101 0 32 0 0 0 6 0 101 0 64 0 0 0 6 0 101 0 160 0 0 0 6 0 101 0 176 0 0 0 6 0 101 0 192 0 0 0 6 0 101 0 208 0 0 0 4 0 5 0 4 0 6 0 28 0 7 0 6 0 101 0 176 0 0 0 6 0 101 0 192 0 0 0 6 0 101 0 208 0 0 0 20 0 8 0 6 0 101 0 64 0 0 0 6 0 101 0 208 0 0 0 60 0 9 0 6 0 101 0 0 0 0 0 6 0 101 0 32 0 0 0 6 0 101 0 64 0 0 0 6 0 101 0 160 0 0 0 6 0 101 0 176 0 0 0 6 0 101 0 192 0 0 0 6 0 101 0 208 0 0 0 20 0 10 0 6 0 101 0 64 0 0 0 6 0 101 0 208 0 0 0 4 0 11 0 4 0 12 0]
		2020/03/17 12:34:39 interface attribute  121 [4 0 4 0 4 0 6 0]
		2020/03/17 12:34:39 interface attribute  120 [48 0 1 0 28 0 1 0 24 0 1 0 8 0 1 0 8 0 0 0 12 0 2 0 4 0 3 0 4 0 7 0 8 0 4 0 1 0 0 0 8 0 2 0 8 0 0 0]
		2020/03/17 12:34:39 interface attribute  143 [227 131 1 8]
		2020/03/17 12:34:39 interface attribute  148 [227 75 31 255 255 255 255 255 255 255 255 255 255 0 0 0 0 0 0 0 0 0 0 0 0 0]
		2020/03/17 12:34:39 PHY:  [{8 1 [0 0 0 0]} {9 2 [112 104 121 48 0]} {8 46 [1 0 0 0]} {5 61 [2]} {5 62 [2]} {8 63 [255 255 255 255]} {8 64 [255 255 255 255]} {5 89 [0]} {5 43 [4]} {5 123 [0]} {6 56 [209 8]} {6 124 [0 0]} {5 133 [0]} {8 222 [1 0 0 0]} {8 223 [255 255 255 255]} {8 224 [0 0 0 0]} {4 104 []} {4 115 []} {32 57 [1 172 15 0 5 172 15 0 2 172 15 0 4 172 15 0 10 172 15 0 8 172 15 0 9 172 15 0]} {5 86 [0]} {4 102 []} {8 113 [0 0 0 0]} {8 114 [0 0 0 0]} {28 32 [4 0 1 0 4 0 2 0 4 0 3 0 4 0 4 0 4 0 6 0 4 0 7 0]} {520 22 [4 2 0 0 20 0 3 0 255 0 0 0 1 0 0 0 0 0 0 0 1 0 0 0 6 0 4 0 126 1 0 0 5 0 5 0 2 0 0 0 5 0 6 0 4 0 0 0 160 0 2 0 12 0 0 0 8 0 1 0 10 0 0 0 16 0 1 0 8 0 1 0 20 0 0 0 4 0 2 0 16 0 2 0 8 0 1 0 55 0 0 0 4 0 2 0 16 0 3 0 8 0 1 0 110 0 0 0 4 0 2 0 12 0 4 0 8 0 1 0 60 0 0 0 12 0 5 0 8 0 1 0 90 0 0 0 12 0 6 0 8 0 1 0 120 0 0 0 12 0 7 0 8 0 1 0 180 0 0 0 12 0 8 0 8 0 1 0 240 0 0 0 12 0 9 0 8 0 1 0 104 1 0 0 12 0 10 0 8 0 1 0 224 1 0 0 12 0 11 0 8 0 1 0 28 2 0 0 52 1 1 0 20 0 0 0 8 0 1 0 108 9 0 0 8 0 6 0 208 7 0 0 20 0 1 0 8 0 1 0 113 9 0 0 8 0 6 0 208 7 0 0 20 0 2 0 8 0 1 0 118 9 0 0 8 0 6 0 208 7 0 0 20 0 3 0 8 0 1 0 123 9 0 0 8 0 6 0 208 7 0 0 20 0 4 0 8 0 1 0 128 9 0 0 8 0 6 0 208 7 0 0 20 0 5 0 8 0 1 0 133 9 0 0 8 0 6 0 208 7 0 0 20 0 6 0 8 0 1 0 138 9 0 0 8 0 6 0 208 7 0 0 20 0 7 0 8 0 1 0 143 9 0 0 8 0 6 0 208 7 0 0 20 0 8 0 8 0 1 0 148 9 0 0 8 0 6 0 208 7 0 0 20 0 9 0 8 0 1 0 153 9 0 0 8 0 6 0 208 7 0 0 20 0 10 0 8 0 1 0 158 9 0 0 8 0 6 0 208 7 0 0 28 0 11 0 8 0 1 0 163 9 0 0 4 0 3 0 4 0 4 0 8 0 6 0 208 7 0 0 28 0 12 0 8 0 1 0 168 9 0 0 4 0 3 0 4 0 4 0 8 0 6 0 208 7 0 0 28 0 13 0 8 0 1 0 180 9 0 0 4 0 3 0 4 0 4 0 8 0 6 0 208 7 0 0]} {220 50 [8 0 1 0 7 0 0 0 8 0 2 0 6 0 0 0 8 0 3 0 11 0 0 0 8 0 4 0 15 0 0 0 8 0 5 0 19 0 0 0 8 0 6 0 23 0 0 0 8 0 7 0 29 0 0 0 8 0 8 0 25 0 0 0 8 0 9 0 37 0 0 0 8 0 10 0 38 0 0 0 8 0 11 0 39 0 0 0 8 0 12 0 40 0 0 0 8 0 13 0 43 0 0 0 8 0 14 0 68 0 0 0 8 0 15 0 57 0 0 0 8 0 16 0 59 0 0 0 8 0 17 0 67 0 0 0 8 0 18 0 49 0 0 0 8 0 19 0 65 0 0 0 8 0 20 0 66 0 0 0 8 0 21 0 84 0 0 0 8 0 22 0 87 0 0 0 8 0 23 0 85 0 0 0 8 0 24 0 89 0 0 0 8 0 25 0 92 0 0 0 8 0 26 0 46 0 0 0 8 0 27 0 48 0 0 0]} {4 108 []} {1080 99 [4 0 0 0 132 0 1 0 6 0 101 0 0 0 0 0 6 0 101 0 16 0 0 0 6 0 101 0 32 0 0 0 6 0 101 0 48 0 0 0 6 0 101 0 64 0 0 0 6 0 101 0 80 0 0 0 6 0 101 0 96 0 0 0 6 0 101 0 112 0 0 0 6 0 101 0 128 0 0 0 6 0 101 0 144 0 0 0 6 0 101 0 160 0 0 0 6 0 101 0 176 0 0 0 6 0 101 0 192 0 0 0 6 0 101 0 208 0 0 0 6 0 101 0 224 0 0 0 6 0 101 0 240 0 0 0 132 0 2 0 6 0 101 0 0 0 0 0 6 0 101 0 16 0 0 0 6 0 101 0 32 0 0 0 6 0 101 0 48 0 0 0 6 0 101 0 64 0 0 0 6 0 101 0 80 0 0 0 6 0 101 0 96 0 0 0 6 0 101 0 112 0 0 0 6 0 101 0 128 0 0 0 6 0 101 0 144 0 0 0 6 0 101 0 160 0 0 0 6 0 101 0 176 0 0 0 6 0 101 0 192 0 0 0 6 0 101 0 208 0 0 0 6 0 101 0 224 0 0 0 6 0 101 0 240 0 0 0 132 0 3 0 6 0 101 0 0 0 0 0 6 0 101 0 16 0 0 0 6 0 101 0 32 0 0 0 6 0 101 0 48 0 0 0 6 0 101 0 64 0 0 0 6 0 101 0 80 0 0 0 6 0 101 0 96 0 0 0 6 0 101 0 112 0 0 0 6 0 101 0 128 0 0 0 6 0 101 0 144 0 0 0 6 0 101 0 160 0 0 0 6 0 101 0 176 0 0 0 6 0 101 0 192 0 0 0 6 0 101 0 208 0 0 0 6 0 101 0 224 0 0 0 6 0 101 0 240 0 0 0 132 0 4 0 6 0 101 0 0 0 0 0 6 0 101 0 16 0 0 0 6 0 101 0 32 0 0 0 6 0 101 0 48 0 0 0 6 0 101 0 64 0 0 0 6 0 101 0 80 0 0 0 6 0 101 0 96 0 0 0 6 0 101 0 112 0 0 0 6 0 101 0 128 0 0 0 6 0 101 0 144 0 0 0 6 0 101 0 160 0 0 0 6 0 101 0 176 0 0 0 6 0 101 0 192 0 0 0 6 0 101 0 208 0 0 0 6 0 101 0 224 0 0 0 6 0 101 0 240 0 0 0 4 0 5 0 4 0 6 0 132 0 7 0 6 0 101 0 0 0 0 0 6 0 101 0 16 0 0 0 6 0 101 0 32 0 0 0 6 0 101 0 48 0 0 0 6 0 101 0 64 0 0 0 6 0 101 0 80 0 0 0 6 0 101 0 96 0 0 0 6 0 101 0 112 0 0 0 6 0 101 0 128 0 0 0 6 0 101 0 144 0 0 0 6 0 101 0 160 0 0 0 6 0 101 0 176 0 0 0 6 0 101 0 192 0 0 0 6 0 101 0 208 0 0 0 6 0 101 0 224 0 0 0 6 0 101 0 240 0 0 0 132 0 8 0 6 0 101 0 0 0 0 0 6 0 101 0 16 0 0 0 6 0 101 0 32 0 0 0 6 0 101 0 48 0 0 0 6 0 101 0 64 0 0 0 6 0 101 0 80 0 0 0 6 0 101 0 96 0 0 0 6 0 101 0 112 0 0 0 6 0 101 0 128 0 0 0 6 0 101 0 144 0 0 0 6 0 101 0 160 0 0 0 6 0 101 0 176 0 0 0 6 0 101 0 192 0 0 0 6 0 101 0 208 0 0 0 6 0 101 0 224 0 0 0 6 0 101 0 240 0 0 0 132 0 9 0 6 0 101 0 0 0 0 0 6 0 101 0 16 0 0 0 6 0 101 0 32 0 0 0 6 0 101 0 48 0 0 0 6 0 101 0 64 0 0 0 6 0 101 0 80 0 0 0 6 0 101 0 96 0 0 0 6 0 101 0 112 0 0 0 6 0 101 0 128 0 0 0 6 0 101 0 144 0 0 0 6 0 101 0 160 0 0 0 6 0 101 0 176 0 0 0 6 0 101 0 192 0 0 0 6 0 101 0 208 0 0 0 6 0 101 0 224 0 0 0 6 0 101 0 240 0 0 0 132 0 10 0 6 0 101 0 0 0 0 0 6 0 101 0 16 0 0 0 6 0 101 0 32 0 0 0 6 0 101 0 48 0 0 0 6 0 101 0 64 0 0 0 6 0 101 0 80 0 0 0 6 0 101 0 96 0 0 0 6 0 101 0 112 0 0 0 6 0 101 0 128 0 0 0 6 0 101 0 144 0 0 0 6 0 101 0 160 0 0 0 6 0 101 0 176 0 0 0 6 0 101 0 192 0 0 0 6 0 101 0 208 0 0 0 6 0 101 0 224 0 0 0 6 0 101 0 240 0 0 0 4 0 11 0 4 0 12 0]} {328 100 [4 0 0 0 36 0 1 0 6 0 101 0 64 0 0 0 6 0 101 0 176 0 0 0 6 0 101 0 192 0 0 0 6 0 101 0 208 0 0 0 20 0 2 0 6 0 101 0 64 0 0 0 6 0 101 0 208 0 0 0 60 0 3 0 6 0 101 0 0 0 0 0 6 0 101 0 32 0 0 0 6 0 101 0 64 0 0 0 6 0 101 0 160 0 0 0 6 0 101 0 176 0 0 0 6 0 101 0 192 0 0 0 6 0 101 0 208 0 0 0 60 0 4 0 6 0 101 0 0 0 0 0 6 0 101 0 32 0 0 0 6 0 101 0 64 0 0 0 6 0 101 0 160 0 0 0 6 0 101 0 176 0 0 0 6 0 101 0 192 0 0 0 6 0 101 0 208 0 0 0 4 0 5 0 4 0 6 0 28 0 7 0 6 0 101 0 176 0 0 0 6 0 101 0 192 0 0 0 6 0 101 0 208 0 0 0 20 0 8 0 6 0 101 0 64 0 0 0 6 0 101 0 208 0 0 0 60 0 9 0 6 0 101 0 0 0 0 0 6 0 101 0 32 0 0 0 6 0 101 0 64 0 0 0 6 0 101 0 160 0 0 0 6 0 101 0 176 0 0 0 6 0 101 0 192 0 0 0 6 0 101 0 208 0 0 0 20 0 10 0 6 0 101 0 64 0 0 0 6 0 101 0 208 0 0 0 4 0 11 0 4 0 12 0]} {12 121 [4 0 4 0 4 0 6 0]} {52 120 [48 0 1 0 28 0 1 0 24 0 1 0 8 0 1 0 8 0 0 0 12 0 2 0 4 0 3 0 4 0 7 0 8 0 4 0 1 0 0 0 8 0 2 0 8 0 0 0]} {8 143 [227 131 1 8]} {30 148 [227 75 31 255 255 255 255 255 255 255 255 255 255 0 0 0 0 0 0 0 0 0 0 0 0 0]}]
		2020/03/17 12:34:39 interface attribute  169 [0 0 0 0 0 0 0 64]
		2020/03/17 12:34:39 interface attribute  170 [0 0 0 0 0 0 0 64]
		2020/03/17 12:34:39 interface attribute  176 [240 31 128 51 255 255 0 0 255 255 0 0]
		2020/03/17 12:34:39 PHY:  [{8 1 [0 0 0 0]} {9 2 [112 104 121 48 0]} {8 46 [1 0 0 0]} {12 169 [0 0 0 0 0 0 0 64]} {12 170 [0 0 0 0 0 0 0 64]} {16 176 [240 31 128 51 255 255 0 0 255 255 0 0]}]
		2020/03/17 12:34:39 PHY:  [{8 1 [0 0 0 0]} {9 2 [112 104 121 48 0]} {8 46 [1 0 0 0]}]
		2020/03/17 12:34:39 PHY:  [{8 1 [0 0 0 0]} {9 2 [112 104 121 48 0]} {8 46 [1 0 0 0]}]
		2020/03/17 12:34:39 interface attribute  217 [2 34 0 100 0]
		2020/03/17 12:34:39 PHY:  [{8 1 [0 0 0 0]} {9 2 [112 104 121 48 0]} {8 46 [1 0 0 0]} {9 217 [2 34 0 100 0]}]
		2020/03/17 12:34:39 interface attribute  239 [0 0 0 0]
		2020/03/17 12:34:39 PHY:  [{8 1 [0 0 0 0]} {9 2 [112 104 121 48 0]} {8 46 [1 0 0 0]} {8 239 [0 0 0 0]}]
		2020/03/17 12:34:39 PHY:  [{8 1 [0 0 0 0]} {9 2 [112 104 121 48 0]} {8 46 [1 0 0 0]}]
		2020/03/17 12:34:39 PHY:  [{8 1 [0 0 0 0]} {9 2 [112 104 121 48 0]} {8 46 [1 0 0 0]}]




	*/

	for _, a := range attrs {
		switch a.Type {
		case nl80211.AttrIfindex:
			ifi.Index = int(nlenc.Uint32(a.Data))
		case nl80211.AttrWiphyName: // 2
			ifi.Name = nlenc.String(a.Data)
		case nl80211.AttrMac:
			ifi.HardwareAddr = net.HardwareAddr(a.Data)
		case nl80211.AttrWiphy: //1
			ifi.PHY = int(nlenc.Uint32(a.Data))
		case nl80211.AttrIftype:
			// NOTE: InterfaceType copies the ordering of nl80211's interface type
			// constants.  This may not be the case on other operating systems.
			ifi.Type = InterfaceType(nlenc.Uint32(a.Data))
		case nl80211.AttrWdev:
			ifi.Device = int(nlenc.Uint64(a.Data))
		case nl80211.AttrWiphyFreq:
			ifi.Frequency = int(nlenc.Uint32(a.Data))
		case nl80211.AttrWiphyTxPowerLevel:
		case nl80211.AttrGeneration: // 46
		case nl80211.AttrNanDual: // 239
		case nl80211.AttrExtFeatures: // 217
		case nl80211.AttrExtCapa: // 169
		case nl80211.AttrExtCapaMask: // 170
		case nl80211.AttrVhtCapabilityMask: // 176
		case nl80211.AttrWiphyRetryShort:
		case nl80211.AttrWiphyRetryLong:
		case nl80211.AttrWiphyFragThreshold:
		case nl80211.AttrWiphyRtsThreshold:
		case nl80211.AttrMaxNumScanSsids:
		case nl80211.AttrMaxNumSchedScanSsids:
		case nl80211.AttrMaxScanIeLen:
		case nl80211.AttrMaxMatchSets: // 123

		case nl80211.AttrTxFrameTypes: // 99
		case nl80211.AttrRxFrameTypes: // 100
		case nl80211.AttrSupportedIftypes: // 32
		case nl80211.AttrWiphyBands: // 22
		case nl80211.AttrOffchannelTxOk: // 108 - true if present
		case nl80211.AttrSoftwareIftypes:
		case nl80211.AttrInterfaceCombinations:

		default:
			//log.Println("interface attribute ", a.Type, a.Data)
		}

	}

	return nil
}

// parseBSS parses a single BSS with a status attribute from nl80211 BSS messages.
func parseBSS(msgs []genetlink.Message) (*BSS, error) {
	for _, m := range msgs {
		attrs, err := netlink.UnmarshalAttributes(m.Data)
		if err != nil {
			return nil, err
		}

		for _, a := range attrs {
			if a.Type != nl80211.AttrBss {
				continue
			}

			nattrs, err := netlink.UnmarshalAttributes(a.Data)
			if err != nil {
				return nil, err
			}

			// The BSS which is associated with an interface will have a status
			// attribute
			if !attrsContain(nattrs, nl80211.BssStatus) {
				continue
			}

			var bss BSS
			if err := (&bss).parseAttributes(nattrs); err != nil {
				return nil, err
			}

			return &bss, nil
		}
	}

	return nil, os.ErrNotExist
}

// parseAttributes parses netlink attributes into a BSS's fields.
func (b *BSS) parseAttributes(attrs []netlink.Attribute) error {
	for _, a := range attrs {
		switch a.Type {
		case nl80211.BssBssid:
			b.BSSID = net.HardwareAddr(a.Data)
		case nl80211.BssFrequency:
			b.Frequency = int(nlenc.Uint32(a.Data))
		case nl80211.BssBeaconInterval:
			// Raw value is in "Time Units (TU)".  See:
			// https://en.wikipedia.org/wiki/Beacon_frame
			b.BeaconInterval = time.Duration(nlenc.Uint16(a.Data)) * 1024 * time.Microsecond
		case nl80211.BssSeenMsAgo:
			// * @NL80211_BSS_SEEN_MS_AGO: age of this BSS entry in ms
			b.LastSeen = time.Duration(nlenc.Uint32(a.Data)) * time.Millisecond
		case nl80211.BssStatus:
			// NOTE: BSSStatus copies the ordering of nl80211's BSS status
			// constants.  This may not be the case on other operating systems.
			b.Status = BSSStatus(nlenc.Uint32(a.Data))
		case nl80211.BssInformationElements:
			ies, err := ParseIEs(a.Data)
			if err != nil {
				return err
			}

			// TODO(mdlayher): return more IEs if they end up being generally useful
			for _, ie := range ies {
				switch ie.ID {
				case IE_SSID:
					b.SSID = decodeSSID(ie.Data)
				}
			}
		}
	}

	return nil
}

// parseStationInfo parses StationInfo attributes from a byte slice of
// netlink attributes.
func parseStationInfo(b []byte) (*StationInfo, error) {
	attrs, err := netlink.UnmarshalAttributes(b)
	if err != nil {
		return nil, err
	}

	var info StationInfo
	for _, a := range attrs {

		switch a.Type {
		case nl80211.AttrMac:
			info.HardwareAddr = net.HardwareAddr(a.Data)

		case nl80211.AttrStaInfo:
			nattrs, err := netlink.UnmarshalAttributes(a.Data)
			if err != nil {
				return nil, err
			}

			if err := (&info).parseAttributes(nattrs); err != nil {
				return nil, err
			}

			// nl80211.AttrStaInfo is last attibute we are interested in
			return &info, nil

		default:
			// The other attributes that are returned here appear
			// nl80211.AttrIfindex, nl80211.AttrGeneration
			// No need to parse them for now.
			continue
		}
	}

	// No station info found
	return nil, os.ErrNotExist
}

// parseAttributes parses netlink attributes into a StationInfo's fields.
func (info *StationInfo) parseAttributes(attrs []netlink.Attribute) error {
	for _, a := range attrs {
		switch a.Type {
		case nl80211.StaInfoConnectedTime:
			// Though nl80211 does not specify, this value appears to be in seconds:
			// * @NL80211_STA_INFO_CONNECTED_TIME: time since the station is last connected
			info.Connected = time.Duration(nlenc.Uint32(a.Data)) * time.Second
		case nl80211.StaInfoInactiveTime:
			// * @NL80211_STA_INFO_INACTIVE_TIME: time since last activity (u32, msecs)
			info.Inactive = time.Duration(nlenc.Uint32(a.Data)) * time.Millisecond
		case nl80211.StaInfoRxBytes64:
			info.ReceivedBytes = int(nlenc.Uint64(a.Data))
		case nl80211.StaInfoTxBytes64:
			info.TransmittedBytes = int(nlenc.Uint64(a.Data))
		case nl80211.StaInfoSignal:
			//  * @NL80211_STA_INFO_SIGNAL: signal strength of last received PPDU (u8, dBm)
			// Should just be cast to int8, see code here: https://git.kernel.org/pub/scm/linux/kernel/git/jberg/iw.git/tree/station.c#n378
			info.Signal = int(int8(a.Data[0]))
		case nl80211.StaInfoRxPackets:
			info.ReceivedPackets = int(nlenc.Uint32(a.Data))
		case nl80211.StaInfoTxPackets:
			info.TransmittedPackets = int(nlenc.Uint32(a.Data))
		case nl80211.StaInfoTxRetries:
			info.TransmitRetries = int(nlenc.Uint32(a.Data))
		case nl80211.StaInfoTxFailed:
			info.TransmitFailed = int(nlenc.Uint32(a.Data))
		case nl80211.StaInfoBeaconLoss:
			info.BeaconLoss = int(nlenc.Uint32(a.Data))
		case nl80211.StaInfoRxBitrate, nl80211.StaInfoTxBitrate:
			rate, err := parseRateInfo(a.Data)
			if err != nil {
				return err
			}

			// TODO(mdlayher): return more statistics if they end up being
			// generally useful
			switch a.Type {
			case nl80211.StaInfoRxBitrate:
				info.ReceiveBitrate = rate.Bitrate
			case nl80211.StaInfoTxBitrate:
				info.TransmitBitrate = rate.Bitrate
			}
		}

		// Only use 32-bit counters if the 64-bit counters are not present.
		// If the 64-bit counters appear later in the slice, they will overwrite
		// these values.
		if info.ReceivedBytes == 0 && a.Type == nl80211.StaInfoRxBytes {
			info.ReceivedBytes = int(nlenc.Uint32(a.Data))
		}
		if info.TransmittedBytes == 0 && a.Type == nl80211.StaInfoTxBytes {
			info.TransmittedBytes = int(nlenc.Uint32(a.Data))
		}
	}

	return nil
}

// rateInfo provides statistics about the receive or transmit rate of
// an interface.
type rateInfo struct {
	// Bitrate in bits per second.
	Bitrate int
}

// parseRateInfo parses a rateInfo from netlink attributes.
func parseRateInfo(b []byte) (*rateInfo, error) {
	attrs, err := netlink.UnmarshalAttributes(b)
	if err != nil {
		return nil, err
	}

	var info rateInfo
	for _, a := range attrs {
		switch a.Type {
		case nl80211.RateInfoBitrate32:
			info.Bitrate = int(nlenc.Uint32(a.Data))
		}

		// Only use 16-bit counters if the 32-bit counters are not present.
		// If the 32-bit counters appear later in the slice, they will overwrite
		// these values.
		if info.Bitrate == 0 && a.Type == nl80211.RateInfoBitrate {
			info.Bitrate = int(nlenc.Uint16(a.Data))
		}
	}

	// Scale bitrate to bits/second as base unit instead of 100kbits/second.
	// * @NL80211_RATE_INFO_BITRATE: total bitrate (u16, 100kbit/s)
	info.Bitrate *= 100 * 1000

	return &info, nil
}

// attrsContain checks if a slice of netlink attributes contains an attribute
// with the specified type.
func attrsContain(attrs []netlink.Attribute, typ uint16) bool {
	for _, a := range attrs {
		if a.Type == typ {
			return true
		}
	}

	return false
}

// decodeSSID safely parses a byte slice into UTF-8 runes, and returns the
// resulting string from the runes.
func decodeSSID(b []byte) string {
	buf := bytes.NewBuffer(nil)
	for len(b) > 0 {
		r, size := utf8.DecodeRune(b)
		b = b[size:]

		buf.WriteRune(r)
	}

	return buf.String()
}
