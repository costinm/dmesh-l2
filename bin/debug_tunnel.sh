#!/bin/bash

# IPSec tunnel using fixed keys, for debugging.
if [ "$4" == "" ]; then
    echo "usage: $0 <local_ip> <remote_ip> <new_local_ip> <new_remote_ip>"
    echo "creates an ipsec tunnel between two machines"
    exit 1
fi

SRC="$1"; shift
DST="$1"; shift
LOCAL="$1"; shift
REMOTE="$1"; shift

#KEY1=0x`dd if=/dev/urandom count=32 bs=1 2> /dev/null| xxd -p -c 64`
KEY1=0x0123456789abcdef0123456789abcdef01234567
KEY2=0x0123456789abcdef0123456789abcdef01234567

ID=0x0a111111

sudo ip xfrm state add src $SRC dst $DST proto esp spi $ID reqid $ID mode tunnel auth sha256 $KEY1 enc aes $KEY2
sudo ip xfrm state add src $DST dst $SRC proto esp spi $ID reqid $ID mode tunnel auth sha256 $KEY1 enc aes $KEY2
sudo ip xfrm policy add src $LOCAL dst $REMOTE dir out tmpl src $SRC dst $DST proto esp reqid $ID mode tunnel
sudo ip xfrm policy add src $REMOTE dst $LOCAL dir in tmpl src $DST dst $SRC proto esp reqid $ID mode tunnel
sudo ip addr add $LOCAL dev lo
sudo ip route add $REMOTE dev eth1 src $LOCAL


ssh $DST /bin/bash << EOF
    echo "spdflush; flush;" | sudo setkey -c
    sudo ip xfrm state add src $SRC dst $DST proto esp spi $ID reqid $ID mode tunnel auth sha256 $KEY1 enc aes $KEY2
    sudo ip xfrm state add src $DST dst $SRC proto esp spi $ID reqid $ID mode tunnel auth sha256 $KEY1 enc aes $KEY2
    sudo ip xfrm policy add src $REMOTE dst $LOCAL dir out tmpl src $DST dst $SRC proto esp reqid $ID mode tunnel
    sudo ip xfrm policy add src $LOCAL dst $REMOTE dir in tmpl src $SRC dst $DST proto esp reqid $ID mode tunnel
    sudo ip addr add $REMOTE dev lo
    sudo ip route add $LOCAL dev eth1 src $REMOTE
EOF