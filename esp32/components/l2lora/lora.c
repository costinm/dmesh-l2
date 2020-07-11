
#include <stdio.h>
#include <string.h>
#include <sys/time.h>
#include "esp_log.h"
#include "freertos/FreeRTOS.h"
#include "freertos/event_groups.h"

#include "lora.h"
#include "nvs.h"

#include "dmesh.h"

// 19 according to one spec, 23 in the old
#define BEACON_SIZE 23
#define FROM_LORA 0x20

static const char *TAG = "lora";

int lastLoraLen;
static uint8_t lorabuf[100];

static int beaconSize = BEACON_SIZE;

// Queue for the lora task
static xQueueHandle rcvQ;

static int lora_repeat_send = 0;
static char *lora_send_buf = "ping";
static int lora_send_len = 4;

//static char beaconOut[100];


void loraSend(int conId, char *c, int len) {
        lora_invert_iq(true);
        lora_explicit_header_mode();
        lora_set_spreading_factor(lora.sf);
        lora_set_coding_rate(lora.cr);
        lora_send_packet((uint8_t *) c, len);
}

// Based on nv-ram setting. Use on one device to turn it into a test ping
// In test mode will also receive.
// May also be used for powered device.

static void lora_task(void *pvParameters) {
    while (1) {
        vTaskDelay(10000 / portTICK_PERIOD_MS);

        loraSend(0, "ping", 4);

        if (lora.beaconReceive) {
            lora_receive_beacon(beaconSize);
        } else if (lora.dataReceive){
            lora_receive_upstream();
        }
    }
}

static void lora_handle_beacon() {
    lora.loraBeacons++;

    // Wait 2 min * 8 ch = 16 min for a beacon.
    // If a beacon is received - switch to packet mode, and read again the next beacon.
    //            I (734817) lora:   - 00 00 00 00 00  -
    //                                  00 44 5f 4b CRC 43 26
    //                                  RSV: 00
    //                                  51 2c 35
    //                                  93 44 a9
    //            I (734817) lora:          00

    // In sec since January 6, 1980 00:00:00 UTC, mod 2^32

    // Unix epoch is Jan 1, 1970 - in seconds. Lora ==  316008000
    // Lora 1264798720
    //lora_receive_upstream();
    // TODO: schedule task in 120 seconds
    lora.beaconBand++;
    if (lora.beaconBand > 7) {
        lora.beaconBand = 0;
    }
    if (lorabuf[0] == 0 && lorabuf[1] == 0) {
        unsigned long ts = lorabuf[8];
        ts = (ts << 8) + lorabuf[7];
        ts = (ts << 8) + lorabuf[6];
        ts = (ts << 8) + lorabuf[5];
        ts += 316008000; // Jan 6 1980
        time_t lorat = (time_t) ts;

        struct tm *loartm = gmtime(&lorat);
        char strftime_buf[64];
        struct tm timeinfo;

        strftime(strftime_buf, sizeof(strftime_buf), "%c", loartm);

        time_t now;
        time(&now);
        //setenv("TZ", "CST-8", 1);
        //tzset();

        localtime_r(&now, &timeinfo);
        strftime(strftime_buf, sizeof(strftime_buf), "%c", &timeinfo);

        ESP_LOGI(TAG, "BEACON: ts=%ld loraTime=%s %d", ts, strftime_buf, lora.beaconBand - 1);

        ESP_LOGI(TAG, "Current time: %s", strftime_buf);

        struct timeval now1 = {.tv_sec = ts};

        settimeofday(&now1, NULL);

        //ui_update(1);
        // TODO: CRC on timestamp
    }

    // TODO: switch to upstream after setting a timer for next
    lora_receive_beacon(beaconSize);
}

static void task_lora_read(void *arg) {
    uint32_t loraEvent;
    for (;;) {
        if (xQueueReceive(rcvQ, &loraEvent, portMAX_DELAY)) {
            printf("LORA[%d]\n", loraEvent);

            if (lora_received()) {
                int rssi = lora_packet_rssi();
                int snr = lora_packet_snr();
                lora.last.rssi = rssi;
                lora.last.snr = snr;
                lora.last.buf = (char *) lorabuf;

                int len = lora_receive_packet(lorabuf, 100);
                lora.last.len = len;

                lastLoraLen = len;
                ESP_LOGI(TAG, "LORA: received=%d rssi=%d snr=%d", len, rssi, snr);

                if (lora.onMessage != NULL) {
                    lora.onMessage((char *) lorabuf, len, lora.interfaceId);
                }

                if (lora.beaconReceive) {
                    lora_handle_beacon();
                } else if (lora.dataReceive){
                    lora.loraReceived++;
                    lora_receive_upstream();
                }
            }

            if (lora_repeat_send > 0) {
                lora_repeat_send--;
                lora_invert_iq(true);
                lora_explicit_header_mode();
                lora_set_spreading_factor(lora.sf);
                lora_set_coding_rate(lora.cr);
                lora_send_packet((uint8_t *) lora_send_buf, lora_send_len);

                if (lora_repeat_send > 0) {
                    // trigger another timer
                }
            }
        }
    }
}


void register_lora(nvs_handle_t nvs_handle) {
    lora_reset();

    //create a queue to handle gpio event from isr
    rcvQ = xQueueCreate(10, sizeof(uint32_t));

    int err = lora_init(rcvQ);
    if (err != ESP_OK) {
        return;
    }

    xTaskCreate(task_lora_read, "lora", 2048, NULL, 10, NULL);

    lora_set_sync_word(0x34);
    lora_set_preamble_length(8);

    lora_set_bandwidth(500000);

    lora_disable_crc();
    int32_t loraMode = 0;

    err = nvs_get_i32(nvs_handle, "lora", &loraMode);
    if (err == ESP_OK) {
        // TODO: Beacon mode if we don't have clock.
        // Device should sync with android/host to get clock, so this may not be needed
        //lora_receive_beacon(BEACON_SIZE);
        if (loraMode > 2) {
            xTaskCreate(&lora_task, "lora_task", 2048, NULL, 10, NULL);
        }
        if (loraMode == 1) {
            lora_receive_beacon(BEACON_SIZE);
        }
        if (loraMode == 2) {
            lora_receive_upstream();
        }
    }

    ESP_LOGI(TAG, "Lora init: %d", loraMode);
}
