/* cmd_i2ctools.h

   This example code is in the Public Domain (or CC0 licensed, at your option.)

   Unless required by applicable law or agreed to in writing, this
   software is distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR
   CONDITIONS OF ANY KIND, either express or implied.
*/

#pragma once

#include <nvs.h>

#ifdef __cplusplus
extern "C" {
#endif

int i2c_master_init(nvs_handle_t nvs_handle);

void register_i2ctools(void);

#ifdef __cplusplus
}
#endif
