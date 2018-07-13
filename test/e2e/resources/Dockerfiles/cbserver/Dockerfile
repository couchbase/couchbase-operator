FROM couchbase/server:5.5.0-beta
MAINTAINER Couchbase Docker Team <docker@couchbase.com>
RUN apt update
RUN apt install openssh-client openssh-server -y
RUN mkdir /var/run/sshd
RUN echo 'root:couchbase' | chpasswd
RUN sed -i 's/PermitRootLogin .*\$/PermitRootLogin yes/' /etc/ssh/sshd_config
RUN sed 's@session\s*required\s*pam_loginuid.so@session optional pam_loginuid.so@g' -i /etc/pam.d/sshd
RUN echo 'export VISIBLE=now' >> /etc/profile
EXPOSE 8091 8092 8093 8094 9100 9101 9102 9103 9104 9105 9998 9999 11207 11210 11211 18091 18092 22
RUN echo '#!/bin/bash' > myentrypoint.sh
RUN echo 'service ssh start' >> myentrypoint.sh
RUN echo '/entrypoint.sh couchbase-server' >> myentrypoint.sh
RUN chmod +x myentrypoint.sh
ENTRYPOINT ["/myentrypoint.sh\"]