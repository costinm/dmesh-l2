
#include <stdio.h>
#include <string.h>
#include <time.h>
#include "nan.h"

#include "esp_wifi.h"

static const char *TAG = "nan";


nan_driver aware;

#ifndef PLATFORMIO

/*
 * This is the (currently unofficial) 802.11 raw frame TX API,
 * defined in esp32-wifi-lib's libnet80211.a/ieee80211_output.o
 *
 * This declaration is all you need for using esp_wifi_80211_tx in your own application.
 */
//esp_err_t esp_wifi_80211_tx(wifi_interface_t ifx, const void *buffer, int len, bool en_sys_seq);

bool init = false;
static EventGroupHandle_t nan_event_group;
static const int START_BIT = BIT1;
#endif
// TODO: what is the equivalent for PLATFORMIO for send ?
// we can add a sleep instead of event group.

// My instance ID - in discovery, follow up, etc.
#define NAN_ID 1

// TYPE_SUBTYPE:
// Last 2 bits are 0
// bit 2-3 are 0 for management
#define TYPE_BEACON 0x80
#define TYPE_ACTION 0xD0

static int cnt;

// Timer management
static long startupTime;
static long nextActivity;
static int timerState;
static esp_timer_handle_t timerOneOff;

// last time we dumped the stats
static long lastStats;

// Frame header for Nan actions.
static const uint8_t nanHeader[] = {
        0xD0, 0x00, // type/sub
        0x00, 0x00, // duration
        /* 4: DST */
        0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF,
        //0x51, 0x6F, 0x9A, 0x01, 0x00, 0x00,
        /* 10: SRC */
        0x00, 0x00, 0x00, 0x00, 0x00, 0x00, // will be set to this device addr
        /* 16 BSSID */
        0x50, 0x6F, 0x9A, 0x01, 0x05, 0x01, // NAN BSSID
        /* 22: SEQ, FRAG */
        0, 0,

        // 24: DATA, Action (vendor, NAN, Discovery)
        0x04, 0x09, // action, vendor // 26
        0x50, 0x6F, 0x9A,
        0x13, // NAN
};

static const u_int8_t nanDeviceCapabilities[] = {
        0x0F, 0x09, 0x00,
            0, 1, 0, // only 2.5GHz
            // bands
            0x04, 1, 0, 0, 0x14, 0,

};

static const u_int8_t nanAvailability[] = {
            // 42: (18) Nan availability
            0x12, 0x1b, 0x00,
            0x0b, 0x01, 0x00, 0x16, 0x00, 0x1a, 0x10, 0x18, 0x00, 0x04, 0xfe,
            0xff, 0xff, 0x3f, 0x31, 0x51, 0xff, 0x07, 0x00, 0x80, 0x20, 0x00, 0x0f, 0x80, 0x01, 0x00, 0x0f,
};

static const u_int8_t nanServiceExtension[] = {
            // 72: (14) Service Extension attribute
            0x0e, 0x04, 0x00,
            NAN_ID, 0x00, 0x02, 0x02,
};

static const u_int8_t nanServiceDescriptor[] = {
        // 79: (3) Service descriptor
            0x03, 0x1A, 0x00,
            // DMesh Service ID
            0x75, 0x94, 0x31, 0x93, 0xea, 0xc9,
            // 88: InstanceID
            NAN_ID,
            // 89: requestor
            0, // extracted from Sub request
            // 90: control, SI present
            0x10, // 0x10 for publish, 0x11 for subscribe type
            // 91: len
            0x10,
            // 92: Data, max 255 bytes
            0x31, 0x32, 0x33, 0x34, 0x35, 0x36, 0x37, 0x38, // MAC
            0x30, 0x30, 0x30, 0x30, // Future
            0x57, 0x78, 0x68, 0x37, // Local ID (string4)
};

// "l3dmesh" service id hash.
// Used to indicate support for mesh forwarding.
// TODO: add additional services for special configurations, for example ESP8266 with lower
// MTU.
const uint8_t SVC_ID[] = {0x75, 0x94, 0x31, 0x93, 0xea, 0xc9};

// We use a single thread to send/receive.
static uint8_t sendBuffer[512];

static unsigned long maxPostSync;

/**
 * Return time in micro.
 *
 * clock() on ESP returns time in millisecond - XSI/linux is in uS.
 *
 * @return
 */
unsigned long int currentTimeMicro() {
    return (uint64_t) esp_timer_get_time();
//    struct timespec ts;
//    clock_gettime(CLOCK_MONOTONIC, &ts);
//    unsigned long ns = ts.tv_nsec / 1000;
//
//    return ns + ts.tv_sec * 1000000;
}

#define currentTimeMillis() clock()

static int sendBufferOff;

/**
 * Send Publish Action frame.
 *
 * @param type 0 for publish, 1 for subscribe
 * @data, @len - payload for service info
 */
void nanSendPublish(int type, int requestorEndpoint, char *data, int len) {
    uint8_t *head = sendBuffer;
    sendBufferOff = 0;

    memcpy(sendBuffer, nanHeader, sizeof(nanHeader));
    // Source address
    memcpy(head + 10, aware.mymac, 6);
    // BSSID update
    memcpy(head + FRAME_SSID + 4, aware.nanNet, 2);

    sendBufferOff = sizeof(nanHeader);

    memcpy(sendBuffer + sendBufferOff, nanDeviceCapabilities, sizeof(nanDeviceCapabilities));
    sendBufferOff += sizeof(nanDeviceCapabilities);

    memcpy(sendBuffer + sendBufferOff, nanAvailability, sizeof(nanAvailability));
    sendBufferOff += sizeof(nanAvailability);

    memcpy(sendBuffer + sendBufferOff, nanServiceExtension, sizeof(nanServiceExtension));
    sendBufferOff += sizeof(nanServiceExtension);

    memcpy(sendBuffer + sendBufferOff, nanServiceDescriptor, sizeof(nanServiceDescriptor));
    // Update the Serv. extension counter
    //head[78] = (uint8_t) cnt++;

    sendBufferOff += sizeof(nanServiceDescriptor);

    // TODO: device ID from nvram, autogen

    // TODO: safe to remove ?
    //head[89] = requestorEndpoint;

//    if (type == 0) {
//        // Subscribe received, send publish message
//        head[51] = 0x10; // TODO extract
//        head[52] = 0x80;
//    }

    unsigned long now = (unsigned long) (currentTimeMicro());
    esp_wifi_80211_tx(WIFI_IF_STA, head, sendBufferOff, true);
    unsigned long now2 = (unsigned long) (currentTimeMicro());

    unsigned long ttime = now2 - now;
    unsigned long stime = (now - aware.lastSBeacon);

    // Len is ~100
    // ttime is in usec - typically around 80, can be larger if medium busy
    if (ttime < 50 || ttime > 140 || stime > maxPostSync) {
        ESP_LOGI(TAG, "Sent pub t=%lu s=%lu", ttime, stime);
    }
    if (stime > maxPostSync) {
        maxPostSync = stime;
    }
}

/**
 * Queue a message to a peer.
 *
 * Will be sent in the next window for the peer's cluster.
 *
 * Note that android requires publish/subscribe, in order to track the device ID and service ID.
 *
 * TODO: schedule a send based on the availability.
 *
 * @param peer
 * @param data
 * @param len
 */
void nanQueue(NanPeer *peer, char *data, int len) {
    int l = peer->outQueueLen;
    if (l >= OUT_QUEUE_SIZE) {
        return;
    }
    peer->outQueue[l].data = data;
    peer->outQueue[l].len = len;
    peer->outQueueLen++;
}

/**
 * Immediately send nan messages to a peer.
 *
 * @param conId
 * @param dstMac
 * @param instanceId
 * @param data
 * @param len
 */
static void nanSendNow(char *dstMac, uint8_t instanceId, char *data, int len) {
    if (len > 255) {
        len = 255;
    }
    memcpy(sendBuffer, nanHeader, sizeof(nanHeader));

    uint8_t *head = sendBuffer;
    memcpy(head + FRAME_DST, dstMac, 6);
    memcpy(head + FRAME_SRC, aware.mymac, 6);
    memcpy(head + FRAME_SSID + 4, aware.nanNet, 2);

    int i = sizeof(nanHeader);

    // 30: 0x03, 0xxx, 0x00,
    head[i++] = 0x03; // Service descriptor
    int sz = len + 6 + 4;
    head[i++] = sz % 256;
    head[i++] = sz / 256;
    // 33: payload
    // DMesh Service ID
    memcpy(head + i, SVC_ID, 6);
    i += 6;
    // 38
    head[i++] = NAN_ID; // My instance ID

    head[i++] = instanceId; // remote
    // 40
    head[i++] = 0x12; // control
    head[i++] = (uint8_t) len;
    memcpy(head + i, data, len);
    i += len;


    unsigned long now = (unsigned long) (currentTimeMicro());
    // Android sends multiple times.
    //for (int x = 0; x < 2; x++) {
    esp_wifi_80211_tx(WIFI_IF_STA, head, i, true);
    //}
    unsigned long now2 = (unsigned long) (currentTimeMicro());

    unsigned long ttime = now2 - now;
    // Typical: 87..91ms for now2
    unsigned long stime = (now - aware.lastSBeacon);
    // Typical: , seen 470, 7500
    // Len is 48
    if (ttime < 50 || ttime > 150) {
        //if (NanService.dump) {
        ESP_LOGI(TAG, "Sent fol tt=%lu sb=%lu len=%d", ttime, stime, i);
        //}
    }
    if (aware.dump) {
        ESP_LOG_BUFFER_HEXDUMP("OUT", head, i, ESP_LOG_INFO);
    }
}


/**
 * Handle Sync or Discovery beacons.
 *
 * The format is mostly the same - difference is that on Sync we need to transmit data.
 *
 * @param rx_ctrl
 * @param peer - the peer for which the frame was received.
 * @param frame
 * @param len
 */
static void handleBeacon(wifi_pkt_rx_ctrl_t *rx_ctrl, NanPeer *peer, uint8_t *frame, uint32_t len) {
    // ts: frame[24:8]
    unsigned long now = (unsigned long) (currentTimeMicro());
    peer->lastSeen = now;
    // - 0 master pref - 1, random
    // - 1 cluster - hop, rank
    // - 2 service ID
    // - 40 subscribe service ID

    // Current discovery frame determines the network.
    // TODO: may result in flip-flops if other devices don't converge. Should use RSSI and
    // number of devices on that net to determine if we join the cluster.
    memcpy(aware.nanNet, frame + FRAME_SSID + 4, 2);

    // Update the device, multiple networks can be supported (we may not join its cluster)
    memcpy(peer->nanNet, frame + FRAME_SSID + 4, 2);



    // Discovery and Sync are identical in content, except duration.
    // Sync beacons use 0x200 interval, discovery 0x64
    if (frame[FRAME_DATA + 8] == 0 && frame[FRAME_DATA + 9] == 2) {
        // Sync Beacon - Discovery frame, at 512 ms

        // TODO: check l3dmesh service ID: 75:94:31:93:ea:c9
        // TODO: update the specific device

        aware.nanSBeacon++;
        peer->syncBeacons++;
        aware.lastSBeacon = now;


        // TODO: send unsolicited publish and subscribe only
        // - if beacon shows the service ID as passive pub or sub
        // - after some delay or in next timer
        // - no more than 3
        nanSendPublish(0, (char *)frame + 10,  "", 0);

        // TODO: grab wake lock, schedule timer in 512 ms (minus wifi startup time) for next window
        // TODO: schedule timer in 16 ms to release wake lock

        // TODO: if we have queued messages for any other device, send now

        // Send queued message for this peer.
        if (peer->outQueueLen > 0) {
            nanSendNow(peer->mac, aware.peers[0].instanceId,
                       peer->outQueue[0].data, peer->outQueue[0].len);
            peer->outQueueLen--;
        }

    } else {
        // TODO: compare the sender and cluster with our anchor master
        // TODO: if RSSI better than anchor master - consider switching clusters
        aware.nanDBeacon++;
        aware.lastDBeacon = now;
        peer->discoveryBeacons++;
    }
}

const uint8_t resBuf[64];

/**
 * Received a pub, sub or data message for this device.
 */
static void handleDAF(wifi_pkt_rx_ctrl_t *rx_ctrl, NanPeer *peer, uint8_t *frame, uint32_t len, unsigned long now2) {

    // In order to communicate with this device we must use the same network ID.
    // Publish and SDF will use it.
    // TODO: pick the network ID with stronger signal and use it for sending our own sync beacons.

    // Technically it is possible to exchange messages with multiple networks - in particular
    // if this device is not sleeping.
    // It means we could exchange messages with multiple clusters.
    // TODO: track multiple clusters, based on signal strength.

    // For now use the cluster from the last message, assume single cluster
    memcpy(aware.nanNet, frame + 20, 2);
    memcpy(peer->nanNet, frame + FRAME_SSID + 4, 2);

    if (aware.dump) {
        ESP_LOGI(TAG, "NANActionFrame len=%d rssi=%d rate=%d mcs=%d ch=%d from=%2x:%2x:%2x:%2x:%2x:%2x", len,
                 rx_ctrl->rssi, rx_ctrl->rate,
                 rx_ctrl->mcs, rx_ctrl->channel,
                 frame[10], frame[11], frame[12], frame[13], frame[14], frame[15]
                 );
    }
    int next = NAN_ACTION_START;
    int i;
    while (next < len - 4) {
        i = next;
        int tag = frame[i++]; // Type
        int tl = frame[i++]; // Length of the frame
        tl = 256 * frame[i++] + tl;

        next = i + tl; // next frame

        if (tl == 0 || i + tl > len) {
            ESP_LOGI(TAG, "Overflow TAG %d %d %d %d ", i, next, tag, tl);
            ESP_LOG_BUFFER_HEXDUMP(TAG, frame, len, ESP_LOG_INFO);
            return;
        }
        if (aware.dump) {
            ESP_LOGI(TAG, "TAG i=%d next=%d tag=%d tl=%d ", i, next, tag, tl);
        }

        // 3 Service descriptor
        if (tag == 3) {
            // 6 bytes service ID (TODO: match)
            i += 6;
            int instanceId = frame[i++];// InstanceID
            peer->instanceId = instanceId;
            // RequestorInstanceID - should be 0 or my own instanceID (1)
            int requestorId = frame[i++];
            // ServiceControl == 0x12 - follow up (2), SI present (10)
            int serviceControl = frame[i++];
            // Len 1B
            uint8_t tlen = frame[i++];
            // Data (<256)
            // frame+i - data, tlen size

            memcpy(peer->mac, frame + FRAME_SRC, 6);
            if (serviceControl == 0x11) {
                // Active Subscribe, with SI present
                nanSendPublish(0, peer->instanceId, "", 0);
                //nanSendNow(0, (char *) frame + FRAME_SRC, peer->instanceId, "PINGs", 5);
                //ESP_LOGI(TAG, "NAN Sub received, send publish");
                aware.subReceived++;
            } else if (serviceControl == 0x10) {
                // Active Publish, with SI present
                nanSendNow((char *) frame + FRAME_SRC, peer->instanceId, "PINGp", 5);
                nanSendPublish(1, peer->instanceId, "", 0);
                //ESP_LOGI(TAG, "NAN Pub received, send Sub");
                aware.pubReceived++;
            } else {

                // Action Frame received
                aware.nanMessagesIn++;

                if (0 == memcmp("PING", frame + i, 4)) {
                    //nanSendNow(0, (char *) BROADCAST, peer->instanceId, "PONGB", 5);
                    memcpy(resBuf, "PONG", 4);
                    memcpy(resBuf + 4, frame + i + 4, tlen);
                    nanSendNow((char *) frame + FRAME_SRC, peer->instanceId,
                            resBuf, tlen + 4);
                }

                if (0 != memcmp(frame + FRAME_DST, aware.mymac, 6)) { // Not for me
                    aware.nanPacketsOthers++;
                    if (aware.dump) {

                        ESP_LOG_BUFFER_HEXDUMP("NAN-other", frame, len, ESP_LOG_INFO);
                        ESP_LOG_BUFFER_HEXDUMP("NAN-other", aware.mymac, 6, ESP_LOG_INFO);
                    }
                    //  return;
                }

                unsigned long afterSend = (unsigned long) (currentTimeMicro());

                frame[i + tlen] = 0;
                if (aware.dump) {
                ESP_LOGI(TAG, "NAN MSG IN from=%2x:%2x:%2x:%2x:%2x:%2x rssi=%d l=%d sb=%d sr=%d %s",
                        frame[10], frame[11], frame[12], frame[13], frame[14], frame[15],
                        rx_ctrl->rssi,
                        tl,
                         (unsigned int) (now2 - aware.lastSBeacon),
                         (unsigned int) (afterSend - now2), frame + i);
                    ESP_LOG_BUFFER_HEXDUMP("IN", frame, len, ESP_LOG_INFO);
                    //ESP_LOG_BUFFER_HEXDUMP(TAG, frame + i, tlen, ESP_LOG_INFO);
                }
            }
        } else {
            if (aware.dump) {
                ESP_LOGI(TAG, "NAN Action tag: %d %d", tag, tl);
                ESP_LOG_BUFFER_HEXDUMP("TAG", frame, tl, ESP_LOG_INFO);
            }
        }
    }

    if (aware.dump) {
        ESP_LOG_BUFFER_HEXDUMP(TAG, frame, len, ESP_LOG_INFO);
    }

}

// Wifi direct support - Action frames with the standard format.
//
static void sniffer_cb1(void *buf, wifi_promiscuous_pkt_type_t type) {
    unsigned long now2 = (unsigned long) (currentTimeMicro());

    wifi_promiscuous_pkt_t *promPkt = (wifi_promiscuous_pkt_t *) buf;

    wifi_pkt_rx_ctrl_t *rx_ctrl = &promPkt->rx_ctrl; //(wifi_pkt_rx_ctrl_t *) buf;

    uint8_t *frame = &promPkt->payload; //(uint8_t *) (rx_ctrl + 1);

#if defined(CONFIG_IDF_TARGET_ESP8266)
    uint32_t len = rx_ctrl->sig_mode ? rx_ctrl->HT_length : rx_ctrl->legacy_length;
#else
    uint32_t len = rx_ctrl->sig_len;
#endif

    aware.capturedSinceLast += len;

    uint32_t i;
    uint8_t total_num = 1, count = 0;
    uint16_t seq_buf = 0;

    aware.capPackets++;

    if (type != WIFI_PKT_MGMT) {
        return;
    }

    // BSSID == nanDriver. Last 2 are random
    if (frame[16] != 0x50 || frame[17] != 0x6f || frame[18] != 0x9a) {
        return;
    }
    // frame[0] == 0xd0 - action
    // frame[0] == 0x80 - beacon

    // TODO: find peer by sender MAC
    NanPeer *peer = &aware.peers[0];

    if (frame[0] == 0x80) {
        handleBeacon(rx_ctrl, peer, frame, len);
        return;
    }

    if (frame[0] != 0xd0) {
        ESP_LOG_BUFFER_HEXDUMP("nan-unknown", frame, len, ESP_LOG_INFO);
        return;
    }
    // SDF
    if (frame[24] != 4 || // public action
        frame[25] != 9 || // vendor
        frame[26] != 0x50 || frame[27] != 0x6f || frame[28] != 0x9a) { // NAN
        // TODO: capture the types and add method to dump
        ESP_LOG_BUFFER_HEXDUMP("nan-unknown", frame, len, ESP_LOG_INFO);
        ESP_LOGI(TAG, "NotNAN len=%d rssi=%d rate=%d mcs=%d ch=%d %x %x %x", len, rx_ctrl->rssi, rx_ctrl->rate,
                 rx_ctrl->mcs, rx_ctrl->channel,
                 frame[24], frame[25], frame[26]);
        return;
    }

    aware.nanPackets++;
    if (frame[29] != 19) { // 0x13
        ESP_LOGI(TAG, "NotSDF len=%d rssi=%d rate=%d mcs=%d ch=%d %x %x %x", len, rx_ctrl->rssi, rx_ctrl->rate,
                 rx_ctrl->mcs, rx_ctrl->channel,
                 frame[24], frame[25], frame[26]);
        return;
    }

    handleDAF(rx_ctrl, peer, frame, len, now2);
}

void nanSendSync(int interval);

static void timerCB(void *arg) {
    if (!aware.running) {
        return;
    }
    long now = currentTimeMicro();

//    if (now - aware.lastDBeacon < 110) {
//        aware.anchor = false;
//    } else {
//        // TODO: compute anchor properly, based on received beacons and priorities
//        // TODO: adjust random, rotate master
//        aware.anchor = true;
//    }

    bool syncNeeded = (now - aware.lastSentSBeacon) > 490;
//    bool discoveryNeeded = aware.anchor && ((now - aware.lastSentDBeacon) > 90) && !syncNeeded;

    // The timer should wake us up at the right moment for next action - sending disc, sync or
    // an availability window.
    // We can use a state to indicate what this window is for - but it also depends on other packets
    // received in between. So just determine what needs to be done.

    if (syncNeeded) {
        // No other master, maybe we are master ?

        //if (now - aware.lastSentSBeacon > 490) {
            // TODO: count of SBeacons, take RSSI into account, 3 beacons.
            aware.lastSentSBeacon = now;
            nanSendSync(512);
            //ESP_LOGI(TAG, "No beacon received, sending %ld %ld", now - aware.lastSBeacon, now - aware.lastSentSBeacon);
        //}
    }

//    if (discoveryNeeded) {
//        // TODO: count of SBeacons, take RSSI into account, 3 beacons.
//        nanSendSync(100);
//        aware.lastSentDBeacon = now;
//    }

    if (now - lastStats > 10 * 1000 * 1000) {
        ESP_LOGI(TAG, "NAN: p/s=%d/%d p/s/d=%d/%d/%d m/o=%d/%d %ld",
                 aware.pubReceived, aware.subReceived,
                 aware.nanPackets, aware.nanSBeacon, aware.nanDBeacon,
                 aware.nanMessagesIn, aware.nanPacketsOthers, aware.capturedSinceLast);
        lastStats = now;
        aware.capturedSinceLast = 0;
    }

    long nextWakeup = now + 512 * 1024;

//    if (aware.anchor) {
//        nextWakeup = now + 100 * 1000;
//    }
    long delta = nextWakeup - currentTimeMicro();
    if (delta < 1000) {
        delta = 100 * 1000;
    }

#if defined(CONFIG_IDF_TARGET_ESP8266)
    hw_timer_alarm_us(delta, false);
    hw_timer_enable(true);
#else
    esp_timer_start_once(timerOneOff, delta);
#endif
}


static void nanTimer() {
    startupTime = currentTimeMicro();

#if defined(CONFIG_IDF_TARGET_ESP8266)
    hw_timer_init(timerCB, "1");
    hw_timer_alarm_us(500 * 1000, false);
    hw_timer_enable(true);
#else
    // Must be called in main
    // TODO: grab wake lock if pm enabled
    esp_timer_init();

    esp_timer_create_args_t args = {
            name: "nan_t",
            callback: &timerCB,
            arg: "1",
    };
    ESP_ERROR_CHECK(esp_timer_create(&args, &timerOneOff));

    // TODO: compute time of next wakeup - next avail of a device with pending writes

    esp_timer_stop(timerOneOff);
    // 5 sec in us - give it time to receive beacons to sync
    esp_timer_start_once(timerOneOff, 5 * 1000000);
    //esp_timer_start_periodic(timerOneOff,5 * 1000000);
#endif
}

static void nanStartSniffing() {

    // Only need ACTION and BEACON frames
    wifi_promiscuous_filter_t sniffer_filter = {filter_mask: WIFI_PROMIS_FILTER_MASK_MGMT};

#if CONFIG_FILTER_MASK_DATA_FRAME_PAYLOAD
    // May need DATA for Nan connections
    sniffer_filter.filter_mask |= WIFI_PROMIS_FILTER_MASK_DATA;

    /*Enable to receive the correct data frame payload*/
    extern esp_err_t esp_wifi_set_recv_data_frame_payload(bool enable_recv);
    ESP_ERROR_CHECK(esp_wifi_set_recv_data_frame_payload(true));
#endif

    //ESP32: esp_wifi_internal_set_rate();
    ESP_ERROR_CHECK(esp_wifi_set_promiscuous_rx_cb(sniffer_cb1));
    ESP_ERROR_CHECK(esp_wifi_set_promiscuous_filter(&sniffer_filter));

    // Default bg, supports bgn ( no n or gn)
//    ESP_ERROR_CHECK(esp_wifi_set_protocol(ESP_IF_WIFI_STA, WIFI_PROTOCOL_11B | WIFI_PROTOCOL_11G |
//        WIFI_PROTOCOL_11N));

    ESP_ERROR_CHECK(esp_wifi_start());

    // TODO: wait callback ready
    ESP_LOGI(TAG, "Wait Wifi event");
    xEventGroupWaitBits(nan_event_group, START_BIT, false, true, 2000); //portMAX_DELAY);
    // Get MAC address for WiFi station
    esp_read_mac(aware.mymac, ESP_MAC_WIFI_STA);
    ESP_ERROR_CHECK(esp_wifi_set_promiscuous(true));

    ESP_LOGI(TAG, "Starting NAN %02x:%02x:%02x:%02x:%02x:%02x",
             aware.mymac[0],
             aware.mymac[1],
             aware.mymac[2],
             aware.mymac[3],
             aware.mymac[4],
             aware.mymac[5]);

    ESP_ERROR_CHECK(esp_wifi_set_channel(6, 0));

    // Only works when scanning, for beacons. Mostly useless
    // Doesn't support action frames...
    //uint8_t ie[] = {WIFI_VENDOR_IE_ELEMENT_ID, 32, 0x50, 0x6F, 0x9A, 0x13};
    //ESP_ERROR_CHECK(esp_wifi_set_vendor_ie_cb(vendor_cb, NULL));
    //ESP_ERROR_CHECK_WITHOUT_ABORT(esp_wifi_set_vendor_ie(true, WIFI_VND_IE_TYPE_BEACON, WIFI_VND_IE_ID_0, ie));

    //esp_wifi_80211_tx
    aware.running = true;
    nanTimer();
    ESP_LOGI(TAG, "NAN reader started");
}

#if !defined(CONFIG_IDF_TARGET_ESP8266)
static esp_netif_t *sta_netif = NULL;

static void event_handler(void *arg, esp_event_base_t event_base,
                          int32_t event_id, void *event_data) {

    xEventGroupSetBits(nan_event_group, START_BIT);
}

#endif

void nanStart() {
    if (!init) {
        nan_event_group = xEventGroupCreate();
        init = true;
    }

    // Infra - see TODO
    aware.masterPref = 129;

    aware.masterRandom = rand();

#if !defined(CONFIG_IDF_TARGET_ESP8266)
    ESP_ERROR_CHECK(esp_event_handler_register(WIFI_EVENT, WIFI_EVENT_STA_START, &event_handler, NULL));

    sta_netif = esp_netif_create_default_wifi_sta();
    assert(sta_netif);
#endif
    ESP_ERROR_CHECK_WITHOUT_ABORT(esp_netif_init());

    wifi_init_config_t cfg = WIFI_INIT_CONFIG_DEFAULT();
    ESP_ERROR_CHECK(esp_wifi_init(&cfg));
    ESP_ERROR_CHECK(esp_wifi_set_storage(WIFI_STORAGE_RAM));

    // Can't change channel
    // ESP_ERROR_CHECK(esp_wifi_set_mode(WIFI_MODE_NULL));
    ESP_ERROR_CHECK(esp_wifi_set_mode(WIFI_MODE_STA));

    nanStartSniffing();
}


void nanStop() {
    ESP_ERROR_CHECK(esp_wifi_set_promiscuous(false));
    ESP_ERROR_CHECK(esp_wifi_stop());
    ESP_ERROR_CHECK(esp_wifi_deinit());

#if !defined(CONFIG_IDF_TARGET_ESP8266)
    ESP_ERROR_CHECK(esp_event_handler_unregister(WIFI_EVENT, WIFI_EVENT_STA_START, &event_handler));

    if (sta_netif != NULL) {
        esp_netif_destroy(sta_netif);
        sta_netif = NULL;
    }
#endif
}

#ifdef USE_CONSOLE
/** Arguments used by 'join' function */
static struct {

    struct arg_lit *mon;

    struct arg_lit *stop;
    struct arg_str *send;

    struct arg_end *end;
} args;

static int cmd_nan(int argc, char **argv) {
    int nerrors = arg_parse(argc, argv, (void **) &args);
    if (nerrors != 0) {
        arg_print_errors(stderr, args.end, argv[0]);
        return 1;
    }

    if (args.stop->count) {
        nanStop();
        return 0;
    }

    if (args.mon->count) {
        nanStart();
        return 0;
    }

    if (args.send->count) {
        nanQueue(&aware.peers[0], *args.send->sval, strlen(*args.send->sval));

        return 0;
    }

    return 0;
}

void console_register_nan() {
    args.mon = arg_lit0(NULL, "start", "Start NAN");
    args.stop = arg_lit0(NULL, "stop", "Stop NAN, disable wifi");
    args.send = arg_str1("s", "send", "<s>", "Send NAN");

    args.end = arg_end(0);

    const esp_console_cmd_t nan_cmd = {
            .command = "nan",
            .help = "WiFi AP, station or monitor",
            .hint = NULL,
            .func = &cmd_nan,
            .argtable = &args
    };

    ESP_ERROR_CHECK(esp_console_cmd_register(&nan_cmd));
}

#endif



