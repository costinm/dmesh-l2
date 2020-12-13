module github.com/costinm/dmesh-l2

go 1.14

replace github.com/google/netstack => github.com/costinm/netstack v0.0.0-20190601172006-f6e50d4d2856

//replace github.com/costinm/wpgate => ../wpgate

require (
	github.com/costinm/wpgate v0.0.0-20200916184639-a66a6c07c5ec
	github.com/go-ble/ble v0.0.0-20200120171844-0a73a9da88eb
	github.com/gogo/protobuf v1.3.1
	github.com/google/gopacket v1.1.17
	github.com/google/netstack v0.0.0-00010101000000-000000000000
	github.com/jsimonetti/rtnetlink v0.0.0-20200117123717-f846d4f6c1f4
	github.com/krolaw/dhcp4 v0.0.0-20190909130307-a50d88189771
	github.com/mdlayher/genetlink v1.0.0
	github.com/mdlayher/netlink v1.1.0
	github.com/songgao/water v0.0.0-20200317203138-2b4b6d7c09d8
	golang.org/x/net v0.0.0-20200707034311-ab3426394381
)
