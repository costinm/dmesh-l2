/*
   This example code is in the Public Domain (or CC0 licensed, at your option.)

   Unless required by applicable law or agreed to in writing, this
   software is distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR
   CONDITIONS OF ANY KIND, either express or implied.
*/


#include "freertos/FreeRTOS.h"
#include "freertos/task.h"
#include "freertos/event_groups.h"
#include <freertos/queue.h>
#include <argtable3/argtable3.h>
#include <esp_gattc_api.h>
#include <esp_gatt_common_api.h>
#include "esp_system.h"
#include "esp_log.h"
#include "nvs_flash.h"
#include "esp_bt.h"
#include "string.h"
#include "esp_console.h"

#include "esp_gap_ble_api.h"
#include "esp_gatts_api.h"
#include "esp_bt_defs.h"
#include "esp_bt_main.h"
#include "ble_spp_server.h"

#include "bt_ble.h"

#define GATTS_TABLE_TAG  "BLE"

#define ESP_SPP_APP_ID              0x56

#define SAMPLE_DEVICE_NAME          "DM-ESP"

#define SPP_SVC_INST_ID                0


#define UUID_LEN ESP_UUID_LEN_16

#define FROM_BLE 0x10

ble_driver ble;

// Use (extended) Eddystone
//
// Beacon includes info in Service data, not name
static const uint16_t spp_service_uuid = 0xFEAA;

// Alternative: 0x2AB9 (http body)
// Alternative: Mesh proxy service - 0x1828

static const uint16_t spp_data_receive_uuid = 0x2ADD;
static const uint16_t spp_data_notify_uuid = 0x2ADE;

// Hand crafted advertising data.
// 07 - list of 16-bit attributes
// 02 - incomplete list of 16-bit UIDs
// 03 - complete list of 16-bit UIDs
// 09 - complete local name
// 08 - shortened name
static const uint8_t spp_adv_data[] = {
        0x02, 0x01, 0x06,
        //0x03,0x03,0xF0,0xAB,
        0x03, 0x03, 0xAA, 0xFE, // Eddystone

        0x0F,
        0x16, 0x45, 0x53, 0x50, 0x5f, 0x53, 0x50, 0x50, 0x5f, 0x53,
        0x45, 0x52, 0x56, 0x45, 0x52
        //0x0F,0x09,0x45,0x53,0x50,0x5f,0x53,0x50,0x50,0x5f,0x53,0x45,0x52,0x56,0x45,0x52
};

static uint16_t spp_mtu_size = 23;
static uint16_t spp_conn_id = 0xffff;
static esp_gatt_if_t spp_gatts_if = 0xff;

static bool enable_data_ntf = false;
static bool is_connected = false;
static esp_bd_addr_t spp_remote_bda = {0x0,};

static uint16_t spp_handle_table[SPP_IDX_NB];

static char *TAG = "ble";

static esp_ble_adv_params_t spp_adv_params = {
        .adv_int_min        = 0x20,
        .adv_int_max        = 0x40,
        .adv_type           = ADV_TYPE_IND,
        .own_addr_type      = BLE_ADDR_TYPE_PUBLIC,
        .channel_map        = ADV_CHNL_ALL,
        .adv_filter_policy  = ADV_FILTER_ALLOW_SCAN_ANY_CON_ANY,
};

struct gatts_profile_inst {
    esp_gatts_cb_t gatts_cb;
    uint16_t gatts_if;
    uint16_t app_id;
    uint16_t conn_id;
    uint16_t service_handle;
    esp_gatt_srvc_id_t service_id;
    uint16_t char_handle;
    esp_bt_uuid_t char_uuid;
    esp_gatt_perm_t perm;
    esp_gatt_char_prop_t property;
    uint16_t descr_handle;
    esp_bt_uuid_t descr_uuid;
};

/*
 *  SPP PROFILE ATTRIBUTES
 ****************************************************************************************
 */
/// SPP Service
/// Characteristic UUID

#define CHAR_DECLARATION_SIZE   (sizeof(uint8_t))

static const uint16_t primary_service_uuid = ESP_GATT_UUID_PRI_SERVICE;
static const uint16_t character_declaration_uuid = ESP_GATT_UUID_CHAR_DECLARE;
static const uint16_t character_client_config_uuid = ESP_GATT_UUID_CHAR_CLIENT_CONFIG;

static const uint8_t char_prop_notify = ESP_GATT_CHAR_PROP_BIT_NOTIFY;
static const uint8_t char_prop_write = ESP_GATT_CHAR_PROP_BIT_WRITE_NR;
//static const uint8_t char_prop_notify_write = ESP_GATT_CHAR_PROP_BIT_WRITE_NR |ESP_GATT_CHAR_PROP_BIT_NOTIFY;

static const uint8_t spp_data_receive_val[20] = {0x00};


static const uint8_t spp_data_notify_val[20] = {0x00};
static const uint8_t spp_data_notify_ccc[2] = {0x00, 0x00};


///Full HRS Database Description - Used to add attributes into the database
static const esp_gatts_attr_db_t spp_gatt_db[] =
        {
                //SPP -  Service Declaration
                [SPP_IDX_SVC]                        =
                        {{ESP_GATT_AUTO_RSP},
                         {UUID_LEN, (uint8_t *) &primary_service_uuid,
                                 ESP_GATT_PERM_READ,
                                 sizeof(spp_service_uuid), sizeof(spp_service_uuid), (uint8_t *) &spp_service_uuid}},

                //SPP -  data receive WRITE characteristic Declaration
                [SPP_IDX_SPP_DATA_RECV_CHAR]            =
                        {{ESP_GATT_AUTO_RSP},
                         {ESP_UUID_LEN_16, (uint8_t *) &character_declaration_uuid,
                                 ESP_GATT_PERM_READ,
                                 CHAR_DECLARATION_SIZE, CHAR_DECLARATION_SIZE, (uint8_t *) &char_prop_write}},

                //SPP -  data receive WRITE characteristic Value
                [SPP_IDX_SPP_DATA_RECV_VAL]                =
                        {{ESP_GATT_AUTO_RSP},
                         {UUID_LEN, (uint8_t *) &spp_data_receive_uuid,
                                 ESP_GATT_PERM_WRITE,
                                 SPP_DATA_MAX_LEN, sizeof(spp_data_receive_val), (uint8_t *) spp_data_receive_val}},

                //SPP -  data notify characteristic Declaration
                [SPP_IDX_SPP_DATA_NOTIFY_CHAR]  =
                        {{ESP_GATT_AUTO_RSP},
                         {ESP_UUID_LEN_16, (uint8_t *) &character_declaration_uuid,
                                 ESP_GATT_PERM_READ,
                                 CHAR_DECLARATION_SIZE, CHAR_DECLARATION_SIZE, (uint8_t *) &char_prop_notify}},

                //SPP -  data notify characteristic Value
                [SPP_IDX_SPP_DATA_NTY_VAL]   =
                        {{ESP_GATT_AUTO_RSP},
                         {UUID_LEN, (uint8_t *) &spp_data_notify_uuid,
                                 ESP_GATT_PERM_WRITE | ESP_GATT_PERM_READ,
                                 SPP_DATA_MAX_LEN, sizeof(spp_data_notify_val), (uint8_t *) spp_data_notify_val}},

                //SPP -  data notify characteristic - Client Characteristic Configuration Descriptor
                [SPP_IDX_SPP_DATA_NTF_CFG]         =
                        {{ESP_GATT_AUTO_RSP},
                         {ESP_UUID_LEN_16, (uint8_t *) &character_client_config_uuid,
                                 ESP_GATT_PERM_READ | ESP_GATT_PERM_WRITE,
                                 sizeof(uint16_t), sizeof(spp_data_notify_ccc), (uint8_t *) spp_data_notify_ccc}},
        };


void bleSend(int conId, void *data, int size) {
    uint8_t total_num = 0;
    uint8_t current_num = 0;

    if (is_connected) {
        uint8_t *temp = NULL;
        uint8_t *ntf_value_p = NULL;
        if (size <= (spp_mtu_size - 3)) {
            esp_ble_gatts_send_indicate(
                    spp_gatts_if,
                    spp_conn_id,
                    spp_handle_table[SPP_IDX_SPP_DATA_NTY_VAL],
                    size,
                    data,
                    false);
        } else if (size > (spp_mtu_size - 3)) {
            if ((size % (spp_mtu_size - 7)) == 0) {
                total_num = size / (spp_mtu_size - 7);
            } else {
                total_num = size / (spp_mtu_size - 7) + 1;
            }
            current_num = 1;
            ntf_value_p = (uint8_t *) malloc((spp_mtu_size - 3) * sizeof(uint8_t));
            if (ntf_value_p == NULL) {
                ESP_LOGE(GATTS_TABLE_TAG, "%s malloc.2 failed\n", __func__);
                free(temp);
                return;
            }
            while (current_num <= total_num) {
                if (current_num < total_num) {
                    ntf_value_p[0] = '#';
                    ntf_value_p[1] = '#';
                    ntf_value_p[2] = total_num;
                    ntf_value_p[3] = current_num;
                    memcpy(ntf_value_p + 4, data + (current_num - 1) * (spp_mtu_size - 7), (spp_mtu_size - 7));
                    esp_ble_gatts_send_indicate(spp_gatts_if, spp_conn_id, spp_handle_table[SPP_IDX_SPP_DATA_NTY_VAL],
                                                (spp_mtu_size - 3), ntf_value_p, false);
                } else if (current_num == total_num) {
                    ntf_value_p[0] = '#';
                    ntf_value_p[1] = '#';
                    ntf_value_p[2] = total_num;
                    ntf_value_p[3] = current_num;
                    memcpy(ntf_value_p + 4, data + (current_num - 1) * (spp_mtu_size - 7),
                           (size - (current_num - 1) * (spp_mtu_size - 7)));
                    esp_ble_gatts_send_indicate(spp_gatts_if, spp_conn_id, spp_handle_table[SPP_IDX_SPP_DATA_NTY_VAL],
                                                (size - (current_num - 1) * (spp_mtu_size - 7) + 4), ntf_value_p,
                                                false);
                }
                vTaskDelay(20 / portTICK_PERIOD_MS);
                current_num++;
            }
            free(ntf_value_p);
        }
    }
}


static void gap_event_handler(esp_gap_ble_cb_event_t event, esp_ble_gap_cb_param_t *param) {
    esp_err_t err;

    switch (event) {
        case ESP_GAP_BLE_ADV_DATA_RAW_SET_COMPLETE_EVT:
            esp_ble_gap_start_advertising(&spp_adv_params);
            ESP_LOGI(GATTS_TABLE_TAG, "GAP_EVT, ADV_DATA_RAW_SET_COMPLETE\n");
            break;
        case ESP_GAP_BLE_ADV_START_COMPLETE_EVT:
            //advertising start complete event to indicate advertising start successfully or failed
            if ((err = param->adv_start_cmpl.status) != ESP_BT_STATUS_SUCCESS) {
                ESP_LOGE(GATTS_TABLE_TAG, "Advertising start failed: %s\n", esp_err_to_name(err));
            } else {
                ESP_LOGI(GATTS_TABLE_TAG, "GAP_EVT, ADV_START_COMPLETE\n");
            }
            break;
        default:
            ESP_LOGI(GATTS_TABLE_TAG, "GAP_EVT, event %d\n", event);
            break;
    }
}


static void gatts_event_handler(esp_gatts_cb_event_t event, esp_gatt_if_t gatts_if, esp_ble_gatts_cb_param_t *param) {
    ESP_LOGI(GATTS_TABLE_TAG, "GATTS event: %d, if=%d\n", event, gatts_if);

    esp_ble_gatts_cb_param_t *p_data = (esp_ble_gatts_cb_param_t *) param;
//    uint8_t res = 0xff;

    switch (event) {
        case ESP_GATTS_REG_EVT: // 0
            if (param->reg.status != ESP_GATT_OK) {
                ESP_LOGI(GATTS_TABLE_TAG, "Reg app failed, app_id %04x, status %d\n", param->reg.app_id,
                         param->reg.status);
                return;
            }

            esp_ble_gap_set_device_name(SAMPLE_DEVICE_NAME);

            // Raw adv data
            esp_ble_gap_config_adv_data_raw((uint8_t *) spp_adv_data, sizeof(spp_adv_data));

            esp_ble_gatts_create_attr_tab(spp_gatt_db, gatts_if, SPP_IDX_NB, SPP_SVC_INST_ID);
            break;
        case ESP_GATTS_READ_EVT: // 1
            break;
        case ESP_GATTS_WRITE_EVT: { // 2
            if (ble.driver.onMessage != NULL) {
                ble.driver.onMessage((char *) p_data->write.value, p_data->write.len, ble.driver.interfaceId);
            }
            //bleSend(0, (char *) (p_data->write.value), p_data->write.len);
            break;
        }
        case ESP_GATTS_EXEC_WRITE_EVT: { // 3
            break;
        }
        case ESP_GATTS_MTU_EVT: // 4
            ESP_LOGI(GATTS_TABLE_TAG, "GATTS MTU: %d\n", p_data->mtu.mtu);
            spp_mtu_size = p_data->mtu.mtu;
            break;
        case ESP_GATTS_CONF_EVT: // 5
            break;
        case ESP_GATTS_UNREG_EVT: // 6
            break;
        case ESP_GATTS_DELETE_EVT: //11
            break;
        case ESP_GATTS_START_EVT: //12
            break;
        case ESP_GATTS_STOP_EVT:
            break;
        case ESP_GATTS_CONNECT_EVT: // 14
            spp_conn_id = p_data->connect.conn_id;
            spp_gatts_if = gatts_if;
            is_connected = true;
            memcpy(&spp_remote_bda, &p_data->connect.remote_bda, sizeof(esp_bd_addr_t));
            break;
        case ESP_GATTS_DISCONNECT_EVT: // 15
            is_connected = false;
            enable_data_ntf = false;
            esp_ble_gap_start_advertising(&spp_adv_params);
            break;
        case ESP_GATTS_OPEN_EVT: // 16
            break;
        case ESP_GATTS_CANCEL_OPEN_EVT:
            break;
        case ESP_GATTS_CLOSE_EVT:
            break;
        case ESP_GATTS_LISTEN_EVT:
            break;
        case ESP_GATTS_CONGEST_EVT:
            break;
        case ESP_GATTS_CREAT_ATTR_TAB_EVT: {
            ESP_LOGI(GATTS_TABLE_TAG, "The number handle =%x\n", param->add_attr_tab.num_handle);
            if (param->add_attr_tab.status != ESP_GATT_OK) {
                ESP_LOGE(GATTS_TABLE_TAG, "Create attribute table failed, error code=0x%x", param->add_attr_tab.status);
            } else if (param->add_attr_tab.num_handle != SPP_IDX_NB) {
                ESP_LOGE(GATTS_TABLE_TAG,
                         "Create attribute table abnormally, num_handle (%d) doesn't equal to HRS_IDX_NB(%d)",
                         param->add_attr_tab.num_handle, SPP_IDX_NB);
            } else {
                memcpy(spp_handle_table, param->add_attr_tab.handles, sizeof(spp_handle_table));
                esp_ble_gatts_start_service(spp_handle_table[SPP_IDX_SVC]);
            }
            break;
        }
        default:
            break;
    }
}

static void esp_gattc_cb(esp_gattc_cb_event_t event, esp_gatt_if_t gattc_if, esp_ble_gattc_cb_param_t *param) {
    ESP_LOGI(TAG, "EVT %d, gattc if %d", event, gattc_if);
}

void init_ble_svc() {
    esp_err_t status;
    char err_msg[20];


    // Both client and server
    esp_ble_gap_register_callback(gap_event_handler);

    // Server side registration
    esp_ble_gatts_register_callback(gatts_event_handler);

    esp_ble_gatts_app_register(ESP_SPP_APP_ID);
    esp_ble_gattc_app_register(ESP_SPP_APP_ID);

    if ((status = esp_ble_gattc_register_callback(esp_gattc_cb)) != ESP_OK) {
        ESP_LOGE(TAG, "gattc register error: %s", esp_err_to_name_r(status, err_msg, sizeof(err_msg)));
        return;
    }

    esp_err_t local_mtu_ret = esp_ble_gatt_set_local_mtu(200);
    if (local_mtu_ret){
        ESP_LOGE(TAG, "set local  MTU failed: %s", esp_err_to_name_r(local_mtu_ret, err_msg, sizeof(err_msg)));
    }
}

void start_ble_server(void) {
    esp_err_t ret;
    esp_bt_controller_config_t bt_cfg = BT_CONTROLLER_INIT_CONFIG_DEFAULT();

    //ESP_ERROR_CHECK(esp_bt_controller_mem_release(ESP_BT_MODE_CLASSIC_BT));

    ret = esp_bt_controller_init(&bt_cfg);
    if (ret) {
        ESP_LOGE(GATTS_TABLE_TAG, "%s enable controller failed: %s\n", __func__, esp_err_to_name(ret));
        return;
    }

    ret = esp_bt_controller_enable(ESP_BT_MODE_BTDM );
    //ret = esp_bt_controller_enable(ESP_BT_MODE_BLE);
    if (ret) {
        ESP_LOGE(GATTS_TABLE_TAG, "%s enable controller failed: %s\n", __func__, esp_err_to_name(ret));
        return;
    }

    ret = esp_bluedroid_init();
    if (ret) {
        ESP_LOGE(GATTS_TABLE_TAG, "%s init bluetooth failed: %s\n", __func__, esp_err_to_name(ret));
        return;
    }
    ret = esp_bluedroid_enable();
    if (ret) {
        ESP_LOGE(GATTS_TABLE_TAG, "%s enable bluetooth failed: %s\n", __func__, esp_err_to_name(ret));
        return;
    }

    init_ble_svc();
    ESP_LOGI(TAG, "BLE started");
    return;
}

static struct {
    struct arg_lit *start;
    struct arg_lit *stop;
    struct arg_end *end;
} ble_args;

static int ble_cmd(int argc, char **argv) {
    int nerrors = arg_parse(argc, argv, (void **) &ble_args);
    if (nerrors != 0) {
        arg_print_errors(stderr, ble_args.end, argv[0]);
        return 1;
    }

    if (ble_args.start->count) {
        start_ble_server();
        return 0;
    }
    if (ble_args.stop->count) {
        esp_bluedroid_disable();
        esp_bt_controller_disable();

        return 0;
    }
    return 0;
}

void register_spp_ble(nvs_handle_t nvs_handle) {
    ble_args.start = arg_lit0(NULL, "start", "");
    ble_args.stop = arg_lit0(NULL, "stop", "");
    ble_args.end = arg_end(0);

    const esp_console_cmd_t join_cmd = {
            .command = "ble",
            .help = "BLE",
            .hint = NULL,
            .func = &ble_cmd,
            .argtable = &ble_args
    };

    ESP_ERROR_CHECK(esp_console_cmd_register(&join_cmd));

    int32_t wmode = 0;
    int err = nvs_get_i32(nvs_handle, "wifi", &wmode);
    int32_t mode = 0;
    err = nvs_get_i32(nvs_handle, "sble", &mode);

    if (err == ESP_OK) {
        if (mode == 1 && wmode == 0) {
            start_ble_server();
        }
    }
}

