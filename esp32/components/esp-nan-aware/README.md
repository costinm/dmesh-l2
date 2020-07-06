# ESP8266 and ESP32 WifiAware / NAN implementation

This is a minimal, experimental implementation of a subset of the WifiAware specification.

The main goal is to allow an ESP device to communicate with an Android or Linux device supporting NAN, and
with other ESP or NAN-supporting devices.

A parallel project attempts to implement a similar subset on Linux devices without native chipset support, using netlink 
'send frame' and 'remain on channel'.

## WifiAware brief

- Devices sync on channel 6, with a rotating master sending beacons, with 500ms interval, and message exchange happens in ~16ms. 
- Optimized for power - devices can sleep most of the time (CPU needs to be ~4% active, rest in light or deep sleep)
- Basic communication very simple - encryption, sessions, etc can be handled at higher layers ( L6, L5 )
- Additional communication windows and creation of higher speed connections possible, can be done on-demand.
- devices can also operate on a different frequency - i.e. be connected to an AP on a different channel.

Android supports this - but only few devices seem to have this enabled (tested on Pixel 2, 3)

Linux also seems to have support - but it is also possible to use netlink to send and receive NAN
frames without any special driver support. I expect rooted Android to have a similar capability using netlink.

Security should be implemented at L6/L7 - NAN spec has some encryption, but it should not be trusted
or used, it is based on shared keys and at the wrong layer. Instead mTLS and Webpush should be used, with E2E encryption
insted of L2/local encryption.

## Why WifiAware

Current practice is to have the IoT device connect to the home access point. That is too risky with many IoT devices.

Even if a separate 'guest' network is used, once the device is connected it may use power management features to
sleep, but must be in range of the access point. There are also limits on how many devices can be connected to 
an access point, in particular if it's an 'ad-hoc' network using Android devices as AP.

With WifiAware/NAN the device can discover and communicate dirrectly with other Iot devices - as well as phones,
without requiring any user setup for networking. Devices discover each other and form clusters, allowing message 
exchange. 

On top of WifiAware it is possible to implement proper L6 security, as well as user-space mesh routing - without
depending on fragmented and poor chipset support.

### Security

Many IoT devices have dubious security - ESP has a number of binary blobs, and the applications itself is
not always trustworthy. Wifi and BLE/Bluetooth implement encryption at L2, only between neighbor devices - 
and usually require all devices to use the same shared key. 

Users should not trust the L2 network security (see "zero trust", Istio). Many devices at home lack proper security
and are vulnerable to attacks from the local network - made worse by adding IoT devices to the same network.
Avoiding the need to connect IoT devices to the Wifi AP also limits the attacks on the device itself from the
internet or other devices.

NAN provides messages and local connections - the app is still required to do its own L6/L7 encryption, ideally
using standard based solutions. I recommend Webpush for messages and mTLS for connections - but any other L6 security
mechanism can be supported without changes or complexity in the lower levels.

### Battery use

An Android device may avoid the requirement to be connected to the AP, and still communicate with
IoT and other Android devices and laptops, with low power use. NAN allows longer sleep intervals - beacon
interval is 500ms, and the master is rotating periodically.

In addition, devices can use lower TX power and higher speed, since they don't need to reach the AP.
This improves both the battery and reduces interferences.

## Other options

### ESPNOW

ESP SDK includes a similar protocol based on action frames, 'espnow'

I believe a subset of NAN is a better solution:

- NAN is standard based, espnow is Esspressif only and closed source. 
- NAN can exchange messages with (some) Android and Linux devices
- NAN is optimized for battery use
- NAN allows larger payloads - unfortunately not with Android, where 255 is still the limit.

### BLE

A separate project attempts to use BLE and BT for similar L2 message exchange with Android and other devices, using 
the same interface as WifiAware. Ideally a device would exchange latency tolerant messages using the lowest-power
available mechanism.

Lower priority since:
- Not available on ESP8266.
- shorter range


## Issues

### Master and Anchor Master support - broken timestap

ESP32 can't act in master or anchor master ! This is due to a bug ( or intentional cripling) of
the send function - the timestamp is set to zero by the SDK or ROM.

This doesn't prevent ESP32 from communicating with other devices like Android, that act as master

Proposed solution:

In order to support ESP32-to-ESP32 communication, a modified alghoritm is used: the SDF Publish frame
is treated in the same way as the beacon. ESP32 will always send a Publish instead of a beacon, if
it doesn't detect a real cluster and beacons.

We could also use a special service ID, and associated service data to include similar timestamp and cluster info.

In practice the timestamp is not critical in a cell - sync can happen by watching for any frame, and publish
is generally sent at the same time. In a mesh case, where messages need multiple hops, it is also ok
to sync on the Publish frame sent by nearby master.


### Data rate

ESP can only send at 1Mbps using the tx function ( 802.11b HR/DSSS)
esp_wifi_internal_set_rate may help.

### Active or passive subscribe

Still not clear what mode should be used.

For Android:
- when attach is called, device will start sending Beacons - including the service ID of any passive pub or sub. It doesn't 
seem useful for android to waste additional battery and bandwidth with the active publish or subscribe. 
- Non-android can detect the beacon and service, and send an active Publish to Android. Android will only detect the
publish from device, as callback in the SubscribeListener - it doesn't seem to receive callbacks when active subscribe
is sent, PublishListener only seems to receive follow up.
- The non-android device can stop sending active publish after it confirms Android saw the device - further reducing
battery/CPU

Based on this, it seems the ideal scenario is:
- Android uses passive subscribe, and stays attached to Nan most of the time. 
- If multiple androids are around, they will share the beacon burden. 
- 

TODO: 
- file bug and convince Android to provide additional callbacks in the NAN API, letting app know that beacons
are detected and what services are advertised.

## Compatibility and status

Working:
- Receive and handle Sync Beacons sent by Android
- Receive and respond to active Subscribe and Publish
- Send and receive FollowUp Messages
- Android can discover the device, get service info, including when update indicator is changed.
- Android can send and receive the messages (ESP to Android is not very reliable yet,  Android to ESP is
more stable because android sends the frame few times)
- A laptop without NAN support can also exchange messages, using a user-space implementation based on netlink
- communication happens only on the 16 ms DW

In progress/possible:
- Send active Subscribe and Publish
- sleep - tested that light sleep will not cause problems, but need to handle the lock.
- mesh features, forwarding - should be in a separate project, with consistent use of NAN, BLE, BT, Lora L2 drivers. 
- higher level messages (common with BT/BLE/Lora L2):
    - get real time ( to use with Lora, etc)
    - ping, flood, test modes
- use 'further availability' maps for more data.

Not planned:
- connections - too much effort, better alternative is to use the discovery to trigger a P2P connection
for the session - and close it as soon as done. The connection should NOT use channel 6.
- ranging
- security - should be only at L6, using mTLS or webpush messaging (or equivalents). Should not be specific to 
the L2, with BT/BLE/Wifi/Lora each using different crypto and only encrypting one hop.


## Dependencies

Current implementation uses RTOS, primary target is ESP32 - but will work with ESP8266 RTOS framework
as well.

Since one of the goals is to optimize battery use - we need to sleep a lot, and ESP32 tickess idle
and advanced features are critical. For ESP8266 no attempt to optimize sleep is made.

If time permits, Arduino ESP8266 and PlatformIO will also be supported - but very low priority.
Arduino/PlatformIO with ESP32 are more realistic since they're based on RTOS.

