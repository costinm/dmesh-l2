#pragma once

#ifndef ARDUINO_BOARD
#include "freertos/FreeRTOS.h"
#endif

#ifdef __cplusplus
extern "C" {
#endif

#ifdef PLATFORMIO
// WIP: stub missing features
#define ESP_LOGI(TAG, format, ... ) printf(format, ##__VA_ARGS__)
#define ESP_LOG_INFO 1
#define ESP_LOG_BUFFER_HEXDUMP(TAG, format, ...)
#define portTICK_PERIOD_MS portTICK_RATE_MS
#else
#include "freertos/event_groups.h"
#include "esp_log.h"
#include <nvs.h>
#include "esp_log.h"

#if defined(CONFIG_IDF_TARGET_ESP32)
#include "esp_console.h"
#include "argtable3/argtable3.h"
#define USE_CONSOLE 1
#endif

#include "esp_netif.h"
#include "esp_event.h"
#include "freertos/event_groups.h"

#endif


#include <lwip/ip4_addr.h>
#include "esp_wifi.h"



// Destination
#define FRAME_DST 4
// Source
#define FRAME_SRC 10
// SSID address
#define FRAME_SSID 16
// Offset of the data in the frame - for action frames starts with type.
#define FRAME_DATA 24

#define NAN_ACTION_START 30

typedef struct {
    char *data;
    int len;
} NanMessage;

#define OUT_QUEUE_SIZE 5

typedef struct {
    // Current MAC
    char mac[6];

    // We should expect it to be in receive state on the next beacon with same nanNet.
    uint8_t nanNet[2];

    // Required for follow-ups.
    uint8_t instanceId;

    // Public-key derived ID, also used in the IP6 address.
    //
    //char id[8];

    int discoveryBeacons;
    int syncBeacons;
    int inMessages;
    int outMessages;

    // Last time a packet of any time was received.
    unsigned long lastSeen;

    // Peer is added when the first beacon of frame is seen.
    // We will keep sending 'publish' until we receive a 'found' follow-up, then we can stop.
    unsigned long lastFoundReceived;

    NanMessage outQueue[OUT_QUEUE_SIZE];
    int outQueueLen;


} NanPeer;

#define NAN_MAX_PEERS 10

typedef void (*onMessageFunc)(char *data, int len, int interfaceId);

typedef enum {
  DISC_BEACON,
  SYNC_BEACON,
  SEND_WINDOW,
} TimerAct;

typedef struct {
    uint8_t mymac[6];
    NanPeer peers[NAN_MAX_PEERS];

    // Extracted from anchor master Sync beacons.
    uint8_t nanNet[2];

    int nanPackets;
    bool running;

    bool dump;
    int pubReceived;
    int subReceived;

    // received sync beacons in last interval - close
    int nanSBeaconClose;
    // received sync beacons in last interval - medium
    int nanSBeaconMedium;

    int nanSBeacon;

    // TODO: master preference based on power management option
    uint8_t masterPref;
    uint8_t masterRandom;

    // 6B - MAC of anchor master
    // 1B - random from AM
    // 1B - master pref.
    uint8_t anchorMasterRank[8];
    uint8_t hopsToMaster;
    // On last master beacon, master Timestamp - currentTime.
    uint8_t lastMasterTime[8];
    long timeDiffToMaster;

    // Last received Sync Beacon - from the anchor master
    unsigned long lastSBeacon;

    // Time when last Sync beacon was sent from this node.
    // Used to identify if we should send a new one or send a discovery, based on current time.
    // If we are master, this should also be used in the frame as tstamp.
    unsigned long lastSentSBeacon;

    // zero if we are the master
    unsigned long currentMaster;

    // Last received Discovery Beacon
    int nanDBeacon;
    unsigned long lastDBeacon;
    unsigned long lastSentDBeacon;

    int nanMessagesIn;
    int nanPacketsOthers;

    onMessageFunc onMessage;
    int interfaceId;

    TimerAct nextAction;

    // Bytes captured since last check (usually 0.5 sec/beacon - maybe 2 min ? )
    int capPackets;
    long capturedSinceLast;
    // TODO: stats on near and distant

    //bool master;
    //bool anchor;
} nan_driver;

extern nan_driver aware;

/**
 * Start Wifi in promiscuous mode, with a handler for NAN messages.
 * Nan callback must be set.
 */
void nanStart();

/*
 * Disable NAN and wifi.
 */
void nanStop();

/**
 * Send a message to all discovered devices
 */
void nanBroadcast(char *data, int len);

#ifdef USE_CONSOLE

void console_register_nan();


// Helpers

unsigned long int currentTimeMicro();

#endif

#ifdef __cplusplus
}
#endif
