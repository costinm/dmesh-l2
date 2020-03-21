package l2

import (
	"encoding/hex"
	"strings"
	"testing"

	mesh "github.com/costinm/dmesh-l2/pkg/l2api"
	//"github.com/costinm/dmesh/dm/mesh"
)

// MAC address is different from the DISC_RESP - but it matches p2p_dev_addr
const devFnd = "P2P-DEVICE-FOUND da:50:e6:91:db:cb p2p_dev_addr=da:50:e6:91:5b:cb pri_dev_type=10-0050F204-5 name='Android_fea8' config_methods=0x188 dev_capab=0x25 group_capab=0xab new=1"

const disNew = "P2P-SERV-DISC-RESP 32:85:a9:da:ce:09 56 3c0001010002646d035f646dc01c0010010a703d497248537142514208633d636f7374696e18733d4449524543542d564f2d416e64726f69645f363536611100010100035f646dc01c000c0102646dc027"

const discTxt = "0a703d497248537142514208633d636f7374696e18733d4449524543542d564f2d416e64726f69645f36353661"

// 3c00 - 60 bytes
// 01
// 01
// 00

// nameTxt:
// dnsData:
// 02 646d dm
// 03 5f646d _dm -- 10
// c0 1c _udp.local
// 00 .
// 10 TXT
// 01 Version
// 0a 703d4972485371425142 -- 26 - pass ( 8 + 2 )
// 08 633d636f7374696e -- 35 - ssid (6 + 2)
// 18 733d4449524543542d564f2d416e6472 6f69645f36353661 -- 60 - net ( 22 + 2 )

// Extra stuff
// 11 00010100035f646dc01c000c0102646dc0
// 27

func TestParse(t *testing.T) {
	//d := onP2PDeviceFound(strings.Split(devFnd, " "))
	//if d.Mac != "da:50:e6:91:db:cb" {
	//	t.Error("Invalid mac")
	//}
	//if d.Meta["name"] != "'Android_fea8'" {
	//	t.Error("Invalid meta")
	//}
	//if d.Meta["new"] != "1" {
	//	t.Error("Invalid new")
	//}
	d := mesh.MeshDevice{}

	parseDisc(strings.Split(disNew, " "), &d)
	if d.SSID != "DIRECT-VO-Android_656a" {
		t.Error("Invalid SSID ", d.SSID)
	}
	if d.PSK != "IrHSqBQB" {
		t.Error("Invalid pass ", d.PSK)
	}
	if d.Net != "costin" {
		t.Error("Invalid net ", d.Net)
	}

	data := make([]byte, len(discTxt)/2)
	n, _ := hex.Decode(data, []byte(discTxt))

	rec := parseDns(data[0:n])
	t.Log(rec)

	rec1 := nameTxt + hex.EncodeToString(packDns("0200010702000108", map[string]string{"a": "b", "c": "d"}))
	rec1b, _ := hex.DecodeString(rec1)

	// TODO: changed
	rec1P, end, err := unpackDomainName(rec1b, 0)
	if rec1b[end] != 0 {
		t.Fatal("Unexpected record ", end, rec1b[end], rec1b)
	}
	end++
	if rec1b[end] != 0x10 {
		t.Fatal("Unexpected record ", end, rec1b[end], rec1b)
	}
	end++
	meta := parseDns(rec1b[end:])
	t.Log(rec1P, meta, err)

}

/*

2019/05/22 22:23:05 WPA_CMD:  1.441768ms P2P_SERV_DISC_RESP 2412 ae:37:43:df:1b:a5 0
02646d035f646dc01c001015733d4449524543542d47632d444d4553482d5750410a703d75566b4b764f5757 -> OK


9-05-22 22:23:06.177 1822-1822/? D/wpa_supplicant: nl80211: RX frame da=ae:37:43:df:1b:a5 sa=38:ba:f8:49:d3:c0 bssid=38:ba:f8:49:d3:c0 freq=2412 ssi_signal=0 fc=0xd0 seq_ctrl=0x30 stype=13 (WLAN_FC_STYPE_ACTION) len=57
2019-05-22 22:23:06.177 1822-1822/? D/wpa_supplicant: p2p0: Event RX_MGMT (18) received
2019-05-22 22:23:06.178 1822-1822/? D/wpa_supplicant: p2p0: Received Action frame: SA=38:ba:f8:49:d3:c0 Category=4 DataLen=32 freq=2412 MHz
2019-05-22 22:23:06.178 1822-1822/? D/wpa_supplicant: GAS: No pending query found for 38:ba:f8:49:d3:c0 dialog token 0
2019-05-22 22:23:06.178 1822-1822/? D/wpa_supplicant: p2p0: Radio work 'p2p-send-action'@0x79a600a2a0 done in 0.018633 seconds
2019-05-22 22:23:06.178 1822-1822/? D/wpa_supplicant: p2p0: radio_work_free('p2p-send-action'@0x79a600a2a0): num_active_works --> 0
2019-05-22 22:23:06.178 1822-1822/? D/wpa_supplicant: Off-channel: Action frame sequence done notification: pending_action_tx=0x0 drv_offchan_tx=1 action_tx_wait_time=5000 off_channel_freq=0 roc_waiting_drv_freq=0
2019-05-22 22:23:06.178 1822-1822/? D/wpa_supplicant: nl80211: Cancel TX frame wait: cookie=0xffffffc09f262000
2019-05-22 22:23:06.185 1822-1822/? D/wpa_supplicant: P2P: Clear timeout (state=SD_DURING_FIND)

2019-05-22 22:23:06.185 1822-1822/? D/wpa_supplicant: P2P: Received GAS Initial Response from 38:ba:f8:49:d3:c0 (len=31)
2019-05-22 22:23:06.185 1822-1822/? D/wpa_supplicant: P2P: dialog_token=0 status_code=0 comeback_delay=0
2019-05-22 22:23:06.185 1822-1822/? D/wpa_supplicant: P2P: Query Response Length: 20
2019-05-22 22:23:06.185 1822-1822/? D/wpa_supplicant: P2P: Service Update Indicator: 0
2019-05-22 22:23:06.186 1822-1822/? D/wpa_supplicant: P2P: Service Response TLV
2019-05-22 22:23:06.186 1822-1822/? D/wpa_supplicant: P2P: Service Protocol Type 1
2019-05-22 22:23:06.186 1822-1822/? D/wpa_supplicant: P2P: Service Transaction ID 5
2019-05-22 22:23:06.186 1822-1822/? D/wpa_supplicant: P2P: Status Code ID 1
2019-05-22 22:23:06.186 1822-1822/? D/wpa_supplicant: P2P: Service Response TLV
2019-05-22 22:23:06.186 1822-1822/? D/wpa_supplicant: P2P: Service Protocol Type 1
2019-05-22 22:23:06.186 1822-1822/? D/wpa_supplicant: P2P: Service Transaction ID 6
2019-05-22 22:23:06.186 1822-1822/? D/wpa_supplicant: P2P: Status Code ID 1
2019-05-22 22:23:06.186 1822-1822/? D/wpa_supplicant: Notifying P2P service discovery response to hidl control 38:ba:f8:49:d3:c0


Good find:
2019-05-23 07:02:47.486 1822-1822/? D/wpa_supplicant: P2P: Added SD Query 0x79a600e700

2019-05-23 06:46:59.692 1822-1822/? D/wpa_supplicant: P2P: State SEARCH -> SD_DURING_FIND
01000015733d4449524543542d4e762d444d4553482d5750410a703d3737417762705561

2019-05-23 06:46:59.858 1822-1822/? D/wpa_supplicant: P2P: Clear timeout (state=SD_DURING_FIND)
2019-05-23 06:46:59.858 1822-1822/? D/wpa_supplicant: P2P: Received GAS Initial Response from 42:4e:36:81:d4:1f (len=151)
2019-05-23 06:46:59.858 1822-1822/? D/wpa_supplicant: P2P: dialog_token=0 status_code=0 comeback_delay=0
2019-05-23 06:46:59.859 1822-1822/? D/wpa_supplicant: P2P: Query Response Length: 140
2019-05-23 06:46:59.859 1822-1822/? D/wpa_supplicant: P2P: Service Update Indicator: 7
2019-05-23 06:46:59.859 1822-1822/? D/wpa_supplicant: P2P: Service Response TLV
2019-05-23 06:46:59.859 1822-1822/? D/wpa_supplicant: P2P: Service Protocol Type 1
2019-05-23 06:46:59.859 1822-1822/? D/wpa_supplicant: P2P: Service Transaction ID 9
2019-05-23 06:46:59.859 1822-1822/? D/wpa_supplicant: P2P: Status Code ID 0
2019-05-23 06:46:59.859 1822-1822/? D/wpa_supplicant: P2P: Service Response TLV
2019-05-23 06:46:59.859 1822-1822/? D/wpa_supplicant: P2P: Service Protocol Type 1
2019-05-23 06:46:59.859 1822-1822/? D/wpa_supplicant: P2P: Service Transaction ID 9
2019-05-23 06:46:59.859 1822-1822/? D/wpa_supplicant: P2P: Status Code ID 0
2019-05-23 06:46:59.859 1822-1822/? D/wpa_supplicant: P2P: Service Response TLV
2019-05-23 06:46:59.859 1822-1822/? D/wpa_supplicant: P2P: Service Protocol Type 1
2019-05-23 06:46:59.859 1822-1822/? D/wpa_supplicant: P2P: Service Transaction ID 10
2019-05-23 06:46:59.859 1822-1822/? D/wpa_supplicant: P2P: Status Code ID 0
2019-05-23 06:46:59.859 1822-1822/? D/wpa_supplicant: P2P: Service Response TLV
2019-05-23 06:46:59.859 1822-1822/? D/wpa_supplicant: P2P: Service Protocol Type 1
2019-05-23 06:46:59.859 1822-1822/? D/wpa_supplicant: P2P: Service Transaction ID 10
2019-05-23 06:46:59.859 1822-1822/? D/wpa_supplicant: P2P: Status Code ID 0
2019-05-23 06:46:59.859 1822-1822/? D/wpa_supplicant: Notifying P2P service discovery response to hidl control 42:4e:36:81:d4:1f
2019-05-23 06:46:59.859 1822-1822/? D/wpa_supplicant: P2P: State SD_DURING_FIND -> SEARCH


*/

const bonjour1 = "096d797072696e746572045f697070c00c001001"

// 09 6d797072696e746572
// 04 5f697070
// c0 0c ???
// 00
// 10 TXT
// 01

const bonjour2 = "09747874766572733d311a70646c3d6170706c69636174696f6e2f706f7374736372797074"

// 09 747874766572733d31
// 1a 70646c3d6170706c69636174696f6e2f 706f7374736372797074
