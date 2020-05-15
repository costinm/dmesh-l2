package main

import (
	"log"
	"net/http"
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
// DMesh can also run as root or with CAP_NET - but for debugging in IDE it is
// easier to use dmroot, and it is also more secure.
// If using ufw, run "ufw allow 67/udp"
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

	// TODO: use h2 transport with certs.

	if "" != os.Getenv("MSG_ADDR") {
		// allow HTTP interface for messages. If not set, only
		// msg communication is via UDS, from user 1337 or 0 or UID
		// Insecure - debug only
		http.ListenAndServe(os.Getenv("MSG_ADDR"), msgs.DefaultMux.Gate.Mux.ServeMux)
	} else {
		select {}
	}
}
