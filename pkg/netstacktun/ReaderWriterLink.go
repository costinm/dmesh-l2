package netstacktun

import (
	"github.com/google/netstack/tcpip"
	"github.com/google/netstack/tcpip/buffer"
	"github.com/google/netstack/tcpip/header"
	"github.com/google/netstack/tcpip/stack"

	"io"
	"log"
	"os"
)

// Attempt to replicate the fdbased
// Copyright 2016 The Netstack Authors. All rights reserved.

// BufConfig defines the shape of the vectorised view used to read packets from the NIC.
var BufConfig = []int{128, 256, 256, 512, 1024, 2048, 4096, 8192, 16384, 32768}

type endpoint struct {
	// mtu (maximum transmission unit) is the maximum size of a packet.
	mtu uint32

	// hdrSize specifies the link-layer header size. If set to 0, no header
	// is added/removed; otherwise an ethernet header is used.
	hdrSize int

	// addr is the address of the endpoint.
	addr tcpip.LinkAddress

	// caps holds the endpoint capabilities.
	caps stack.LinkEndpointCapabilities

	// closed is a function to be called when the FD's peer (if any) closes
	// its end of the communication pipe.
	closed func(*tcpip.Error)

	vv *buffer.VectorisedView
	//iovecs   []syscall.Iovec
	views    []buffer.View
	attached bool

	tunw io.WriteCloser
	tunr io.Reader
}

// Options specify the details about the fd-based endpoint to be created.
type Options struct {
	MTU             uint32
	EthernetHeader  bool
	ChecksumOffload bool
	ClosedFunc      func(*tcpip.Error)
	Address         tcpip.LinkAddress
}

// New creates a new fd-based endpoint.
//
// Makes fd non-blocking, but does not take ownership of fd, which must remain
// open for the lifetime of the returned endpoint.
func NewReaderWriterLink(tunw io.WriteCloser, tunr io.Reader, opts *Options) tcpip.LinkEndpointID {

	caps := stack.LinkEndpointCapabilities(0)
	if opts.ChecksumOffload {
		caps |= stack.CapabilityChecksumOffload
	}

	hdrSize := 0
	//if opts.EthernetHeader {
	//	hdrSize = header.EthernetMinimumSize
	//	caps |= stack.CapabilityResolutionRequired
	//}

	e := &endpoint{
		tunr:    tunr,
		tunw:    tunw,
		mtu:     opts.MTU,
		caps:    caps,
		closed:  opts.ClosedFunc,
		addr:    opts.Address,
		hdrSize: hdrSize,
		views:   make([]buffer.View, len(BufConfig)),
		//iovecs:  make([]syscall.Iovec, len(BufConfig)),
	}
	vv := buffer.NewVectorisedView(0, e.views)
	e.vv = &vv
	return stack.RegisterLinkEndpoint(e)
}

// Attach launches the goroutine that reads packets from the file descriptor and
// dispatches them via the provided dispatcher.
func (e *endpoint) Attach(dispatcher stack.NetworkDispatcher) {
	e.attached = true
	go e.dispatchLoop(dispatcher)
}

// IsAttached implements stack.LinkEndpoint.IsAttached.
func (e *endpoint) IsAttached() bool {
	return e.attached
}

// MTU implements stack.LinkEndpoint.MTU. It returns the value initialized
// during construction.
func (e *endpoint) MTU() uint32 {
	return e.mtu
}

// Capabilities implements stack.LinkEndpoint.Capabilities.
func (e *endpoint) Capabilities() stack.LinkEndpointCapabilities {
	return e.caps
}

// MaxHeaderLength returns the maximum size of the link-layer header.
func (e *endpoint) MaxHeaderLength() uint16 {
	return uint16(e.hdrSize)
}

// LinkAddress returns the link address of this endpoint.
func (e *endpoint) LinkAddress() tcpip.LinkAddress {
	return e.addr
}

// WritePacket writes outbound packets to the file descriptor. If it is not
// currently writable, the packet is dropped.
func (e *endpoint) WritePacket(r *stack.Route, hdr buffer.Prependable, payload buffer.VectorisedView, protocol tcpip.NetworkProtocolNumber) *tcpip.Error {

	if payload.Size() == 0 {
		e.tunw.Write(hdr.View())
	}

	// TODO: reuse a buffer
	m1 := make([]byte, 1600)
	hdrB := hdr.View()
	copy(m1, hdrB)
	pv := payload.ToView()
	copy(m1[len(hdrB):], pv)
	//e.tunw.Write(hdr.UsedBytes())
	//e.tunw.Write(payload)
	go func() {
		if Dump {
			log.Println("TUNW: Write start ", len(pv))
		}
		n, err := e.tunw.Write(m1[0 : len(hdrB)+len(pv)])
		if Dump {
			log.Println("TUNW: Write done ", n, err)
		}
	}()
	return nil
}

// dispatch reads one packet from the file descriptor and dispatches it.
func (e *endpoint) dispatch(d stack.NetworkDispatcher, largeV buffer.View) (bool, *tcpip.Error) {
	//e.allocateViews(BufConfig)
	// TODO: reuse
	b := buffer.NewView(2048)

	n, err := e.tunr.Read(b)
	if err != nil {
		log.Printf("TUNR: READ error %V", err)
		if err == os.ErrClosed || err == io.EOF {
			return false, tcpip.ErrClosedForSend
		}
		return true, nil
	}
	if Dump {
		log.Print("TUNR: ", n, err)
	}
	if n <= e.hdrSize {
		return false, nil
	}

	var (
		p                             tcpip.NetworkProtocolNumber
		remoteLinkAddr, localLinkAddr tcpip.LinkAddress
	)
	if e.hdrSize > 0 {
		eth := header.Ethernet(b)
		p = eth.Type()
		remoteLinkAddr = eth.SourceAddress()
		localLinkAddr = eth.DestinationAddress()
	} else {
		// We don't get any indication of what the packet is, so try to guess
		// if it's an IPv4 or IPv6 packet.
		switch header.IPVersion(b) {
		case header.IPv4Version:
			p = header.IPv4ProtocolNumber
		case header.IPv6Version:
			p = header.IPv6ProtocolNumber
		default:
			return true, nil
		}
	}

	//used := e.capViews(n, BufConfig)
	//vv := buffer.NewVectorisedView(n, e.views[:used])
	//vv.TrimFront(e.hdrSize)

	e.views[0] = b[0:n]
	vv := buffer.NewVectorisedView(n, e.views[:1])
	vv.TrimFront(e.hdrSize)
	//e.vv.SetViews(e.views[0:1])
	//e.vv.SetSize(n)
	//e.vv.TrimFront(e.hdrSize)

	d.DeliverNetworkPacket(e, remoteLinkAddr, localLinkAddr, p, vv)

	return true, nil
}

func (e *endpoint) capViews(n int, buffers []int) int {
	c := 0
	for i, s := range buffers {
		c += s
		if c >= n {
			e.views[i].CapLength(s - (c - n))
			return i + 1
		}
	}
	return len(buffers)
}

// dispatchLoop reads packets from the file descriptor in a loop and dispatches
// them to the network stack.
func (e *endpoint) dispatchLoop(d stack.NetworkDispatcher) *tcpip.Error {
	v := buffer.NewView(header.MaxIPPacketSize)
	log.Print("TUN: Starting TUN dispatch loop")
	for {
		cont, err := e.dispatch(d, v)
		if err != nil || !cont {
			if e.closed != nil {
				e.closed(err)
			}
			log.Print("TUN: CLOSE dispatch loop ", err)
			return err
		}
	}
}
