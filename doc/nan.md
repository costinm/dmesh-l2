Notes on nan findings from this project.

# NAN discovery

On a normal Linux machine, with typical Wifi driver we can use NL80211
interface to send a frame on NAN discovery channel and listen for a 
short time. 

If the machine has an active connection to an AP - most of the time will
be spent on the managed frequency, and it's very difficult to sync unless
the chipset allows operation on 2 channels. 


## Workaround for time sync 

Send NAN discovery frames with very high preference. Each Linux machine
with an internet connection (connected to an AP) will form its own cluster, 
and not attempt to merge the clusters. 

Merging clusters involve syncing on a different master - which is not possible
in this case. However, the intent is to reduce the battery use on each device,
by rotating the role of master - Linux machines are expected to have much 
larger batteries and will be in a higher power mode while connected to an AP.

One benefit is that Android devices will have lower battery use when a 
linux machine in NAN mode is around. 

This doesn't seem to violate the standard (too much).

## Linux - STA active

If STA interface is down we can send sync frames with relatively good accuracy.
Mon receives all beacons and frames from Android, can send fine.

It seems possible to implement a mostly compliant aware in user space - without 
concurrent operation.


If STA interface is up - we can send discovery, but hard to sync, the interval
is far from 512. Also hard to receive frames in discovery window.

It seems the only options in this mode are:

- use a cheap ESP8266/32 as a 'Aware modem', syncing with Android and 
other devices. Will send to Linux on the receive freq ( if in 2.4 GHz),
will receive frames sent using netlink. If Linux is on 5GHz - poll is possible,
using the non-sync sync frame. 

- use P2P discovery instead


## Linux + Android

## Linux + Linux - different channel

If 2 linux machines are around and connected to different APs, there is no
good way to sync using NAN.
Each machine will send NAN beacons on channel 6, but are unlikely to hear 
the other machine at the same time.

However it is expected the 2 devices will be connected to internet, so they
can sync using an Internet discovery server - and may find out each other's 
channel.

## Linux + Linux - same channel

As an extension to NAN, this implementation allows receiving discovery frames
on the current operating channel of the STA. 



## Linux + ESP8266/ESP32

If the mesh has an ESP or equivalent device in range of Linux or Android 
device, the ESP will act as a Gateway for discovery frames:
- will listen on channel 6 with high duty-cycle
- respond fast to Linux or Android discovery frames, and keep track of 
operating frequencies
- forward messages and discovery beacons on the STA operating channel for 
linux.

