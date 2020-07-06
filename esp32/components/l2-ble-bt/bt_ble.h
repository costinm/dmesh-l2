#pragma once

// L2 transport for BLE and BT
// Will send 'packets' over either BT SPP or BLE

// In both cases, the plain text mode is used - encryption should be handled at L6
// Routing, mesh, etc are handled at L3, and across multiple types of L2.
#include "dmesh.h"

typedef struct {
    l2_driver driver;

} ble_driver;

typedef struct {
    l2_driver driver;



} bt_driver;

// Singletons - there is only one radio
extern bt_driver bt;

extern ble_driver ble;