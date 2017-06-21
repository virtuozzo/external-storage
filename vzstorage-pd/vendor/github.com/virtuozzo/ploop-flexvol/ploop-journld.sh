#!/bin/bash

exec 3>&1
exec 1> >(systemd-cat --identifier ploop-flexvol)
exec 2>&1

`dirname $0`/ploop.bin wrapper -logtostderr -- ploop "$@"
