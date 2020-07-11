idf_component_register(SRCS
        "dmesh.c"
        "cmd_system.c"
        "dmpeers.cpp"
                    INCLUDE_DIRS .
                    REQUIRES console nvs_flash esp-nan-aware)
