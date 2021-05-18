#!/bin/bash

# script to write a yaml test configuration file corresponding to a physical zfs layout
# (to be run as sudo; requires zfs_to_yaml.lua to be in the same folder as this script)

script="$(dirname "$(readlink -f "$0")")/zfs_to_yaml.lua"

echo "pools:"
for pool in $(zpool list -Ho name); do
    echo -e "  - name: ${pool}\n    datasets:"
    ret=$(zfs program -n ${pool} ${script} -- ${pool}) || exit
    echo "${ret}" | sed -e "1d" -e "2s|^[^']*'||" -e '$d'
done
