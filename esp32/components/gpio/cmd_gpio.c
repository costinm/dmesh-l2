/* cmd_i2ctools.c

   This example code is in the Public Domain (or CC0 licensed, at your option.)

   Unless required by applicable law or agreed to in writing, this
   software is distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR
   CONDITIONS OF ANY KIND, either express or implied.
*/
#include <stdio.h>
#include "argtable3/argtable3.h"
#include "driver/gpio.h"
#include "esp_console.h"
#include "esp_log.h"

static const char *TAG = "gpio";

static gpio_num_t i2c_gpio_sda = 18;

// 6-11 used for flash
// 34-39 - only for input
//

static struct {
    struct arg_int *pin;
    struct arg_int *level;
    struct arg_int *mode;

    struct arg_end *end;
} gpio_args;

// TODO: interrupt enable/disable

static int gpio_cmd(int argc, char **argv)
{
    int nerrors = arg_parse(argc, argv, (void **)&gpio_args);
    if (nerrors != 0) {
        arg_print_errors(stderr, gpio_args.end, argv[0]);
        return 0;
    }

    int pin = 22;

    /* Check "--port" option */
    if (gpio_args.pin->count) {
        pin = gpio_args.pin->ival[0];
    }

    if (gpio_args.mode->count) {
        gpio_set_direction(pin, gpio_args.mode->ival[0]);
    }

    if (gpio_args.level->count) {
        gpio_set_level(pin, gpio_args.level->ival[0]);
    }

    return 0;
}

void register_gpio(void)
{
    gpio_args.pin = arg_int0(NULL, "pin", "<gpio>", "Pin");
    gpio_args.level = arg_int0(NULL, "level", "<0|1>", "Level");
    gpio_args.mode = arg_int1(NULL, "mode", "<0|1>", "Set the mode, default output");
    gpio_args.end = arg_end(0);
    const esp_console_cmd_t i2cconfig_cmd = {
        .command = "gpio",
        .help = "Config GPIO bus",
        .hint = NULL,
        .func = &gpio_cmd,
        .argtable = &gpio_args
    };
    ESP_ERROR_CHECK(esp_console_cmd_register(&i2cconfig_cmd));
}
