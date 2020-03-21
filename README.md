Provide raw exchange of data at L2, using WifiAware, P2P/WifiDirect, BLE, Lora(using an adapter).
To communicate securely across 'mesh' devices - including low-end IoT, Android, servers - it is useful 
to support multiple protocols that don't require the device to be given full Internet access or
added to the Wifi access point.
 
This uses the same idea as Istio and other mesh protocols based on 'zero trust'. A device 
connected to the mesh is not trusted - will not be allowed direct access to the Wifi network nor
full internet access. The link security is also not trusted - Wifi and BLE provide some encryption, 
but it is local, not end-to-end. Security is implemented at L6, using end-to-end encryption - mTLS 
for streams and WebPush for single messages. Neither are part of this package. 

Wifi and BLE requires root or NET_ADMIN capabilities - only minimal code required to achieve 
low-level packet exchange included, to further minimize security risks and keep things simple. 

# Interface

The app will open a 'dmesh' UDS socket. A non-root app is expected to connect. UID of the peer 
will be checked and use to authorize the connection. 

This implements a message-based communication - commands and raw packets are exchange 
over the socket, similar with Netlink. 

# Protocols

## NAN - WifiAware - Neighbour Aware Network

This is the recommended and most interesting protocol, supported in Android Pixel2+. 
It operates on Channel 6, as a peer-to-peer protocol, with a rotating 'master' sending
beacons every 1/2 second. Devices advertise the time they are awake and receiving, and on
which frequencies. 

An Android device can be connected to an AP, but still communicate via NAN with other 
devices on a different frequncy.

It is better optimized for low-power and disconnected operation, allowing devices to 
exchange messages without having a connection, and to create direct connections while 
both devices can still sleep.  With P2P one of the device (the group owner) is typically active 
all the time, in particular if 'legacy' API is used to connect (the only way to 
create connections without user interaction on most android versions).

The package implements a minimal subset of the protocol, enough to communicate with 
Android and ESP32, by using NetLink SEND_FRAME interface and creating a monitor interface.

### Other NAN benefits

A future extension of this will be to allow each device to select a different receive channel, to
maximize the use of the spectrum. Channel 6 will be used according to the standard, to exchange 
information about the time schedule and channel of each device. A control plane will attempt to 
optimize the allocation (in an even more distant future). 

Operation will be similar with LoRA - a device will know the channel and time when each peer is 
available and use that to send frames. 

A device may have multiple drivers - it could also listen on a BLE or LoRA channel, the goal is to 
use the most battery efficient mechanism for transmission of low-speed control data, as well
as activate high-speed interface on the best channel when needed.

## WifiDirect/P2P and wpa_supplicant

A connection to wpa_supplicant is used to control P2P discovery, starting an AP and 
connecting to other P2P devices. 

The app will also start a dhcp server, using the defined port - non-root applications can't do this.
This is needed since most versions of Android expect a DHCP response.

Communication with non-rooted Android uses normal UDP, using IPv6 link-local address.

##  BLE

Uses an 'extended' version of Eddystone to advertise a UUID. 
The extension consists of using 'connectable', with a Proxy characteristic used to 
send and receive frames.  

Testing with Android and ESP32.

TODO: L2 communication is more efficient, supported in recent Android.


## Iptables/routing setup

WIP - similar with Istio, to allow capturing local traffic and redirecting to the high-level proxy.

