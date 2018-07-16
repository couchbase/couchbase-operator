package e2eutil

import (
	"encoding/json"
	"errors"
	"io/ioutil"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type cbClusterInfo struct {
	Uuid string `json:"uuid"`
}

type cbBucketInfo struct {
	BucketType string `json:"bucketType"`
	BasicStats struct {
		DataUsed         int     `json:"dataUsed"`
		DiskFetches      int     `json:"diskFetches"`
		DiskUsed         int     `json:"diskUsed"`
		ItemCount        int     `json:"itemCount"`
		MemUsed          int     `json:"memUsed"`
		OpsPerSec        float32 `json:"opsPerSec"`
		QuotaPercentUsed float32 `json:"quotaPercentUsed"`
	} `json:"basicStats"`
}

type xdcrRemoteClusterReference struct {
	Name     string `json:"name"`
	Uri      string `json:"uri"`
	Hostname string `json:"hostname"`
	Username string `json:"username"`
}

func GenerateHttpRequest(requestType, hostUrl, hostUsername, hostPassword string, reqParams *strings.Reader) ([]byte, error) {
	var request *http.Request
	var err error

	if reqParams == nil {
		request, err = http.NewRequest(requestType, hostUrl, nil)
	} else {
		request, err = http.NewRequest(requestType, hostUrl, reqParams)
	}
	if err != nil {
		return nil, errors.New("Http request failed: " + err.Error())
	}

	request.SetBasicAuth(hostUsername, hostPassword)
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return nil, errors.New("Failed to " + err.Error())
	}
	defer response.Body.Close()
	responseBody := response.Body
	responseData, _ := ioutil.ReadAll(responseBody)
	if response.StatusCode != http.StatusOK {
		return nil, errors.New("Remote call failed with response: " + response.Status + ", " + string(responseData))
	}
	return responseData, nil
}

func GetBucketInfo(hostUrl, bucketName, hostUsername, hostPassword string) (cbBucketInfo, error) {
	//curl -u [admin]:[password] http://[localhost]:8091/pools/default/buckets/[bucket-name]
	var bucketInfo cbBucketInfo
	hostUrl = "http://" + hostUrl + "/pools/default/buckets/" + bucketName
	responseData, err := GenerateHttpRequest("GET", hostUrl, hostUsername, hostPassword, nil)
	if err != nil {
		return bucketInfo, err
	}
	err = json.Unmarshal(responseData, &bucketInfo)
	return bucketInfo, err
}

func FlushBucket(hostUrl, bucketName, hostUsername, hostPassword string) ([]byte, error) {
	//curl -X POST -u [admin]:[password] [localhost]:8091/pools/default/buckets/[bucket-name]/controller/doFlush
	hostUrl = "http://" + hostUrl + "/pools/default/buckets/" + bucketName + "/controller/doFlush"
	return GenerateHttpRequest("POST", hostUrl, hostUsername, hostPassword, nil)
}

func PopulateBucket(hostUrl, bucketName, hostUsername, hostPassword string, numOfItems, docStartIndex int) ([]byte, error) {
	//curl 'http://172.23.121.211:8091/pools/default/buckets/sample/docs/1' --data 'flags=24&value={"city":"chennai"}'  -u Administrator:password
	hostUrl = "http://" + hostUrl + "/pools/default/buckets/" + bucketName + "/docs/"
	numOfItems += docStartIndex - 1
	for docIndex := docStartIndex; docIndex <= numOfItems; docIndex++ {
		currReqUrl := hostUrl + strconv.Itoa(docIndex)
		flagStr := "flags=24"
		docData := "value={\"docIndex\":\"TestData-" + strconv.Itoa(docIndex) + "\"}"
		reqParamList := []string{flagStr, docData}
		reqParams := strings.NewReader(strings.Join(reqParamList, "&"))

		if responseBody, err := GenerateHttpRequest("POST", currReqUrl, hostUsername, hostPassword, reqParams); err != nil {
			return responseBody, err
		}
	}
	return nil, nil
}

func VerifyDocCountInBucket(url, bucketName, userName, password string, reqNumOfDocs, maxRetries int) error {
	docMatched := false
	currDocCount := 0
	for retryCount := 0; retryCount < maxRetries; retryCount++ {
		bucketStat, err := GetBucketInfo(url, bucketName, userName, password)
		if err != nil {
			return errors.New("Failed to get bucket info: " + err.Error())
		}
		currDocCount = bucketStat.BasicStats.ItemCount
		if currDocCount == reqNumOfDocs {
			docMatched = true
			break
		}
		time.Sleep(time.Second * 5)
	}
	if !docMatched {
		return errors.New("Replication count did not match. Item count is " + strconv.Itoa(currDocCount) + " expecting " + strconv.Itoa(reqNumOfDocs))
	}
	return nil
}

func GetRemoteUuid(hostUrl, cbUsername, cbPassword string) (uuid string, err error) {
	// curl -u [admin]:[password] http://[localhost]:8091/pools
	hostUrl = "http://" + hostUrl + "/pools"
	responseData, err := GenerateHttpRequest("GET", hostUrl, cbUsername, cbPassword, nil)
	if err != nil {
		return
	}
	var cbClusterInfo cbClusterInfo
	if err = json.Unmarshal(responseData, &cbClusterInfo); err != nil {
		return
	}
	uuid = cbClusterInfo.Uuid
	return
}

func CreateDestClusterReference(hostUrl, hostUsername, hostPassword, destClusterUuid, destClusterName, destClusterHostUrl, destClusterUsername, destClusterPassword string) ([]byte, error) {
	// curl -v -u Administrator:password http://192.168.99.100:32589/pools/default/remoteClusters -d 'uuid=08c5dc4ff21fdcda30ca5b1281f9fc0f'
	// -d 'name=test-couchbase-zcrxp' -d 'hostname=192.168.99.100:31943' -d 'username=Administrator' -d 'password=password'
	hostUrl = "http://" + hostUrl + "/pools/default/remoteClusters"
	destClusterUuid = "uuid=" + destClusterUuid
	destClusterName = "name=" + destClusterName
	destClusterHostUrl = "hostname=" + destClusterHostUrl
	destClusterUsername = "username=" + destClusterUsername
	destClusterPassword = "password=" + destClusterPassword

	reqParamList := []string{destClusterUuid, destClusterName, destClusterHostUrl, destClusterUsername, destClusterPassword}
	reqParams := strings.NewReader(strings.Join(reqParamList, "&"))

	return GenerateHttpRequest("POST", hostUrl, hostUsername, hostPassword, reqParams)
}

func GetXdcrClusterReferences(hostUrl, hostUsername, hostPassword string) (xdcrClusterRefList []xdcrRemoteClusterReference, err error) {
	// Get all XDCR cluster reference
	hostUrl = "http://" + hostUrl + "/pools/default/remoteClusters"
	responseData, err := GenerateHttpRequest("POST", hostUrl, hostUsername, hostPassword, nil)
	if err != nil {
		return xdcrClusterRefList, err
	}
	json.Unmarshal(responseData, &xdcrClusterRefList)
	return xdcrClusterRefList, err
}

func DeleteXdcrClusterReferences(hostUrl, hostUsername, hostPassword string, xdcrClusterRef xdcrRemoteClusterReference) error {
	// Stop replication of default bucket
	hostUrl = "http://" + xdcrClusterRef.Hostname + "/controller/cancelXDCR/" + xdcrClusterRef.Uri + "%2Fdeafult%2Fdefault"
	GenerateHttpRequest("DELETE", hostUrl, hostUsername, hostPassword, nil)

	// Delete XDCR reference
	hostUrl = "http://" + xdcrClusterRef.Hostname + xdcrClusterRef.Uri
	_, err := GenerateHttpRequest("DELETE", hostUrl, hostUsername, hostPassword, nil)
	return err
}

func CreateXdcrBucketReplication(hostUrl, hostUsername, hostPassword, remoteClusterName, fromBucketName, destBucketName, versionType string) ([]byte, error) {
	// curl -v -X POST -u Administrator:password http://192.168.99.100:32589/controller/createReplication -d fromBucket=default
	//  -d toCluster=test-couchbase-zcrxp -d toBucket=default  -d replicationType=continuous
	fromBucketName = "fromBucket=" + fromBucketName
	remoteClusterName = "toCluster=" + remoteClusterName
	destBucketName = "toBucket=" + destBucketName
	replicationType := "replicationType=continuous"
	versionType = "type=" + versionType

	hostUrl = "http://" + hostUrl + "/controller/createReplication"
	reqParamList := []string{fromBucketName, remoteClusterName, destBucketName, replicationType, versionType}
	reqParams := strings.NewReader(strings.Join(reqParamList, "&"))

	return GenerateHttpRequest("POST", hostUrl, hostUsername, hostPassword, reqParams)
}
