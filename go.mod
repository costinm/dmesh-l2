module github.com/costinm/dmesh-l2

go 1.21

//replace github.com/google/netstack => github.com/costinm/netstack v0.0.0-20190601172006-f6e50d4d2856

replace github.com/costinm/ugate => ../ugate

replace github.com/costinm/ugate/auth => ../ugate/auth

require (
	github.com/costinm/ugate v0.0.0-20210221155556-10edd21fadbf
	github.com/go-ble/ble v0.0.0-20200120171844-0a73a9da88eb
	github.com/gogo/protobuf v1.3.1
	github.com/google/gopacket v1.1.18
	github.com/google/gousb v1.1.1
	//github.com/google/netstack v0.0.0-00010101000000-000000000000
	github.com/jsimonetti/rtnetlink v0.0.0-20200117123717-f846d4f6c1f4
	github.com/krolaw/dhcp4 v0.0.0-20190909130307-a50d88189771
	github.com/mdlayher/genetlink v1.0.0
	github.com/mdlayher/netlink v1.1.0
	golang.org/x/net v0.0.0-20211014172544-2b766c08f1c0
)

require (
	github.com/costinm/hbone v0.0.0-20220731143958-835b4d46903e // indirect
	github.com/costinm/ugate/auth v0.0.0-00010101000000-000000000000 // indirect
	github.com/mattn/go-colorable v0.1.4 // indirect
	github.com/mattn/go-isatty v0.0.10 // indirect
	github.com/mgutz/ansi v0.0.0-20170206155736-9520e82c474b // indirect
	github.com/mgutz/logxi v0.0.0-20161027140823-aebf8a7d67ab // indirect
	github.com/pkg/errors v0.8.1 // indirect
	golang.org/x/sys v0.0.0-20210423082822-04245dca01da // indirect
)
