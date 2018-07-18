FROM centos/systemd

MAINTAINER Couchbase Docker Team <docker@couchbase.com>

# Update server
RUN yum -y update; yum clean all

# Install utils and dependencies
RUN yum install -y tar \
    && yum clean all && \
      yum -y install openssl openssh-clients \
      lsof lshw net-tools numactl \
      sysstat wget screen psmisc \
      zip unzip glibc glibc-devel \
      openssh-server libssl0.9.8 \
      tcpdump iptables psmisc nmap initscripts

# Install python-httplib2
RUN curl https://bootstrap.pypa.io/get-pip.py | python - ; pip install httplib2

COPY functions /etc/init.d/

# Install gosu for startup script
RUN gpg --keyserver hkp://p80.pool.sks-keyservers.net:80 --recv-keys B42F6819007F00F88E364FD4036A9C25BF357DD4 \
    && curl -o /usr/local/bin/gosu -sSL "https://github.com/tianon/gosu/releases/download/1.4/gosu-amd64" \
    && curl -o /usr/local/bin/gosu.asc -sSL "https://github.com/tianon/gosu/releases/download/1.4/gosu-amd64.asc" \
    && gpg --verify /usr/local/bin/gosu.asc \
    && rm /usr/local/bin/gosu.asc \
    && chmod +x /usr/local/bin/gosu