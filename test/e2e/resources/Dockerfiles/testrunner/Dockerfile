FROM ashwin2002/testrunner-cloud:baseimage
WORKDIR /
# install python deps
RUN pip2 install --upgrade packaging appdirs
RUN pip install -U pip setuptools
RUN pip install paramiko && pip install gevent && pip install boto && pip install httplib2 && pip install pyyaml && pip install couchbase
# build libcouchbase
RUN git clone git://github.com/couchbase/libcouchbase.git && mkdir libcouchbase/build
WORKDIR libcouchbase/build
RUN ../cmake/configure --prefix=/usr && make && make install
WORKDIR /
RUN git clone git://github.com/couchbase/testrunner.git --branch master
WORKDIR testrunner

# for tpcc job only
RUN git clone https://github.com/couchbaselabs/py-tpcc.git

COPY 4node.ini 4node.ini
COPY testcases.conf testcases.conf
COPY getNodeIps.py getNodeIps.py
COPY tpcc_entrypoint.sh entrypoint.sh
RUN chmod +x ./entrypoint.sh
ENTRYPOINT ["./entrypoint.sh", "4", "5.5.0-beta", "3600"]

FROM ashwin2002/testrunner-cloud:baseimage
WORKDIR /
RUN rm -rf testrunner libcouchbase
# install python deps
RUN pip2 install --upgrade packaging appdirs
RUN pip install -U pip setuptools
RUN pip install paramiko && pip install gevent && pip install boto && pip install httplib2 && pip install pyyaml && pip install couchbase
# build libcouchbase
RUN git clone git://github.com/couchbase/libcouchbase.git && mkdir libcouchbase/build
WORKDIR libcouchbase/build
RUN ../cmake/configure --prefix=/usr && make && make install
WORKDIR /
RUN git clone git://github.com/couchbase/testrunner.git --branch master
WORKDIR testrunner
COPY 1node.ini 1node.ini
COPY testcases.conf testcases.conf
COPY getNodeIps.py getNodeIps.py
COPY entrypoint.sh entrypoint.sh
RUN chmod +x ./entrypoint.sh
ENTRYPOINT ["./entrypoint.sh", "1"]