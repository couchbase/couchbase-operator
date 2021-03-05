#!/bin/bash

numOfNodes=$1
cbServerVersion=$2
pyTpccDurationInSeconds=$3
currentNamespace=$(cat /var/run/secrets/kubernetes.io/serviceaccount/namespace)
nodeConfigName="${numOfNodes}node"
declare -a podIpArray

#### TPCC parameters ####
warehouses_to_load=1
warehouses_to_query=1
clients=30

if [ "$numOfNodes" == "" ]; then
    echo "Exiting: Number of nodes missing"
    exit 1
fi

if [ "$cbServerVersion" == "" ] ; then
    echo "Exiting: Couchbase-server version is NULL"
    exit 1
fi

# Install Python libraries for Kubernetes #
#git clone --recursive https://github.com/kubernetes-client/python.git k8sPython
#cd k8sPython
#python setup.py install
#cd ..

# Manipulate IPs in node.ini file #
index=0
for podIp in $(python getNodeIps.py $currentNamespace | grep "cb-example" | awk '{print $2}')
do
    if [ "$podIp" != "" ]
    then
        podIpArray+=($podIp)
    fi
done

if [ ${#podIpArray[@]} -ne $numOfNodes ]
then
    echo "Abort: IPs are less than expected pods"
    exit 1
fi

sed -i "s/ip:.*$/ip:/" ${nodeConfigName}.ini
for index in ${!podIpArray[@]}
do
    occurence=$(expr $index + 1)
    if [ $index -eq 0 ]
    then
        tr '\n' '^' < ${nodeConfigName}.ini | sed "s/ip:/ip:${podIpArray[$index]}/$occurence" | tr '^' '\n' > ${nodeConfigName}.ini.$occurence
        rm -f ${nodeConfigName}.ini
    else
        tr '\n' '^' < ${nodeConfigName}.ini.$index | sed "s/ip:/ip:${podIpArray[$index]}/$occurence" | tr '^' '\n' > ${nodeConfigName}.ini.$occurence
        rm -f ${nodeConfigName}.ini.$index
    fi
done
mv ${nodeConfigName}.ini.$numOfNodes ${nodeConfigName}.ini

#### Step 2 ####

build_version=$cbServerVersion
echo "Using couchbaser server '$cbServerVersion' for installation"
echo "Invoking install script:"
echo "  python scripts/install.py -i ${nodeConfigName}.ini -p version=${cbServerVersion},product=cb,parallel=True"

python scripts/install.py -i ${nodeConfigName}.ini -p version=${cbServerVersion},product=cb,parallel=True

#### Cluster init ####
curl -u Administrator:password -v -X POST http://${podIpArray[0]}:8091/node/controller/setupServices -d 'services=kv%2Cn1ql%2Cindex%2Cfts'
curl -u Administrator:password -v -X POST http://${podIpArray[0]}:8091/nodes/self/controller/settings -d 'path=%2Fopt%2Fcouchbase%2Fvar%2Flib%2Fcouchbase%2Fdata&index_path=%2Fopt%2Fcouchbase%2Fvar%2Flib%2Fcouchbase%2Fdata'
curl -u Administrator:password -v -X POST http://${podIpArray[0]}:8091/settings/web -d 'password=password&username=Administrator&port=SAME'
curl -u Administrator:password -v -X POST http://${podIpArray[0]}:8091/pools/default -d 'memoryQuota=3072' -d 'indexMemoryQuota=1024'

echo "Delete Buckets"
Site=http://${podIpArray[0]}:8091/pools/default/buckets/
Auth=Administrator:password
bucket=(CUSTOMER DISTRICT HISTORY ITEM NEW_ORDER ORDERS ORDER_LINE STOCK WAREHOUSE)

echo "POST /pools/default/buckets"
for i in "${bucket[@]}"
do
    echo "curl -u $Auth $Site$i"
    curl -X DELETE -u $Auth $Site$i
done

echo "rm -rf /run/data/"
rm -rf /run/data/

# This test set to memory_optimized
curl -XPOST http://${podIpArray[0]}:8091/settings/indexes -u $Auth -d storageMode=memory_optimized

echo "Sleep for 120 seconds.."
sleep 120

echo "Add nodes into cluster"
echo curl -XPOST -u Administrator:password http://${podIpArray[0]}:8091/controller/addNode -d hostname=${podIpArray[1]} -d user=Administrator -d password=password
echo curl -XPOST -u Administrator:password http://${podIpArray[0]}:8091/controller/addNode -d hostname=${podIpArray[2]} -d user=Administrator -d password=password
echo curl -XPOST -u Administrator:password http://${podIpArray[0]}:8091/controller/addNode -d hostname=${podIpArray[3]} -d user=Administrator -d password=password

curl -XPOST -u Administrator:password http://${podIpArray[0]}:8091/controller/addNode -d hostname=${podIpArray[1]} -d user=Administrator -d password=password
curl -XPOST -u Administrator:password http://${podIpArray[0]}:8091/controller/addNode -d hostname=${podIpArray[2]} -d user=Administrator -d password=password
curl -XPOST -u Administrator:password http://${podIpArray[0]}:8091/controller/addNode -d hostname=${podIpArray[3]} -d user=Administrator -d password=password

echo "Do Rebalance Operation"
echo "curl -XPOST -u Administrator:password http://${podIpArray[0]}:8091/controller/rebalance -d knownNodes='ns_1@'${podIpArray[0]},'ns_1@'${podIpArray[1]},'ns_1@'${podIpArray[2]},'ns_1@'${podIpArray[3]} -d ejectedNodes="
curl -XPOST -u Administrator:password http://${podIpArray[0]}:8091/controller/rebalance -d knownNodes='ns_1@'${podIpArray[0]},'ns_1@'${podIpArray[1]},'ns_1@'${podIpArray[2]},'ns_1@'${podIpArray[3]} -d ejectedNodes=

echo "Sleep for 120 seconds.."
sleep 120

echo "Creating Buckets"
#CUSTOMER
curl -XPOST http://${podIpArray[0]}:8091/pools/default/buckets -u Administrator:password -d name=CUSTOMER -d ramQuotaMB=212 -d authType=none -d proxyPort=11224 -d threadsNumber=8

#DISTRICT
curl -XPOST http://${podIpArray[0]}:8091/pools/default/buckets -u Administrator:password -d name=DISTRICT -d ramQuotaMB=100 -d authType=none -d proxyPort=11225 -d threadsNumber=8

#HISTORY
curl -XPOST http://${podIpArray[0]}:8091/pools/default/buckets -u Administrator:password -d name=HISTORY -d ramQuotaMB=128 -d authType=none -d proxyPort=11226 -d threadsNumber=8

#ITEM
curl -XPOST http://${podIpArray[0]}:8091/pools/default/buckets -u Administrator:password -d name=ITEM -d ramQuotaMB=128 -d authType=none -d proxyPort=11227 -d threadsNumber=8

#NEW_ORDER
curl -XPOST http://${podIpArray[0]}:8091/pools/default/buckets -u Administrator:password -d name=NEW_ORDER -d ramQuotaMB=256 -d authType=none -d proxyPort=11228 -d threadsNumber=8

#ORDERS
curl -XPOST http://${podIpArray[0]}:8091/pools/default/buckets -u Administrator:password -d name=ORDERS -d ramQuotaMB=124 -d authType=none -d proxyPort=11229 -d threadsNumber=8

#ORDER_LINE
curl -XPOST http://${podIpArray[0]}:8091/pools/default/buckets -u Administrator:password -d name=ORDER_LINE -d ramQuotaMB=1024 -d authType=none -d proxyPort=11230 -d threadsNumber=8

#STOCK
curl -XPOST http://${podIpArray[0]}:8091/pools/default/buckets -u Administrator:password -d name=STOCK -d ramQuotaMB=100 -d authType=none -d proxyPort=11231 -d threadsNumber=8

#WAREHOUSE
curl -XPOST http://${podIpArray[0]}:8091/pools/default/buckets -u Administrator:password -d name=WAREHOUSE -d ramQuotaMB=128 -d authType=none -d proxyPort=11232 -d threadsNumber=3

sleep 60

echo "drop index CUSTOMER.CU_ID_D_ID_W_ID USING GSI"
curl -XPOST http://${podIpArray[0]}:8093/query/service -d 'statement=drop index CUSTOMER.CU_ID_D_ID_W_ID USING GSI&creds=[{"user":"admin:Administrator", "pass":"password"}]"'

curl -XPOST http://${podIpArray[0]}:8093/query/service -d 'statement=drop index CUSTOMER.CU_W_ID_D_ID_LAST USING GSI&creds=[{"user":"admin:Administrator", "pass":"password"}]"'
curl -XPOST http://${podIpArray[0]}:8093/query/service -d 'statement=drop index DISTRICT.DI_ID_W_ID using gsi&creds=[{"user":"admin:Administrator", "pass":"password"}]"'
curl -XPOST http://${podIpArray[0]}:8093/query/service -d 'statement=drop index ITEM.IT_ID using gsi&creds=[{"user":"admin:Administrator", "pass":"password"}]"'
curl -XPOST http://${podIpArray[0]}:8093/query/service -d 'statement=drop index NEW_ORDER.NO_D_ID_W_ID using gsi&creds=[{"user":"admin:Administrator", "pass":"password"}]"'
curl -XPOST http://${podIpArray[0]}:8093/query/service -d 'statement=drop index ORDERS.OR_O_ID_D_ID_W_ID using gsi&creds=[{"user":"admin:Administrator", "pass":"password"}]"'
curl -XPOST http://${podIpArray[0]}:8093/query/service -d 'statement=drop index ORDERS.OR_W_ID_D_ID_C_ID using gsi&creds=[{"user":"admin:Administrator", "pass":"password"}]"'
curl -XPOST http://${podIpArray[0]}:8093/query/service -d 'statement=drop index ORDER_LINE.OL_O_ID_D_ID_W_ID using gsi&creds=[{"user":"admin:Administrator", "pass":"password"}]"'
curl -XPOST http://${podIpArray[0]}:8093/query/service -d 'statement=drop index STOCK.ST_W_ID_I_ID1 using gsi&creds=[{"user":"admin:Administrator", "pass":"password"}]"'
curl -XPOST http://${podIpArray[0]}:8093/query/service -d 'statement=drop index WAREHOUSE.WH_ID using gsi&creds=[{"user":"admin:Administrator", "pass":"password"}]"'

echo "Creating indexes on CUSTOMER"

curl -XPOST http://${podIpArray[0]}:8093/query/service -d 'statement=create index CU_ID_D_ID_W_ID on CUSTOMER(C_ID, C_D_ID, C_W_ID) using gsi WITH {"defer_build":true}&creds=[{"user":"admin:Administrator", "pass":"password"}]"'
curl -XPOST http://${podIpArray[0]}:8093/query/service -d 'statement=create index CU_W_ID_D_ID_LAST on CUSTOMER(C_W_ID, C_D_ID, C_LAST) using gsi WITH {"defer_build":true}&creds=[{"user":"admin:Administrator", "pass":"password"}]"'
curl -XPOST http://${podIpArray[0]}:8093/query/service -d 'statement=build index on CUSTOMER(CU_ID_D_ID_W_ID, CU_W_ID_D_ID_LAST) using gsi&creds=[{"user":"admin:Administrator", "pass":"password"}]"'

echo "Creating index on DISTRICT"

curl -XPOST http://${podIpArray[0]}:8093/query/service -d 'statement=create index DI_ID_W_ID on DISTRICT(D_ID, D_W_ID) using gsi WITH {"defer_build":true}&creds=[{"user":"admin:Administrator", "pass":"password"}]"'
curl -XPOST http://${podIpArray[0]}:8093/query/service -d 'statement=build index on DISTRICT(DI_ID_W_ID) using gsi&creds=[{"user":"admin:Administrator", "pass":"password"}]"'

echo "Creating index on ITEM"

curl -XPOST http://${podIpArray[0]}:8093/query/service -d 'statement=create index IT_ID on ITEM(I_ID) using gsi WITH {"defer_build":true}&creds=[{"user":"admin:Administrator", "pass":"password"}]"'
curl -XPOST http://${podIpArray[0]}:8093/query/service -d 'statement=build index on ITEM(IT_ID) using gsi&creds=[{"user":"admin:Administrator", "pass":"password"}]"'

echo "Creating index on NEW_ORDER"

curl -XPOST http://${podIpArray[0]}:8093/query/service -d 'statement=create index NO_D_ID_W_ID on NEW_ORDER(NO_O_ID, NO_D_ID, NO_W_ID) using gsi WITH {"defer_build":true}&creds=[{"user":"admin:Administrator", "pass":"password"}]"'
curl -XPOST http://${podIpArray[0]}:8093/query/service -d 'statement=build index on NEW_ORDER(NO_D_ID_W_ID) using gsi&creds=[{"user":"admin:Administrator", "pass":"password"}]"'

echo "Creating indexes on ORDERS"

curl -XPOST http://${podIpArray[0]}:8093/query/service -d 'statement=create index OR_O_ID_D_ID_W_ID on ORDERS(O_ID, O_D_ID, O_W_ID, O_C_ID) using gsi WITH {"defer_build":true}&creds=[{"user":"admin:Administrator", "pass":"password"}]"'
curl -XPOST http://${podIpArray[0]}:8093/query/service -d 'statement=create index OR_W_ID_D_ID_C_ID on ORDERS(O_W_ID, O_D_ID, O_C_ID) using gsi WITH {"defer_build":true}&creds=[{"user":"admin:Administrator", "pass":"password"}]"'
curl -XPOST http://${podIpArray[0]}:8093/query/service -d 'statement=build index on ORDERS(OR_O_ID_D_ID_W_ID, OR_W_ID_D_ID_C_ID) using gsi&creds=[{"user":"admin:Administrator", "pass":"password"}]"'

echo "Creating index on ORDER_LINE"

curl -XPOST http://${podIpArray[0]}:8093/query/service -d 'statement=create index OL_O_ID_D_ID_W_ID on ORDER_LINE(OL_O_ID, OL_D_ID, OL_W_ID) using gsi WITH {"defer_build":true}&creds=[{"user":"admin:Administrator", "pass":"password"}]"'
curl -XPOST http://${podIpArray[0]}:8093/query/service -d 'statement=build index on ORDER_LINE(OL_O_ID_D_ID_W_ID) using gsi&creds=[{"user":"admin:Administrator", "pass":"password"}]"'

echo "Creating index on STOCK"
curl -XPOST http://${podIpArray[0]}:8093/query/service -d 'statement=create index ST_W_ID_I_ID1 on STOCK(S_W_ID, S_I_ID) using gsi WITH {"defer_build":true}&creds=[{"user":"admin:Administrator", "pass":"password"}]"'
curl -XPOST http://${podIpArray[0]}:8093/query/service -d 'statement=build index on STOCK(ST_W_ID_I_ID1) using gsi&creds=[{"user":"admin:Administrator", "pass":"password"}]"'

echo "Creating index on WAREHOUSE"
curl -XPOST http://${podIpArray[0]}:8093/query/service -d 'statement=create index WH_ID on WAREHOUSE(W_ID) using gsi WITH {"defer_build":true}&creds=[{"user":"admin:Administrator", "pass":"password"}]"'
curl -XPOST http://${podIpArray[0]}:8093/query/service -d 'statement=build index on WAREHOUSE(WH_ID) using gsi&creds=[{"user":"admin:Administrator", "pass":"password"}]"'

curl -XPOST http://${podIpArray[0]}:8093/query/service -d 'statement=select keyspace_id, state from system:indexes&creds=[{"user":"admin:Administrator", "pass":"password"}]"'

curl -XPOST http://${podIpArray[0]}:8093/query/service -d 'statement=select keyspace_id, state from system:indexes where state != 'online'&creds=[{"user":"admin:Administrator", "pass":"password"}]"'

#### Step 3 ####

cd py-tpcc/pytpcc

echo "############################# Starting tpcc data load #####################################################"
python tpcc.py --warehouses ${warehouses_to_load} --clients ${clients} --no-execute n1ql --query-url ${podIpArray[0]}:8093

echo "############################# Starting query phase for $pyTpccDurationInSeconds seconds #####################################################"
python tpcc.py --no-load --warehouses ${warehouses_to_query} --duration $pyTpccDurationInSeconds --clients ${clients} --query-url ${podIpArray[0]}:8093 n1ql

#### Step 4 ####
#sleep 360000000
#wget http://172.23.120.24/builds/latestbuilds/analytics/1.0.0/${Analytics_Build}/couchbase-analytics-1.0.0-${Analytics_Build}-generic.zip
#cd cbas-install/cbas
#curl -v -data http://localhost:8095/analytics/shutdown
#./samples/local/bin/stop-sample-cluster.sh
#cd ../..
#rm -rf cbas-install
#mkdir cbas-install
#cd cbas-install/
#unzip ../couchbase-analytics-1.0.0-${Analytics_Build}-generic.zip
#cd cbas
#./samples/local/bin/start-sample-cluster.sh

echo "Tpcc: command completed"

while true ; do sleep 1000 ; done

