idf_component_register(SRCS "lora.c" "lora_drv.c"
                    INCLUDE_DIRS .
            REQUIRES console nvs_flash l3dmesh
                    )