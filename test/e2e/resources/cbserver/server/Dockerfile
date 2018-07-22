FROM base.centos7

RUN mkdir /var/run/sshd
RUN echo 'root:couchbase' | chpasswd
RUN sed -i 's/PermitRootLogin without-password/PermitRootLogin yes/' /etc/ssh/sshd_config

# SSH login fix. Otherwise user is kicked off after login
RUN sed 's@session\s*required\s*pam_loginuid.so@session optional pam_loginuid.so@g' -i /etc/pam.d/sshd

RUN echo "export VISIBLE=now" >> /etc/profile

RUN sed -ri 's/UsePAM yes/#UsePAM yes/g' /etc/ssh/sshd_config && \
    sed -ri 's/#UsePAM no/UsePAM no/g' /etc/ssh/sshd_config && \
    ssh-keygen -f /etc/ssh/ssh_host_rsa_key -N '' -t rsa && \
    ssh-keygen -f /etc/ssh/ssh_host_dsa_key -N '' -t dsa

# Create Couchbase user with UID 1000 (necessary to match default
# boot2docker UID)
RUN groupadd -g1000 couchbase && \
    useradd couchbase -g couchbase -u1000 -m -s /bin/bash && \
    echo 'couchbase:couchbase' | chpasswd


ARG VERSION=5.5.0
ARG BUILD_NO=2958
ARG FLAVOR=vulcan
ARG BUILD_PKG=couchbase-server-enterprise-$VERSION-$BUILD_NO-centos7.x86_64.rpm
ARG BASE_URL=http://172.23.120.24/builds/latestbuilds/couchbase-server/$FLAVOR/$BUILD_NO

ARG BUILD_URL=$BASE_URL/$BUILD_PKG

RUN echo ${BUILD_URL}
RUN wget -N $BUILD_URL &>/dev/null

# Install couchbase
RUN rpm --install $BUILD_PKG

#clean the cache
RUN yum clean all


# custom startup scripts
COPY scripts/couchbase-start /usr/local/bin/
RUN mv /bin/systemctl /bin/systemctl.bin
COPY scripts/systemctl /bin/systemctl


LABEL Name=rhel7/couchbase-server
LABEL Release=Latest
LABEL Vendor=Couchbase
LABEL Version=${VERSION}
LABEL Architecture="x86_64"
LABEL RUN="docker run -d --rm --privileged -p 8091:8091 --restart always --name NAME IMAGE \
            -v /opt/couchbase/var:/opt/couchbase/var \
            -v /opt/couchbase/var/lib/moxi:/opt/couchbase/var/lib/moxi \
            -v /opt/couchbase/var/lib/stats:/opt/couchbase/var/lib/stats "


ENV PATH=$PATH:/opt/couchbase/bin:/opt/couchbase/bin/tools:/opt/couchbase/bin/install
COPY start.sh /start.sh

EXPOSE 8091 8092 8093 8094 8095 8096 9100 9101 9102 9103 9104 9105 9998 9999 11207 11210 11211 18091 18092 18093 18094 18095 18096 22
ARG MEMBASE_RAM_MEGS=0
RUN bash -c '[[ $MEMBASE_RAM_MEGS != 0 ]] && sed  -i "s/export PATH/export PATH\nMEMBASE_RAM_MEGS=$MEMBASE_RAM_MEGS\nexport MEMBASE_RAM_MEGS/" /opt/couchbase/bin/couchbase-server || true'

RUN echo "*        soft    nproc           unlimited" >> /etc/security/limits.conf
RUN echo "*        hard    nproc           unlimited" >> /etc/security/limits.conf
RUN echo "ulimit -u unlimited" >> /home/couchbase/.bashrc
RUN sed -i 's/--user couchbase/--user root/' /etc/init.d/couchbase-server || true

RUN chmod +x ./start.sh

ENTRYPOINT ["./start.sh"]
# pass -noinput so it doesn't drop us in the erlang shell

#VOLUME /opt/couchbase/var