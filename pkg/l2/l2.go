package l2

import (
	"sync"

	"github.com/costinm/dmesh-l2/pkg/l2/wifi"
	"github.com/costinm/dmesh-l2/pkg/l2api"
	"github.com/costinm/wpgate/pkg/msgs"
)

type L2 struct {
	m sync.Mutex

	// The map of devices. The key depends on the device type, all devices are mapped to uint64
	// Devices with multiple interfaces will appear multiple times.
	devByL2Id map[uint64]*l2api.MeshDevice

	// The mesh id is only available after discovery.
	// It can be passed in beacons, nan FSD, DNS-SD, etc.
	// The ID is also used in the mesh IPv6 address as node address, and is usually the sha(public key)
	devByMeshId map[uint64]*l2api.MeshDevice
	mux         *msgs.Mux

	// List of active wifi interfaces - STA, AP, etc - excluding monitors
	actWifi []*wifi.Interface
	// Monitor interfaces
	physMon     map[int]*wifi.Interface
	netLinkWifi *wifi.Client
}

func NewL2(mux *msgs.Mux) *L2 {
	l2 := &L2{
		devByL2Id:   map[uint64]*l2api.MeshDevice{},
		devByMeshId: map[uint64]*l2api.MeshDevice{},
		mux:         mux,
	}
	return l2
}
