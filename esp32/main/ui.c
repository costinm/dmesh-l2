
#include <stdio.h>
#include <string.h>
#include "esp_log.h"
#include "esp_console.h"
#include "argtable3/argtable3.h"
#include "freertos/FreeRTOS.h"
#include "freertos/event_groups.h"

#include "driver/gpio.h"
#include "driver/i2c.h"

#include "ssd1366.h"
#include "nvs.h"

#include <string.h>
#include <esp_pm.h>
#include <esp_sleep.h>
#include "dmesh.h"
#include "nan.h"
#include "lora.h"

int i2c_master_init();


// 128x64 matrix, 8 rows (pages)
// For each row, data is writted one byte at a type representing a column.
// Font is 8 cols per char, so 16x8 char display

static EventGroupHandle_t ui_eg;
static ui u;
static char printbuf[100];

static int32_t rst = -1;

static DRAM_ATTR esp_pm_lock_handle_t ui_lock;

void ui_update(int line) {
    xEventGroupSetBits(ui_eg, 0);
}

void task_ui(void *arg_text) {
    ESP_ERROR_CHECK(esp_pm_lock_create(ESP_PM_NO_LIGHT_SLEEP, 0, "ui", &ui_lock));

	while (1) {
        xEventGroupWaitBits(ui_eg, 1,
                            false, true, 5000 / portTICK_PERIOD_MS);
        if (rst > 0) {
            gpio_set_direction(rst, GPIO_MODE_OUTPUT);

            gpio_set_level(rst, 1);
        }

        ui_setline(&u, 0);

        time_t now;
        time(&now);
        struct tm timeinfo;

        localtime_r(&now, &timeinfo);
        strftime(printbuf, sizeof(printbuf), "%T", &timeinfo);

        ui_println(&u, printbuf);

        snprintf(printbuf, 20, "L:%d/%d %d/%d", lora.loraBeacons, lora.loraReceived, lora.last.rssi,
                lora.last.snr);
        ui_println(&u, printbuf);

        snprintf(printbuf, 20, "N: %d %d %d %d", aware.nanMessagesIn, aware.nanSBeacon, aware.nanDBeacon,
                aware.nanPackets);
        ui_println(&u, printbuf);

        snprintf(printbuf, 20, "%s", "                ");
        ui_println(&u, printbuf);
        ui_println(&u, printbuf);
        ui_println(&u, printbuf);
        ui_println(&u, printbuf);
        ui_println(&u, printbuf);

        esp_pm_lock_acquire(ui_lock);
        vTaskDelay(3000 / portTICK_PERIOD_MS);
        esp_pm_lock_release(ui_lock);
    }
}

void oled_sleep() {


    //display.sendCommand(0x8D); //into charger pump set mode
    //display.sendCommand(0x10); //turn off charger pump
    //display.sendCommand(0xAE); //set OLED sleep
}


void oled_wakeup() {
    if (rst > 0) {
        gpio_set_direction(rst, GPIO_MODE_OUTPUT);
        gpio_set_level(rst, 1);
        //gpio_hold_en(rst);
    }
}


/**
 * Storage:
 * - oledrst - optional, will be set to 1
 * - sda/scl - pins to use for display
 *
 * @param my_handle
 */
void register_ui(nvs_handle_t my_handle)
{
    ui_eg = xEventGroupCreate();

    int err = nvs_get_i32(my_handle, "oledrst", &rst);
    if (err == ESP_OK) {
        gpio_set_direction(rst, GPIO_MODE_OUTPUT);
        gpio_set_level(rst, 1);
        //gpio_hold_en(rst);

    }

	err = i2c_master_init(my_handle);
	if (err != ESP_OK) {
        ESP_LOGE("UI", "I2C: err %d", err);
	    return;
	}

	err = ssd1306_init();
    if (err != ESP_OK) {
        ESP_LOGE("UI", "SSD: err1 %d", err);
	    return;
	}

    ESP_LOGI("UI", "UI: %d", rst);
	xTaskCreate(&task_ui, "display",  2048,NULL, 6, NULL);
}

