#!/bin/sh

# run this on each k8 node that has portworx installed

sudo systemctl stop portworx
sudo systemctl disable portworx
sudo rm -f /etc/systemd/system/portworx*.service
grep -q '/opt/pwx/oci /opt/pwx/oci' /proc/self/mountinfo && sudo umount /opt/pwx/oci
sudo chattr -i  /etc/pwx/.private.json
sudo rm -fr /opt/pwx
sudo rm -fr /etc/pwx