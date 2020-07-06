package udp

import "testing"

func testUDP(t *testing.T) {
	dc, _ := NewUdpServer()
	defer dc.Close()

	err := dc.Listen()
	if err != nil {t.Fatal(err)}



}
