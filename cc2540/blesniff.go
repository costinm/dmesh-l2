package main

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"log"
	"os"

	"github.com/google/gousb"
)

// http://shukra.cedt.iisc.ernet.in/edwiki/Smart_RF_Equivalent

// https://github.com/bertrik/cc2540

type BLESniff struct {
	Devices map[string]*BLEDevice
}

type BLEDevice struct {
	MAC       string
	LastTS    uint64
	LastDelta uint32
	LastMsg   []byte
	RSSI      uint16
}

func main() {
	// Initialize a new Context.
	ctx := gousb.NewContext()
	defer ctx.Close()

	dev, err := ctx.OpenDeviceWithVIDPID(0x451, 0x16B3)
	if err != nil {
		log.Fatalf("Could not open a device: %v", err)
	}
	defer dev.Close()

	// The default interface is always #0 alt #0 in the currently active
	// config.
	intf, done, err := dev.DefaultInterface()
	if err != nil {
		log.Fatalf("%s.DefaultInterface(): %v", dev, err)
	}
	defer done()

	// Open an OUT endpoint.
	ep, err := intf.InEndpoint(0x83)
	if err != nil {
		log.Fatalf("%s.InEndpoint(1): %v", intf, err)
	}

	data := make([]byte, 128)

	numBytes, err := dev.Control(0xc0, 0xc0, 0, 0, data)
	if err != nil {
		log.Fatalf("%s.OutEndpoint(1): %v", intf, err)
	}
	fmt.Println(numBytes, data[0:numBytes])

	// set power
	power := 4
	numBytes, err = dev.Control(0x40, 0xc5, 0, uint16(power), nil)
	if err != nil {
		log.Fatalf("%s setPower %v", intf, err)
	}

	for i := 0; i < 4; i++ {
		numBytes, err = dev.Control(0xC0, 0xc6, 0, 0, data)
		if err != nil {
			log.Fatalf("%s getPower %v", intf, err)
		}
		if data[0] == byte(power) {
			break
		} else {
			log.Println("Power: ", data[0])
		}
	}

	// TODO: C0 until value is the same

	numBytes, err = dev.Control(0x40, 0xC9, 0, 0, nil)
	if err != nil {
		log.Fatalf("%s.OutEndpoint(1): %v", intf, err)
	}

	channel := 37
	data[0] = byte(channel & 0xFF)
	numBytes, err = dev.Control(0x40, 0xD2, 0, 0, data)
	if err != nil {
		log.Fatalf("%s.OutEndpoint(1): %v", intf, err)
	}
	data[0] = byte(channel >> 8 & 0xFF)
	numBytes, err = dev.Control(0x40, 0xD2, 0, 1, data)
	if err != nil {
		log.Fatalf("%s.OutEndpoint(1): %v", intf, err)
	}

	numBytes, err = dev.Control(0x40, 0xD0, 0, 0, nil)
	if err != nil {
		log.Fatalf("%s.OutEndpoint(1): %v", intf, err)
	}

	for {
		rd, err := ep.Read(data)
		if err != nil {
			log.Fatalf("%s.OutEndpoint(1): %v", intf, err)
		}
		if data[0] == 1 {
			continue
		}
		if rd < 12+4+1+2+6 {
			continue
		}

		// 0, len(packet), ????, len(ble)
		plen := binary.LittleEndian.Uint16(data[1:])
		if int(plen)+3 != rd {
			log.Println(rd, plen)
		}
		blen := data[7]
		if int(blen)+5 != int(plen) {
			log.Println(data[7], plen)
		}
		start := 1 + 2 + 4 + 1
		accAddr := binary.LittleEndian.Uint32(data[start:])
		if accAddr != 0x8E89BED6 {
			log.Println("Access address: ", hex.EncodeToString(data[start:start+4]))
		}
		start += 4
		ts := binary.LittleEndian.Uint32(data[3:7])

		if int(data[start+1]) != rd-start-7 {
			log.Println("Invalid len", data[start+3], rd-start-7)
		}

		log.Println(ts, hex.EncodeToString(data[start:start+2]),
			hex.EncodeToString(data[start+2:rd-2]), rd-7-start, data[rd-2], data[rd-1]&0x7f)

		//
		//log.Println(hex.EncodeToString(data[start:start+2]),
		//	hex.EncodeToString(data[start+2:start+8]),
		//	hex.EncodeToString(data[start+8:rd-5]),
		//	data[rd-2],
		//	data[rd-1] & 0x7F)
		//
		//
	}
}
