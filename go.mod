module github.com/costinm/dmesh-l2

go 1.14

//replace github.com/costinm/wpgate => ../wpgate

require (
	github.com/costinm/wpgate v0.0.0-20200310154919-3cd18aaa0415
	github.com/go-ble/ble v0.0.0-20200120171844-0a73a9da88eb
	github.com/google/gopacket v1.1.17
	github.com/jsimonetti/rtnetlink v0.0.0-20200117123717-f846d4f6c1f4
	github.com/krolaw/dhcp4 v0.0.0-20190909130307-a50d88189771
	github.com/mdlayher/genetlink v1.0.0
	github.com/mdlayher/netlink v1.1.0
	github.com/prometheus/client_golang v0.9.3-0.20190127221311-3c4408c8b829 // indirect
	golang.org/x/net v0.0.0-20200202094626-16171245cfb2
	golang.org/x/sys v0.0.0-20200202164722-d101bd2416d5
)
