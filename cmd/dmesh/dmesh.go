package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/costinm/dmesh-l2/pkg/lmnet"
	"github.com/costinm/dmesh-l2/pkg/netstacktun"
	"github.com/costinm/wpgate/pkg/bootstrap"
	"github.com/costinm/wpgate/pkg/conf"
	"github.com/costinm/wpgate/pkg/h2"
	"github.com/costinm/wpgate/pkg/mesh"
	"github.com/costinm/wpgate/pkg/msgs"
	"github.com/costinm/wpgate/pkg/transport/local"
	uds2 "github.com/costinm/wpgate/pkg/transport/uds"
)

// Full: all features. Has a UDS connection, similar with the Android package.
func main() {
	log.Print("Starting native process pwd=", os.Getenv("PWD"), os.Environ())
	bp := 5200
	base := os.Getenv("BASE_PORT")
	if base != "" {
		bp, _ = strconv.Atoi(base)
	}

	cfgDir := os.Getenv("HOME") + "/.ssh/"
	all := &bootstrap.ServerAll{
		ConfDir:  cfgDir,
		BasePort: bp,
	}
	bootstrap.StartAll(all)

	// Debug interface
	log.Println("Starting WPS server on ", all.BasePort)

	initUDSConnection(all.H2, all.GW, all.Local, all.Conf)

	//// Periodic registrations.
	//m.Registry.RefreshNetworksPeriodic()

	http.ListenAndServe(fmt.Sprintf("127.0.0.1:%d", all.BasePort+bootstrap.HTTP_DEBUG), all.UI)
}

func initUDSConnection(h2 *h2.H2, gw *mesh.Gateway, ld *local.LLDiscovery, cfg *conf.Conf) {

	var vpn *os.File
	// Attempt to connect to local UDS socket, to communicate with android app.
	for i := 0; i < 5; i++ {
		ucon, err := uds2.Dial("dmesh", msgs.DefaultMux, map[string]string{})
		if err != nil {
			log.Println("Failed to initialize UDS ", err)
			time.Sleep(1 * time.Second)
		} else {
			lmnet.NewWifi(ld, &ucon.MsgConnection, ld)

			// Special messages:
			// - close - terminate program, java side dead
			// - KILL - explicit request to stop
			ucon.Handler = msgs.HandlerCallbackFunc(func(ctx context.Context, cmdS string, meta map[string]string, data []byte) {
				args := strings.Split(cmdS, "/")

				switch args[1] {

				case "KILL":
					log.Printf("Kill command received, exit")
					os.Exit(1)

					// Handshake's first message - metadata for the other side.
				case "P": // properties - on settings change. Properties will be stored in H2.Conf
					log.Println("Received settings: ", meta)
					for k, v := range meta {
						cfg.Conf[k] = v
						if k == "ua" {
							//dm.Registry.UserAgent = v
							gw.UA = v
						}
					}
					ld.RefreshNetworks()

				case "r": // refresh networks
					log.Println("UDS: refresh network (r)")
					go func() {
						time.Sleep(2 * time.Second)
						ld.RefreshNetworks()
					}()

				case "k": // VPN kill
					if vpn != nil {
						vpn.Close()
						vpn = nil
						log.Println("Closing VPN")
					}
				case "v": // VPN client for android
					fa := ucon.File()
					vpn = fa
					if fa != nil {
						log.Println("Received VPN UDS client (v), starting TUN", fa, ucon.Files)
						// The dmtun will be passed as a reader when connecting to the VPN.
						//mesh.Tun = NewTun(fa, fa)
						link := netstacktun.NewReaderWriterLink(fa, fa, &netstacktun.Options{MTU: 1600})
						netstack := netstacktun.NewTunCapture(&link, gw, false)
						gw.UDPWriter = netstack
					} else {
						log.Println("ERR: UDS: VPN TUN: invalid VPN file descriptor (v)")
					}

				case "V": // VPN master
					fa := ucon.File()
					vpn = fa
					if fa != nil {
						log.Println("Received VPN UDS master (V), starting VPN DmDns", fa, ucon.Files)
						link := netstacktun.NewReaderWriterLink(fa, fa, &netstacktun.Options{MTU: 1600})
						netstack := netstacktun.NewTunCapture(&link, gw, false)
						gw.UDPWriter = netstack
					} else {
						log.Println("ERR: UDS: invalid VPN file descriptor (V)")
					}

				case "CON":
					switch args[2] {
					case "STOP":
						ld.RefreshNetworks()
						ld.WifiInfo.Net = ""
					case "START":
						ld.RefreshNetworks()
						ld.WifiInfo.Net = meta["ssid"]
					}
				}
			})
			go func() {
				for {
					ucon.HandleStream()
					// Connection closes if the android side is dead.
					// TODO: this is only for the UDS connection !!!
					log.Printf("UDS: parent closed, exiting ")
					os.Exit(4)
				}
			}()

			break
		}
	}
}
