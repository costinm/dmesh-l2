# Interface with low-level local mesh network

Communicates using messages with a trusted app handling the low-level scan/connect - android app or app running
as root.

The driver subscribes to the /wifi topic, and receives messages setting the desired state and commands.
Implementation uses Android Wifi, BLE, BT and NAN, exposing discovery, connection, messages.
On Linux the 'wpa' package is used for wpa_supplicant, using a separate root process. Other features may be 
added later, but Wifi P2P is the main mechanism.

The driver sends "/net" messages, with details about the network status.
