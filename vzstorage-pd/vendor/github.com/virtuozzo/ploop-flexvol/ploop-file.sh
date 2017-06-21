#!/bin/sh

exec 3>&1

`dirname $0`/ploop.bin wrapper -logtostderr -- ploop "$@" &>> /var/log/ploop-flexvol.log
