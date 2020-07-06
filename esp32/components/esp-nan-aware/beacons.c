#include <stdio.h>
#include <string.h>
#include <time.h>
#include "nan.h"

#include "esp_wifi.h"

static const char *TAG = "nan";

// WIP: support for sending NAN beacons, master election
// ESP32 doesn't allow sending beacons properly - the timestamp is set to zero.
// If this is fixed it'll be possible to properly support this.
//

int esp_wifi_internal_tx(wifi_interface_t wifi_if, void *buffer, uint16_t len);

/**
 * send SYNC (512 ms, if master) or discovery beacon (100ms, if anchor master)
 *
 *
 * @param conId
 * @param data
 * @param len
 */
void nanSendSync(int interval) {
    uint8_t head[] = {
            0x80, 0x00, // type/sub
            0x00, 0x00, // duration
            /* 4: DST */
            0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF,
            /* 10: SRC */
            0x00, 0x00, 0x00, 0x00, 0x00, 0x00, // will be set to this device addr
            /* 16 BSSID */
            0x50, 0x6F, 0x9A, 0x01, 0xd9, 0x49, // NAN BSSID
            /* 22: SEQ, FRAG */
            0, 0,

            // FRAME_DATA
            // 24: fixed params. TS - SET TO ZERO BY TX !!!!
            0xcc, 0, 0xc0, 0xb3, 1, 2, 3, 4,
            // 32
            0, 2, // beacon interval = 512
            0x20, 0x04, // caps

            // 36: tagged info
            0xdd, // vendor specific
            34, // tag length
            0x50, 0x6F, 0x9A,
            0x13, // NAN

            // 42: Master attribute - android uses 1, ESP will use 81 (infra, high) to save android bat life.
            0, 2, 0,
            129, 0,

            // 47: Cluster attribute
            1, 0x0d, 0,
            // 50 - master MAC, rand, pref
            2, 0xeb, 0x23, 0x0a, 0xc9, 0x31, // set to my address if anchor
            // 56 - Master random, master pref (default I am anchor)
            0x74, 129,
            // 58 - hops to master
            0,
            // 59 - time delta
            0, 0, 0, 0,

            // 63: Service ID list
            2, 6, 0,
            0x75, 0x94, 0x31, 0x93, 0xea, 0xc9,
            // 72
    };
    memcpy(head + FRAME_SRC, aware.mymac, 6);
    memcpy(head + 50, aware.mymac, 6);

    memcpy(head + FRAME_SSID + 4, aware.nanNet, 2);
    head[32] = interval & 0xFF;
    head[33] = (interval >> 8)  & 0xFF;

    head[45] = aware.masterPref;
    head[46] = aware.masterRandom;
    head[57] = aware.masterPref;
    head[56] = aware.masterRandom;

    // TODO: rand in master attribute
    // TODO: higher master rank (128)

    // TODO: if master is present, adjust cluster attribute, distance to master

    // If I am the anchor, should be my MAC, pref, etc.
    //memcpy(head + 49, aware.anchorMasterRank, 8);

    // unsigned long - 4 bytes !!
    //unsigned long time = currentTimeMicro();
    //char *tp = &time;
    // Last 4 seem to be 0

    esp_err_t err = esp_wifi_80211_tx(WIFI_IF_STA, head, sizeof(head), true);

    if (err != ESP_OK) {
        ESP_LOGI(TAG, "Error sending %d", err);
    }
}
