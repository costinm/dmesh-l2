ifeq (${TARGET},esp8266)

# Used to build 8266 variant, using RTOS SDK.
# Not all features are enabled.

all:
	make build MAKE_RESTARTS=1

esp8266:
	cp sdkconfig.esp8266 sdkconfig


PROJECT_NAME := nan

# Skip regen, fast build
#MAKE_RESTARTS = 1

# flash monitor
include $(IDF_PATH)/make/project.mk

mon: monitor

else 
all:
	(mkdir -p build)
	(cd build;  ESPBAUD=921600 make app-flash V=1 MAKE_RESTARTS=1)
	idf.py monitor

slow:
	#(cd build;  ESPBAUD=256000 make app-flash V=1 MAKE_RESTARTS=1)
	(cd build;  ESPBAUD=115200 make app-flash V=1 MAKE_RESTARTS=1)
	idf.py monitor


mon:
	idf.py monitor
full:
	(cd build;  ESPBAUD=921600 make flash V=1 MAKE_RESTARTS=1)
	idf.py monitor

configure:
	idf.py reconfigure

endif
