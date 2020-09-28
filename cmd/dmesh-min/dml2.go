package main

import (
	"log"
	_ "net/http/pprof"
	"os"

	"github.com/costinm/dmesh-l2/pkg/l2"
	"github.com/costinm/wpgate/pkg/msgs"
	"github.com/costinm/wpgate/pkg/transport/uds"
)

// Helper program to allow running the L2 high priviledge operations separated
// from the rest.
//
// Will handle tun, iptables, routing - as well as BT and WifiInterface.
//
// mips: 6.4M (12M for dmesh) before including dmesh
func main() {

	mux := msgs.DefaultMux

	// WifiInterface into the mux. Receives messages and send events.
	udsS, err := uds.NewServer("dmesh", mux)
	if err != nil {
		log.Fatal("Failed to start server ", err)
	}
	go udsS.Start()

	l2main := l2.NewL2(mux)

	// Used to communicate with wpa_supplicant, if any
	wpaDir := os.Getenv("WPA_DIR")
	if wpaDir == "" {
		wpaDir = "/var/run/wpa_supplicant/"
	}

	wpa, err := l2main.NewWPA(wpaDir, 0, os.Getenv("AP"))
	if err != nil {
		log.Print("Failed to open WPA ", err)
	} else {
		// Messages on the wifi topic, received on the mux.
		mux.AddHandler("wifi", wpa)
	}

	_, err = l2main.InitBLE()
	if err != nil {
		log.Println("BLE: ", err)
	}

	// Low level Wifi - NAN
	err = l2main.InitWifi()
	if err != nil {
		log.Fatal(err)
	}

	// TODO: reset the iptables capture on exit
	// TODO: setup iptables capture

	// TODO: start dmesh iw interface if possible.
	// TODO: start ap

	// TODO: use h2 transport ?

	select {}
}

