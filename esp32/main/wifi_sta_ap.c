// Standard Wifi AP and STA, using the ESP binary blobs.

#include <stdio.h>
#include <string.h>
#include <nvs.h>
#include <esp_sntp.h>
#include "esp_log.h"
#include "esp_console.h"
#include "argtable3/argtable3.h"
#include "freertos/FreeRTOS.h"
#include "freertos/event_groups.h"
#include "esp_wifi.h"
#include "esp_netif.h"
#include "esp_event.h"
#include "dmesh.h"

#define JOIN_TIMEOUT_MS (10000)

static const char *TAG = "dmwifi";

#define MAC_HEADER_LEN 24
#define MAC_HDR_LEN_MAX 40

#define CONFIG_CHANNEL 6

EventGroupHandle_t wifi_event_group;
const int CONNECTED_BIT = BIT0;
const int START_BIT = BIT1;


static bool initialized = false;
esp_netif_t *ap_netif;
esp_netif_t *sta_netif = NULL;



#ifdef NOTUSED
// Works only when scanning (for example when attempting to connect)
// No support for action frames.
static void
vendor_cb(void *ctx, wifi_vendor_ie_type_t type, const uint8_t sa[6], const vendor_ie_data_t *vnd_ie, int rssi) {

    ESP_LOGI(TAG, "Vendor %d %d %d %d", type, vnd_ie[0].vendor_oui[0], vnd_ie[0].vendor_oui[1],
             vnd_ie[0].vendor_oui[2]);
}
#endif

static void event_handler(void *arg, esp_event_base_t event_base,
                          int32_t event_id, void *event_data) {

    xEventGroupSetBits(wifi_event_group, START_BIT);

    ESP_LOGI(TAG, "Wifi event %d %s\n", event_id, event_base);

    if (event_base == WIFI_EVENT && event_id == WIFI_EVENT_STA_DISCONNECTED) {
        esp_wifi_connect();
        xEventGroupClearBits(wifi_event_group, CONNECTED_BIT);
    } else if (event_base == IP_EVENT && event_id == IP_EVENT_STA_GOT_IP) {
        xEventGroupSetBits(wifi_event_group, CONNECTED_BIT);
    }
}





static void syncTime(char *server) {
    sntp_setoperatingmode(SNTP_OPMODE_POLL);
    sntp_setservername(0, server);
    sntp_init();
}

static void stop() {
    ESP_ERROR_CHECK(esp_event_handler_unregister(WIFI_EVENT, WIFI_EVENT_STA_DISCONNECTED, &event_handler));
    ESP_ERROR_CHECK(esp_event_handler_unregister(WIFI_EVENT, WIFI_EVENT_STA_CONNECTED, &event_handler));
    ESP_ERROR_CHECK(esp_event_handler_unregister(WIFI_EVENT, WIFI_EVENT_STA_START, &event_handler));

    ESP_ERROR_CHECK(esp_event_handler_unregister(WIFI_EVENT, WIFI_EVENT_WIFI_READY, &event_handler));

    ESP_ERROR_CHECK(esp_event_handler_unregister(IP_EVENT, IP_EVENT_STA_GOT_IP, &event_handler));

    //ESP_ERROR_CHECK(esp_event_handler_unregister(IP_EVENT, IP_EVENT_GOT_IP6, &on_got_ipv6));

    esp_err_t err = esp_wifi_stop();

    if (err == ESP_ERR_WIFI_NOT_INIT) {
        return;
    }
    ESP_ERROR_CHECK(err);
    ESP_ERROR_CHECK(esp_wifi_deinit());

    ESP_ERROR_CHECK(esp_wifi_clear_default_wifi_driver_and_handlers(sta_netif));

    esp_netif_destroy(sta_netif);
    sta_netif = NULL;

    esp_netif_destroy(ap_netif);
    ap_netif = NULL;

    initialized = false;
}

// Mon mode is not compatible with STA ( needs to be on channel 6 )
void initialise_wifi(void) {
    esp_log_level_set("wifi", ESP_LOG_INFO);
    if (initialized) {
        return;
    }
    ESP_ERROR_CHECK(esp_netif_init());

    ap_netif = esp_netif_create_default_wifi_ap();
    assert(ap_netif);
    sta_netif = esp_netif_create_default_wifi_sta();
    assert(sta_netif);

    wifi_init_config_t cfg = WIFI_INIT_CONFIG_DEFAULT();

    ESP_ERROR_CHECK(esp_wifi_init(&cfg));

    ESP_ERROR_CHECK(esp_event_handler_register(WIFI_EVENT, WIFI_EVENT_WIFI_READY, &event_handler, NULL));
    ESP_ERROR_CHECK(esp_event_handler_register(WIFI_EVENT, WIFI_EVENT_STA_START, &event_handler, NULL));
    ESP_ERROR_CHECK(esp_event_handler_register(WIFI_EVENT, WIFI_EVENT_STA_CONNECTED, &event_handler, NULL));

    ESP_ERROR_CHECK(esp_event_handler_register(WIFI_EVENT, WIFI_EVENT_STA_DISCONNECTED, &event_handler, NULL));
    ESP_ERROR_CHECK(esp_event_handler_register(IP_EVENT, IP_EVENT_STA_GOT_IP, &event_handler, NULL));

    ESP_ERROR_CHECK(esp_wifi_set_storage(WIFI_STORAGE_RAM));

    //esp_wifi_set_auto_connect(false);
//        wifi_config_t wifi_config = { };
//    strncpy((char*) wifi_config.sta.ssid, "GoogleGuest", sizeof(wifi_config.sta.ssid));
//
//    ESP_ERROR_CHECK( esp_wifi_set_config(ESP_IF_WIFI_STA, &wifi_config) );


    //

    ESP_LOGI(TAG, "Wifi initialized");
    initialized = true;
}

static void wifi_sta(const char *ssid, const char *pass) {
    initialise_wifi();

    wifi_config_t wifi_config = {
            sta: {

            }};

    strlcpy((char *) wifi_config.sta.ssid, ssid, sizeof(wifi_config.sta.ssid));
    if (pass) {
        strlcpy((char *) wifi_config.sta.password, pass, sizeof(wifi_config.sta.password));
    }

    ESP_ERROR_CHECK(esp_wifi_set_mode(WIFI_MODE_STA));
    ESP_ERROR_CHECK(esp_wifi_set_config(ESP_IF_WIFI_STA, &wifi_config));

    ESP_ERROR_CHECK(esp_wifi_start());

    ESP_ERROR_CHECK(esp_wifi_connect());
}

static bool wifi_join(const char *ssid, const char *pass, int timeout_ms) {
    wifi_sta(ssid, pass);

    int bits = xEventGroupWaitBits(wifi_event_group, CONNECTED_BIT,
                                   pdFALSE, pdTRUE, timeout_ms / portTICK_PERIOD_MS);
    return (bits & CONNECTED_BIT) != 0;
}

int init_wifi(int mode, char *ssid, char *psk) {
    wifi_event_group = xEventGroupCreate();

    ESP_LOGI(TAG, "Startup: wifi=%d (1=STA, 2=AP, 3=APSTA) ssid=%s psk=%s", mode,
            ssid == NULL ? "":ssid, psk==NULL ? "": psk);
    if (mode == 1 && ssid != NULL) {
        ESP_LOGI(TAG, "Connecting to STA %s %s", ssid, psk);
        wifi_sta(ssid, psk);
    } else if (mode == 2) { // AP+mon, channel 6
        ESP_LOGI(TAG, "AP");

    } else if (mode == 3) { // AP+STA+mon
        ESP_LOGI(TAG, "AP_STA %s %s", ssid, psk);
    }
    return 0;
}

/** Arguments used by 'join' function */
static struct {
    struct arg_int *timeout;
    struct arg_str *ssid;
    struct arg_str *password;

    struct arg_int *ap;
    struct arg_lit *mon;

    struct arg_lit *stop;
    struct arg_str *time;

    struct arg_end *end;
} args;

static int cmd_wifi(int argc, char **argv) {
    int nerrors = arg_parse(argc, argv, (void **) &args);
    if (nerrors != 0) {
        arg_print_errors(stderr, args.end, argv[0]);
        return 1;
    }

    if (args.time->count) {
        syncTime("pool.ntp.org");
        return 0;
    }

    if (args.stop->count) {
        stop();
        return 0;
    }

    // AP mode, channel 6, DMESH, mon
    // Uses a lot of power !!
    if (args.ap->count) {
        return 0;
    }

    ESP_LOGI(__func__, "Connecting to '%s'", args.ssid->sval[0]);

    /* set default value*/
    if (args.timeout->count == 0) {
        args.timeout->ival[0] = JOIN_TIMEOUT_MS;
    }

    bool connected = wifi_join(args.ssid->sval[0],
                               args.password->sval[0],
                               args.timeout->ival[0]);
    if (!connected) {
        ESP_LOGW(__func__, "Connection timed out");
        return 1;
    }
    ESP_LOGI(__func__, "Connected");
    return 0;
}

void register_wifi(nvs_handle_t nvs_handle) {
    args.timeout = arg_int0(NULL, "timeout", "<t>", "Connection timeout, ms");
    args.ssid = arg_str0(NULL, "ssid", "<ssid>", "SSID of AP");
    args.password = arg_str0(NULL, "psk", "<pass>", "PSK of AP");

    args.time = arg_str0(NULL, "time", "<ssid>", "ntp server");
    args.ap = arg_int0(NULL, "ap", "<t>", "AP mode, s");

    args.stop = arg_lit0(NULL, "stop", "Stop wifi");

    args.end = arg_end(0);

    const esp_console_cmd_t join_cmd = {
            .command = "wifi",
            .help = "WiFi AP, station or monitor",
            .hint = NULL,
            .func = &cmd_wifi,
            .argtable = &args
    };

    ESP_ERROR_CHECK(esp_console_cmd_register(&join_cmd));

}
