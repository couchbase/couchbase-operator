#!/bin/bash

######## usage
# 	start with ipv6 mode and assign cb1.ipv6.couchbase.com in DNS(172.17.0.2)
# 	./start.sh true cb1 172.17.0.2

# 	start with ipv4 mode
# 	./start.sh

IPV6=${1:-false}
HOSTNAME=${2}
DNS_IP=${3}

# to lower case
IPV6="$(echo ${IPV6} | tr '[A-Z]' '[a-z]')"

if [ ! -z ${IPV6} ] && [ "${IPV6}" == "true" ]; then
	# enalbe ipv6
	sed -i 's/ipv6, false/ipv6, true/' /opt/couchbase/etc/couchbase/static_config
fi

# register FQDN in local DNS server and update /etc/resolv.conf to lookup local DNS server first
if [ ! -z ${HOSTNAME} ] && [ ! -z ${DNS_IP} ]; then
        # is the DNS_IP reachable?
        IS_REACHABLE=$(nmap -Pn -p80 ${DNS_IP}| awk "\$1 ~ /^80\/tcp/ {print \$2}")
        # register domain name
        if [ "${IS_REACHABLE}" == "open" ]; then
                curl -X PUT -H 'Content-Type: application/json' -d "{\"domains\": [\"${HOSTNAME}.ipv6.couchbase.com\"]}" http://${DNS_IP}:80/container/name/${HOSTNAME}
                # add dns container in resolv.conf
                sed "s/^nameserver.*/nameserver ${DNS_IP}\n&/" /etc/resolv.conf > /tmp/resolv.conf
                cat /tmp/resolv.conf > /etc/resolv.conf
                rm /tmp/resolv.conf
        else
                echo "${DNS_IP}:80 is not reachable"
        fi
fi

nohup couchbase-start couchbase-server -- -noinput &
/usr/bin/ssh-keygen -A
/usr/sbin/sshd -D