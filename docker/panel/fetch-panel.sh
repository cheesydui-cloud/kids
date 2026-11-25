#!/bin/sh
# Retired. This script used to copy nft-server onto /usr/local/bin/nft-server
# and make that file the process Docker executes. overlayfs then refuses later
# upgrades with "device or resource busy".
echo "fetch-panel.sh 已停用。新机器请执行 install-docker-panel.sh。已有容器请在宿主机执行 install-container-panel.sh。" >&2
exit 1
