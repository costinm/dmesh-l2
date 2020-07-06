/* Console example

   This example code is in the Public Domain (or CC0 licensed, at your option.)

   Unless required by applicable law or agreed to in writing, this
   software is distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR
   CONDITIONS OF ANY KIND, either express or implied.
*/

#include <stdio.h>
#include <string.h>
#include <esp_pm.h>
#include <esp_sleep.h>
#include <esp_bt.h>
#include <esp_wifi.h>
#include <esp_bt_main.h>
#include <lora.h>
#include <driver/rtc_io.h>
#include "esp_system.h"
#include "esp_log.h"
#include "esp_vfs_dev.h"
#include "driver/uart.h"
#include "esp_vfs_fat.h"
#include "nvs.h"
#include "nvs_flash.h"

#include "dmesh.h"
#include "nan.h"

static const char *TAG = "l3dmesh";

static void initialize_nvs(void) {
    esp_err_t err = nvs_flash_init();
    if (err == ESP_ERR_NVS_NO_FREE_PAGES || err == ESP_ERR_NVS_NEW_VERSION_FOUND) {
        ESP_ERROR_CHECK(nvs_flash_erase());
        err = nvs_flash_init();
    }
    ESP_ERROR_CHECK(err);
}



// Disable BT, Wifi.
// Keep lora interrupt
void dmesh_enter_deep_sleep(uint64_t timeout) {
    esp_bluedroid_disable();
    esp_bt_controller_disable();

    //esp_wifi_stop();

    // interupts work ?
    //lora_sleep();

    ESP_ERROR_CHECK(esp_sleep_enable_timer_wakeup(timeout));
    int io_num = lora.pinInterupt;
    if (!rtc_gpio_is_valid_gpio(io_num)) {
        ESP_LOGE(TAG, "GPIO %d is not an RTC IO", io_num);
        return;
    }
    ESP_ERROR_CHECK(esp_sleep_enable_ext1_wakeup(1ULL << io_num, 1));

    rtc_gpio_isolate(GPIO_NUM_12);
    // On resume, will initialize Lora again.

    esp_deep_sleep_start();
}

void register_console(nvs_handle_t nvs_handle);
int init_wifi(int mode, char *ssid, char *psk);

/*
 * - 4k or RTC RAM ( or 8 - but 4 reserved )
 * - only accessible from the PRO CPU, not APP
 * - app-main runs PRO CPU, other threads need to be pinned to core - or copy the data
 */
RTC_DATA_ATTR unsigned long firstBoot;
unsigned long resetTime;

void app_main(void) {
    resetTime = currentTimeMicro();
    if (firstBoot == 0) {
        firstBoot = currentTimeMicro();
    }
    esp_sleep_wakeup_cause_t wcause = esp_sleep_get_wakeup_cause();

    initialize_nvs();

#if !defined(CONFIG_PM_ENABLE) || !defined(CONFIG_FREERTOS_USE_TICKLESS_IDLE)
#error Required PM_ENABLE
#endif

    nvs_handle_t nvs_handle;
    ESP_ERROR_CHECK(nvs_open("storage", NVS_READWRITE, &nvs_handle));

    nvs_handle_t wifi_handle;
    ESP_ERROR_CHECK(nvs_open("nvs.net80211", NVS_READWRITE, &wifi_handle));

    ESP_ERROR_CHECK(esp_event_loop_create_default());

    // This seems to allow Lora to receive interrupts !
    // Not OLED yet.
    esp_sleep_pd_config(ESP_PD_DOMAIN_RTC_PERIPH, ESP_PD_OPTION_ON);

    esp_sleep_pd_config(ESP_PD_DOMAIN_RTC_SLOW_MEM, ESP_PD_OPTION_OFF);
    esp_sleep_pd_config(ESP_PD_DOMAIN_RTC_FAST_MEM, ESP_PD_OPTION_OFF);

    ESP_ERROR_CHECK(esp_pm_lock_create(ESP_PM_NO_LIGHT_SLEEP, 0, "dmsh", &status.light_sleep_pm_lock));

#ifdef USE_CONSOLE
    register_console(nvs_handle);
#endif

    ESP_ERROR_CHECK(esp_sleep_enable_gpio_wakeup());
    // char will not be in the buffer !
    // TODO: how to detect, so it doesn't go to sleep immediately ?
    ESP_ERROR_CHECK(uart_set_wakeup_threshold(CONFIG_ESP_CONSOLE_UART_NUM, 3));

    ESP_ERROR_CHECK(esp_sleep_enable_uart_wakeup(CONFIG_ESP_CONSOLE_UART_NUM));

    // Dynamic freq adj: 20 mA
    // light sleep: 10 mA

    int32_t light_sleep = 0;
    int err =nvs_get_i32(nvs_handle, "lsleep", &light_sleep);
    if (err == ESP_OK && light_sleep > 0 ) {
        ESP_LOGI(TAG, "STARTUP: lsleep=%d", light_sleep);
        // Configure dynamic frequency scaling:
        // maximum and minimum frequencies are set in sdkconfig,
        // automatic light sleep is enabled if tickless idle support is enabled.
        int xtal_freq = (int) rtc_clk_xtal_freq_get();
        esp_pm_config_esp32_t pm_config = {
                .max_freq_mhz = CONFIG_ESP32_DEFAULT_CPU_FREQ_MHZ,
                .min_freq_mhz = xtal_freq, // 26, // 40, // can be 26 for some devices
                .light_sleep_enable = true
        };
        ESP_ERROR_CHECK(esp_pm_configure(&pm_config));
    }

    int32_t mode = 0;
    err = nvs_get_i32(nvs_handle, "wifi", &mode);
    if (err != ESP_OK) {
        mode = 0;
    }
    char ssid[32];
    size_t ssid_len = 0;
    err = nvs_get_str(nvs_handle, "ssid", NULL, &ssid_len);
    err = nvs_get_str(nvs_handle, "ssid", ssid, &ssid_len);
    if (err != ESP_OK) {
        ssid_len = 0;
    }
    char psk[16];
    size_t psk_len = 0;

    nvs_get_str(nvs_handle, "psk", NULL, &psk_len);
    err = nvs_get_str(nvs_handle, "psk", psk, &psk_len);
    if (err != ESP_OK) {
        psk_len = 0;
    }

    if (ssid_len > 0) {
        ESP_LOGI(TAG, "SSID %s %s %d", ssid, psk, mode);
    }
    if (mode == 4) {
        nanStart();
    } else {
        init_wifi(mode, ssid_len == 0 ? NULL : ssid, psk_len == 0 ? NULL : psk);
    }

    char strftime_buf[64];
    struct tm timeinfo;

    setenv("TZ", "UTC-8", 1);
    tzset();

    time_t now;
    time(&now);
    localtime_r(&now, &timeinfo);
    strftime(strftime_buf, sizeof(strftime_buf), "%c", &timeinfo);

    register_lora(nvs_handle);

    ESP_LOGI("l3dmesh", "Current time: RTC:%s RST:%ld FB:%ld wc:%d", strftime_buf,
            currentTimeMicro() - resetTime, currentTimeMicro() - firstBoot, wcause);

}
