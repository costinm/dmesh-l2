/*
   This example code is in the Public Domain (or CC0 licensed, at your option.)

   Unless required by applicable law or agreed to in writing, this
   software is distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR
   CONDITIONS OF ANY KIND, either express or implied.
*/
#pragma once

#include <stdint.h>
#include <nvs.h>
#include <esp_pm.h>
#include "freertos/FreeRTOS.h"
#include "freertos/event_groups.h"


#ifdef __cplusplus
extern "C" {
#endif

#include "driver/uart.h"


typedef void (*onMessageFunc)(char *data, int len, int interfaceId);
typedef void (*sendFunc)(char *data, int len, int interfaceId);

typedef struct {
    onMessageFunc onMessage;
    int interfaceId;
    sendFunc send;

} l2_driver;

/**
 * Common fields for peers.
 */
typedef struct {
    l2_driver *iface;

    char mac[6];

    int inMessages;
    int outMessages;

    // In micros
    unsigned long lastSeen;

    // spp_handle for bt
    // service instance for NAN
    //
    uint32_t handle;

} l2_peer;


void onMessage(char *c, int len, int from);
void onPeer(int id, int status, int phy);



// In some cases, code assumes there is a single client
void btSend(int conId, char *data, int len);
void nanSend(int conId, char *dstMac, char *data, int len);
void bleSend(int conId, void *data, int size);
void loraSend(int conId, char *c, int len);


void nanSendSync(int conId, char *data, int len);

int8_t free80211_send(uint8_t *buffer, uint16_t len);

// Declared in wifi.c.
extern const int START_BIT;
extern EventGroupHandle_t wifi_event_group;
void initialise_wifi();

void ui_update(int line);

// Register l3dmesh functions for console
void register_dmesh();
void register_dmesh_nvs();
void register_dmesh_system();
void register_lora(nvs_handle_t nvs_handle);
void register_i2ctools();
void register_ui(nvs_handle_t nvs_handle);
void register_gpio();
void register_ble(nvs_handle_t nvs_handle);
void register_spp_ble(nvs_handle_t nvs_handle);
void register_hci_ble(nvs_handle_t nvs_handle);
void register_wifi(nvs_handle_t nvs_handle);

int mon(int argc, char **argv);

//void dump(const char *tag, uint8_t* buf, int len);

typedef struct {
//    char* packet;
//    int packetSize;
//    int rssi;
//    int snr;

//    int capPackets;
//    int nanPackets;
//    int nanSBeacon;
//    int nanDBeacon;
//    int nanAction;

//    int id;
//    int from;
//    int lost;
//    int type;

    // uint64_t chipId;
//    uint8_t* chipId;
  //  int outId;

    //int16_t bat;
    //int hal;

    //uint8_t beaconMode;

//    int loraBeacons;
//    int loraReceived;
//    int loraSent;

    int loraTime;

    uint8_t bleConnected;

    esp_pm_lock_handle_t light_sleep_pm_lock;

} Status;

extern Status status;

#ifdef __cplusplus
}
#endif
