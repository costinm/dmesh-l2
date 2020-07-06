// L3 - routing
// Will receive callbacks from L2 - NAN, BLE, BT, Lora, etc.
// Will send messages to L2
//
// No ACK/retry - that is L4
// Sessions - i.e. longer lived circuits, sessions are L5
// No encryption - this is L6
// Commands or services are L7 - the app will need to handle them and use the L6 libraryes for authz/authn

#include <stdio.h>
#include <string.h>
#include "esp_log.h"
#include "esp_console.h"
#include "argtable3/argtable3.h"
#include "freertos/FreeRTOS.h"
#include "freertos/event_groups.h"
#include "esp_wifi.h"
#include "dmesh.h"

#define TAG "dm"

/* TODO: L7/L6 - control, encryption fo the device.
 *
 * - generate P256 key pair
 * - register or configure CA
 * - get pubkey of 'master' and/or cert
 * - webpush encryption for all outgoing messages
 *
 */

// Current code is forwarding frames as-is - no L6 encryption. It acts as a repeater, L3
// No 'owner' device, zero trust in the ESP32 device.

Status status;

//static char printbuf[100];
//void dump(const char *tag, uint8_t* buf, int len) {
//    int i = 0;
//    for (i = 0; i < len; i++) {
//    sprintf(printbuf + (i % 16) * 3, "%02x ", *((uint8_t *) buf + i));
//
//    if ((i + 1) % 16 == 0) {
//        ESP_LOGI(tag, "  - %s", printbuf);
//    }
//    }
//    if ((i % 16) != 0) {
//        printbuf[((i) % 16) * 3 - 1] = 0;
//        ESP_LOGI(tag, "  - %s", printbuf);
//    }
//
//}

void onMessage(char *c, int len, int from) {
    ESP_LOGI(TAG, "MSGIN: %d %d", from, len);
    //dump(TAG, (u_int8_t *)c, len);
    ESP_LOG_BUFFER_HEXDUMP(TAG, c, len, ESP_LOG_INFO);

    if (c[0] == 'l') {
        loraSend(0, c, len);
    }

    // TODO: map of recent message IDs, hop count
    //
    bleSend(0, c, len);
    btSend(0, c, len);
    //nanSendNow(0, c, len);
}

void timerCB(void* arg) {
//    //ESP_LOGI(TAG, "Timer");
//    ESP_LOGI(TAG, "NAN: p/s=%d/%d p/s/d=%d/%d/%d m/o=%d/%d",
//            NanService.pubReceived, NanService.subReceived,
//            NanService.nanPackets, NanService.nanSBeacon, NanService.nanDBeacon,
//            NanService.nanMessagesIn, NanService.nanPacketsOthers);

    // TODO: send a discovery beacon on Wifi, lora, BLE adv
    //nanSendSync(0, "", 0);
//    vTaskDelay(10 / portTICK_PERIOD_MS);
    //nanSendPublish(0, "ping", 4);
    //nanSendNow(0, "ping", 4);


}

esp_timer_handle_t timerOneOff;

void register_dmesh(nvs_handle_t nvs_handle) {
    // high res timer
    esp_timer_init();

    esp_timer_create_args_t args = {
        name: "dmesh_t",
        callback: &timerCB,
        arg: "1",
    };
    ESP_ERROR_CHECK(esp_timer_create(&args, &timerOneOff));

    esp_timer_stop(timerOneOff);
    // 5 sec in us
    //esp_timer_start_once(timerOneOff,5 * 1000000);
    esp_timer_start_periodic(timerOneOff,5 * 1000000);

//    esp_timer_stop();
//    esp_timer_start_periodic();

    esp_timer_dump(stdout);

}