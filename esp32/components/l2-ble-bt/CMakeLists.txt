idf_component_register(SRCS "ble_spp_server.c" "bt_spp_server.c"
                    INCLUDE_DIRS .
                    REQUIRES console nvs_flash bt l3dmesh)

set(COMPONENT_REQUIRES bt)