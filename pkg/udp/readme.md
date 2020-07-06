Experimental UDP NAT for ESP8266/ESP32 or similar devices, with low capabilities.

The idea is the device will use the 'raw' eth interface - 
there are limits on how many TCP connection they can handle.
UDP encap is easy to parse.

This code would run on FFD (android or pi devices), controlling 
the ESP routing. 

Each node will use multicast for local discovery, FFD will
control the low end devices directing the routing. Most
of the time it'll just be a tree.

TODO: minimal security - pairing, etc.
