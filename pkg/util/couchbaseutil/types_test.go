package couchbaseutil

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/couchbase/couchbase-operator/pkg/util/urlencoding"
)

func TestEmbedding(t *testing.T) {
	jsonString := `{
		"name": "John Doe",
		"counters": {
			"likes": 10,
			"dislikes": 5,
			"children": 2,
			"pets": 1
		}
	}`

	type ActivityCounters struct {
		Likes    int `json:"likes"`
		Dislikes int `json:"dislikes"`
	}

	type PersonalCounters struct {
		Children int `json:"children"`
		Pets     int `json:"pets"`
		Women    int `json:"women"`
	}

	type Counters struct {
		ActivityCounters
		PersonalCounters
	}

	type person struct {
		Name     string   `json:"name"`
		Counters Counters `json:"counters"`
	}

	data := []byte(jsonString)
	p := &person{}

	err := json.Unmarshal(data, &p)
	if err != nil {
		t.Errorf("Error while unmarshalling json %s", err)
	}
}

func TestCustomMarshallingOfUserRole(t *testing.T) {
	t.Parallel()

	jsonRole := `{
			"role": "data_reader",
			"bucket_name": "sample",
			"scope_name": "north_america",
			"collection_name": "users",
			"origins": [
			  {
				"type": "user"
			  }
			]
		  }`
	data := []byte(jsonRole)
	role := &UserRole{}

	err := json.Unmarshal(data, &role)
	if err != nil {
		t.Errorf("Error while unmarshalling json %s", err)
	}

	if role.Role != "data_reader" {
		t.Errorf("Role - Expected: %s, Got: %s", "data_reader", role.Role)
	}

	if role.BucketName != "sample" {
		t.Errorf("Bucket - Expected: %s, Got: %s", "sample", role.BucketName)
	}

	if role.ScopeName != "north_america" {
		t.Errorf("Scope - Expected: %s, Got: %s", "north_america", role.ScopeName)
	}

	if role.CollectionName != "users" {
		t.Errorf("Collection Expected: %s, Got: %s", "users", role.CollectionName)
	}
}

func TestCustomMarshallDoesNotFailifMissingFields(t *testing.T) {
	t.Parallel()

	jsonRole := `{
			"role": "data_reader",
			"origins": [
			  {
				"type": "user"
			  }
			]
		  }`
	data := []byte(jsonRole)
	role := &UserRole{}

	err := json.Unmarshal(data, &role)
	if err != nil {
		t.Errorf("Error while unmarshalling json %s", err)
	}

	if role.Role != "data_reader" {
		t.Errorf("Role - Expected: %s, Got: %s", "data_reader", role.Role)
	}
}

func TestCustomMarshallReplacesAsterickWithBlankOnCollection(t *testing.T) {
	t.Parallel()

	jsonRole := `{
			"role": "data_reader",
			"bucket_name": "sample",
			"scope_name": "north_america",
			"collection_name": "*",
			"origins": [
			  {
				"type": "user"
			  }
			]
		  }`
	data := []byte(jsonRole)
	role := &UserRole{}

	err := json.Unmarshal(data, &role)
	if err != nil {
		t.Errorf("Error while unmarshalling json %s", err)
	}

	if role.Role != "data_reader" {
		t.Errorf("Role - Expected: %s, Got: %s", "data_reader", role.Role)
	}

	if role.BucketName != "sample" {
		t.Errorf("Bucket - Expected: %s, Got: %s", "sample", role.BucketName)
	}

	if role.ScopeName != "north_america" {
		t.Errorf("Scope - Expected: %s, Got: %s", "north_america", role.ScopeName)
	}

	if role.CollectionName != "" {
		t.Errorf("Collection Expected: \"\", Got: %s", role.CollectionName)
	}
}

func TestCustomMarshallReplacesAsterickWithBlankOnScope(t *testing.T) {
	t.Parallel()

	jsonRole := `{
			"role": "data_reader",
			"bucket_name": "sample",
			"scope_name": "*",
			"collection_name": "*",
			"origins": [
			  {
				"type": "user"
			  }
			]
		  }`
	data := []byte(jsonRole)
	role := &UserRole{}

	err := json.Unmarshal(data, &role)
	if err != nil {
		t.Errorf("Error while unmarshalling json %s", err)
	}

	if role.Role != "data_reader" {
		t.Errorf("Role - Expected: %s, Got: %s", "data_reader", role.Role)
	}

	if role.BucketName != "sample" {
		t.Errorf("Bucket - Expected: %s, Got: %s", "sample", role.BucketName)
	}

	if role.ScopeName != "" {
		t.Errorf("Scope - Expected: \"\", Got: %s", role.ScopeName)
	}

	if role.CollectionName != "" {
		t.Errorf("Collection Expected: \"\", Got: %s", role.CollectionName)
	}
}

func TestIsBucketRoleReturnsTrueWhenRoleIsScopedToABucketOnly(t *testing.T) {
	role := &UserRole{
		Role:           "data_reader",
		BucketName:     "sample",
		ScopeName:      "",
		CollectionName: "",
	}

	if !role.IsBucketRole() {
		t.Errorf("Expected: true Got: %t", role.IsBucketRole())
	}
}

func TestIsBucketRoleReturnsFalseWhenRoleIsScopedToAScope(t *testing.T) {
	role := &UserRole{
		Role:           "data_reader",
		BucketName:     "sample",
		ScopeName:      "north_america",
		CollectionName: "",
	}

	if role.IsBucketRole() {
		t.Errorf("Expected: false Got: %t", role.IsBucketRole())
	}
}

func TestIsBucketRoleReturnsFalseWhenRoleIsScopedToACollection(t *testing.T) {
	role := &UserRole{
		Role:           "data_reader",
		BucketName:     "sample",
		ScopeName:      "north_america",
		CollectionName: "users",
	}

	if role.IsBucketRole() {
		t.Errorf("Expected: false Got: %t", role.IsBucketRole())
	}
}

func TestIsBucketRoleReturnsFalseWhenRoleIsNotScopedAtAll(t *testing.T) {
	role := &UserRole{
		Role:           "data_reader",
		BucketName:     "",
		ScopeName:      "",
		CollectionName: "",
	}

	if role.IsBucketRole() {
		t.Errorf("Expected: false Got: %t", role.IsBucketRole())
	}
}

func TestIsScopeRoleReturnsTrueWhenRoleIsScopedToABucketOnly(t *testing.T) {
	role := &UserRole{
		Role:           "data_reader",
		BucketName:     "sample",
		ScopeName:      "",
		CollectionName: "",
	}

	if role.IsScopeRole() {
		t.Errorf("Expected: false Got: %t", role.IsScopeRole())
	}
}

func TestIsScopeRoleReturnsTrueWhenRoleIsScopedToAScope(t *testing.T) {
	role := &UserRole{
		Role:           "data_reader",
		BucketName:     "sample",
		ScopeName:      "north_america",
		CollectionName: "",
	}

	if !role.IsScopeRole() {
		t.Errorf("Expected: true Got: %t", role.IsScopeRole())
	}
}

func TestIsScopeRoleReturnsFalseWhenRoleIsScopedToACollection(t *testing.T) {
	role := &UserRole{
		Role:           "data_reader",
		BucketName:     "sample",
		ScopeName:      "north_america",
		CollectionName: "users",
	}

	if role.IsScopeRole() {
		t.Errorf("Expected: false Got: %t", role.IsScopeRole())
	}
}

func TestIsScopeRoleReturnsFalseWhenRoleIsNotScopedAtAll(t *testing.T) {
	role := &UserRole{
		Role:           "data_reader",
		BucketName:     "",
		ScopeName:      "",
		CollectionName: "",
	}

	if role.IsScopeRole() {
		t.Errorf("Expected: false Got: %t", role.IsScopeRole())
	}
}

func TestIsCollectionRoleReturnsTrueWhenRoleIsScopedToABucketOnly(t *testing.T) {
	role := &UserRole{
		Role:           "data_reader",
		BucketName:     "sample",
		ScopeName:      "",
		CollectionName: "",
	}

	if role.IsCollectionRole() {
		t.Errorf("Expected: false Got: %t", role.IsCollectionRole())
	}
}

func TestIsCollectionRoleReturnsFalseWhenRoleIsScopedToAScope(t *testing.T) {
	role := &UserRole{
		Role:           "data_reader",
		BucketName:     "sample",
		ScopeName:      "north_america",
		CollectionName: "",
	}

	if role.IsCollectionRole() {
		t.Errorf("Expected: false Got: %t", role.IsCollectionRole())
	}
}

func TestIsCollectionRoleReturnsFalseWhenRoleIsScopedToACollection(t *testing.T) {
	role := &UserRole{
		Role:           "data_reader",
		BucketName:     "sample",
		ScopeName:      "north_america",
		CollectionName: "users",
	}

	if !role.IsCollectionRole() {
		t.Errorf("Expected: true Got: %t", role.IsCollectionRole())
	}
}

func TestIsCollectionRoleReturnsFalseWhenRoleIsNotScopedAtAll(t *testing.T) {
	role := &UserRole{
		Role:           "data_reader",
		BucketName:     "",
		ScopeName:      "",
		CollectionName: "",
	}

	if role.IsCollectionRole() {
		t.Errorf("Expected: false Got: %t", role.IsCollectionRole())
	}
}

func TestRoleToStrConvertsNonScopedRoleToCorrectStringRepresentation(t *testing.T) {
	role := UserRole{
		Role:           "data_reader",
		BucketName:     "",
		ScopeName:      "",
		CollectionName: "",
	}

	strRole := RoleToStr(role)

	if strRole != "data_reader" {
		t.Errorf("Expected: %s - Got: %s", "data_reader", strRole)
	}
}

func TestRoleToStrConvertsBucketScopedRoleToCorrectStringRepresentation(t *testing.T) {
	role := UserRole{
		Role:           "data_reader",
		BucketName:     "sample",
		ScopeName:      "",
		CollectionName: "",
	}

	if strRole := RoleToStr(role); strRole != "data_reader[sample]" {
		t.Errorf("Expected: %s - Got: %s", "data_reader[sample]", strRole)
	}
}

func TestRoleToStrConvertsScopeScopedRoleToCorrectStringRepresentation(t *testing.T) {
	role := UserRole{
		Role:           "data_reader",
		BucketName:     "sample",
		ScopeName:      "north_america",
		CollectionName: "",
	}

	if strRole := RoleToStr(role); strRole != "data_reader[sample:north_america]" {
		t.Errorf("Expected: %s - Got: %s", "data_reader[sample:north_america]", strRole)
	}
}

func TestRoleToStrConvertsCollectionScopedRoleToCorrectStringRepresentation(t *testing.T) {
	role := UserRole{
		Role:           "data_reader",
		BucketName:     "sample",
		ScopeName:      "north_america",
		CollectionName: "users",
	}

	if strRole := RoleToStr(role); strRole != "data_reader[sample:north_america:users]" {
		t.Errorf("Expected: %s - Got: %s", "data_reader[sample:north_america:users]", strRole)
	}
}

func TestRolesToStrConvertsMultipleRolesCorrectlyAndSortsAccordingly(t *testing.T) {
	roles := []UserRole{
		{
			Role:           "data_writer",
			BucketName:     "sample",
			ScopeName:      "north_america",
			CollectionName: "user",
		},
		{
			Role:           "data_reader",
			BucketName:     "sample",
			ScopeName:      "north_america",
			CollectionName: "user",
		},
		{
			Role:           "bucket_admin",
			BucketName:     "sample",
			ScopeName:      "",
			CollectionName: "",
		},
		{
			Role:           "replication_admin",
			BucketName:     "",
			ScopeName:      "",
			CollectionName: "",
		},
	}

	sortedStrings := RolesToStr(roles)

	if sortedStrings[0] != "bucket_admin[sample]" {
		t.Errorf("Expected: %s - Got: %s", "bucket_admin[sample]", sortedStrings[0])
	}

	if sortedStrings[1] != "data_reader[sample:north_america:user]" {
		t.Errorf("Expected: %s - Got: %s", "data_reader[sample:north_america:user]", sortedStrings[1])
	}

	if sortedStrings[2] != "data_writer[sample:north_america:user]" {
		t.Errorf("Expected: %s - Got: %s", "data_writer[sample:north_america:user]", sortedStrings[2])
	}

	if sortedStrings[3] != "replication_admin" {
		t.Errorf("Expected: %s - Got: %s", "replication_admin", sortedStrings[3])
	}
}

func TestHandleUndefinedInt64(t *testing.T) {
	testcases := []struct {
		value       int64
		expected    string
		shouldExist bool
	}{
		{
			value:       0,
			expected:    "undefined",
			shouldExist: true,
		},
		{
			value:       -1,
			shouldExist: false,
		},
		{
			value:       10000,
			expected:    "10000",
			shouldExist: true,
		},
	}

	for _, testcase := range testcases {
		data := make(map[string]string)
		handleUndefinedInt64(&data, "urlFieldKey", testcase.value)
		testUndefinedMapTestCases(t, data, "urlFieldKey", testcase.expected, testcase.shouldExist)
	}
}

func TestHandleUndefinedInt(t *testing.T) {
	testcases := []struct {
		value       int
		expected    string
		shouldExist bool
	}{
		{
			value:       0,
			expected:    "undefined",
			shouldExist: true,
		},
		{
			value:       -1,
			shouldExist: false,
		},
		{
			value:       10000,
			expected:    "10000",
			shouldExist: true,
		},
	}

	for _, testcase := range testcases {
		data := make(map[string]string)
		handleUndefinedInt(&data, "urlFieldKey", testcase.value)
		testUndefinedMapTestCases(t, data, "urlFieldKey", testcase.expected, testcase.shouldExist)
	}
}

func testUndefinedMapTestCases(t *testing.T, data map[string]string, key, expected string, shouldExist bool) {
	result, exists := data[key]

	if shouldExist {
		if !exists {
			t.Errorf("expected key 'urlFieldKey' to exist in map but it doesn't")
		} else if result != expected {
			t.Errorf("expected %q but got %q", expected, result)
		}
	} else {
		if exists {
			t.Errorf("expected key 'urlFieldKey' to not exist in map, but it exists with value %q", result)
		}
	}
}

func TestSetAutoCompactionUndefinedFieldsForEncoding(t *testing.T) {
	testcases := []struct {
		isOver71 bool
		settings AutoCompactionAutoCompactionSettings
		expected AutoCompactionAutoCompactionSettings
	}{
		{
			isOver71: false,
			settings: AutoCompactionAutoCompactionSettings{},
			expected: AutoCompactionAutoCompactionSettings{
				DatabaseFragmentationThreshold: AutoCompactionDatabaseFragmentationThreshold{
					Percentage: -1,
					Size:       -1,
				},
				ViewFragmentationThreshold: AutoCompactionViewFragmentationThreshold{
					Percentage: -1,
					Size:       -1,
				},
			},
		},
		{
			isOver71: false,
			settings: AutoCompactionAutoCompactionSettings{
				DatabaseFragmentationThreshold: AutoCompactionDatabaseFragmentationThreshold{
					Percentage: 50,
					Size:       104857600,
				},
				ViewFragmentationThreshold: AutoCompactionViewFragmentationThreshold{
					Size: 104857600,
				},
			},
			expected: AutoCompactionAutoCompactionSettings{
				DatabaseFragmentationThreshold: AutoCompactionDatabaseFragmentationThreshold{
					Percentage: 50,
					Size:       104857600,
				},
				ViewFragmentationThreshold: AutoCompactionViewFragmentationThreshold{
					Percentage: -1,
					Size:       104857600,
				},
			},
		},
		{
			isOver71: true,
			// We don't need to set explicitly set anything here as the default values are 0
			settings: AutoCompactionAutoCompactionSettings{},
			expected: AutoCompactionAutoCompactionSettings{},
		},
	}

	for _, testcase := range testcases {
		testcase.settings.SetAutoCompactionUndefinedFieldsForEncoding(testcase.isOver71)

		if !reflect.DeepEqual(testcase.settings, testcase.expected) {
			t.Errorf("expected settings did not match actual settings")
		}
	}
}

func TestEncodeReplication(t *testing.T) {
	disabled := false
	replication := Replication{
		ToCluster:       "targetCluster",
		FromBucket:      "source",
		ToBucket:        "target",
		ReplicationType: ReplicationReplicationTypeContinuous,
		Type:            ReplicationTypeXMEM,
		ConflictLogging: &ConflictLoggingSettings{
			Disabled:   &disabled,
			Bucket:     "test",
			Collection: "test",
			LoggingRules: map[string]*ConflictLoggingLocation{
				"scope1": {
					Bucket:     "bucket1",
					Collection: "s1.c1",
				},
				"scope2": {},
				"scope3": nil,
			},
		},
	}

	encoded, err := urlencoding.Marshal(replication)
	if err != nil {
		t.Errorf("Error encoding ConflictLoggingSettings: %v", err)
	}

	expected := `conflictLogging=%7B%22disabled%22%3Afalse%2C%22bucket%22%3A%22test%22%2C%22collection%22%3A%22test%22%2C%22loggingRules%22%3A%7B%22scope1%22%3A%7B%22bucket%22%3A%22bucket1%22%2C%22collection%22%3A%22s1.c1%22%7D%2C%22scope2%22%3A%7B%7D%2C%22scope3%22%3Anull%7D%7D&fromBucket=source&pauseRequested=false&replicationType=continuous&toBucket=target&toCluster=targetCluster&type=xmem`
	if string(encoded) != expected {
		t.Errorf("Expected: %s - Got: %s", expected, string(encoded))
	}
}

func TestUnmarshalEncryptionKeys(t *testing.T) {
	managedKeyJSONString := `{
        "data": {
            "keys": [
                {
                    "id": "14701477-02e9-4faf-800b-b73f4994c9a3",
                    "active": true,
                    "creationDateTime": "2025-08-21T10:39:32Z",
                    "keyMaterial": "******"
                }
            ],
            "encryptWith": "nodeSecretManager",
            "canBeCached": true,
            "autoRotation": false
        },
        "id": 27,
        "name": "testServerKey",
        "type": "cb-server-managed-aes-key-256",
        "usage": [
            "KEK-encryption",
            "bucket-encryption",
            "config-encryption",
            "log-encryption",
            "audit-encryption"
        ],
        "creationDateTime": "2025-08-21T10:39:32Z"
    }`

	managedKey := EncryptionKeyInfo{}

	err := json.Unmarshal([]byte(managedKeyJSONString), &managedKey)

	if managedKey.ID != 27 {
		t.Errorf("Expected id to be %d but got %d", 27, managedKey.ID)
	}

	if err != nil {
		t.Errorf("Error unmarshalling managed key: %v", err)
	}

	if managedKey.Type != EncryptionKeyTypeCBServerManagedAES256 {
		t.Errorf("Expected type to be %s but got %s", EncryptionKeyTypeCBServerManagedAES256, managedKey.Type)
	}

	if managedKey.AutoGenerated == nil {
		t.Errorf("Expected autoGenerated to be set but it is nil")
	}

	kmipKeyJSONString := `{
        "data": {
            "port": 1111,
            "host": "bestkmipservert",
            "reqTimeoutMs": 1900,
            "keyPath": "",
            "keyPassphrase": "******",
            "encryptionApproach": "useGet",
            "certPath": "",
            "caSelection": "skipServerCertVerification",
            "encryptWith": "nodeSecretManager",
            "historicalKeys": [],
            "activeKey": {
                "id": "26c1714d-3c4e-4b75-87e5-ab3aa4539e42",
                "kmipId": "12",
                "creationDateTime": "2025-08-21T10:39:01Z"
            },
            "encryptWithKeyId": -1
        },
        "id": 26,
        "name": "testKMIPKey",
        "type": "kmip-aes-key-256",
        "usage": [
            "KEK-encryption",
            "bucket-encryption",
            "config-encryption",
            "log-encryption",
            "audit-encryption"
        ],
        "creationDateTime": "2025-08-21T10:39:01Z"
    }`

	kmipKey := &EncryptionKeyInfo{}
	err = json.Unmarshal([]byte(kmipKeyJSONString), kmipKey)

	if err != nil {
		t.Errorf("Error unmarshalling kmip key: %v", err)
	}

	if kmipKey.Type != EncryptionKeyTypeKMIPAES256 {
		t.Errorf("Expected type to be %s but got %s", EncryptionKeyTypeKMIPAES256, kmipKey.Type)
	}

	if kmipKey.KMIP == nil {
		t.Errorf("Expected kmip to be set but it is nil")
	}

	awsKeyJSONString := `{
        "data": {
            "profile": "",
            "configFile": "",
            "useIMDS": true,
            "region": "us-east-11",
            "keyARN": "arn:aws:kms:us-west-2:111122223333:key/1234abcd-12ab-34cd-56ef-1234567890ab",
            "credentialsFile": "",
            "storedKeyIds": [
                {
                    "id": "83badbec-cc11-4d37-a5fe-c6a9738a62a0",
                    "creationDateTime": "2025-08-21T10:38:15Z"
                }
            ]
        },
        "id": 25,
        "name": "testAWSKey",
        "type": "awskms-symmetric-key",
        "usage": [
            "KEK-encryption",
            "bucket-encryption",
            "config-encryption",
            "log-encryption",
            "audit-encryption"
        ],
        "creationDateTime": "2025-08-21T10:38:15Z"
    }`

	awsKey := &EncryptionKeyInfo{}

	err = json.Unmarshal([]byte(awsKeyJSONString), awsKey)

	if err != nil {
		t.Errorf("Error unmarshalling aws key: %v", err)
	}

	if awsKey.Type != EncryptionKeyTypeAWSKMSSymmetric {
		t.Errorf("Expected type to be %s but got %s", EncryptionKeyTypeAWSKMSSymmetric, awsKey.Type)
	}

	if awsKey.AWS == nil {
		t.Errorf("Expected aws to be set but it is nil")
	}
}

func TestMarshalEncryptionKeys(t *testing.T) {
	managedKey := EncryptionKey{
		Name: "testServerKey",
		Usage: []string{
			"KEK-encryption",
			"bucket-encryption",
			"config-encryption",
			"log-encryption",
			"audit-encryption",
		},
		Type: EncryptionKeyTypeCBServerManagedAES256,
		AutoGenerated: &AutoGeneratedKeyData{
			AutoRotation: false,
			CanBeCached:  true,
			EncryptWith:  EncryptionKeyEncryptWithNodeSecretManager,
		},
	}

	encoded, err := json.Marshal(managedKey)
	if err != nil {
		t.Errorf("Error marshalling managed key: %v", err)
	}

	expected := `{"type":"cb-server-managed-aes-key-256","name":"testServerKey","usage":["KEK-encryption","bucket-encryption","config-encryption","log-encryption","audit-encryption"],"data":{"autoRotation":false,"canBeCached":true,"encryptWith":"nodeSecretManager"}}`
	if string(encoded) != expected {
		t.Errorf("Expected: %s - Got: %s", expected, string(encoded))
	}

	awsKey := EncryptionKey{
		Name: "testAWSKey",
		Usage: []string{
			"KEK-encryption",
			"bucket-encryption",
			"config-encryption",
			"log-encryption",
			"audit-encryption",
		},
		Type: EncryptionKeyTypeAWSKMSSymmetric,
		AWS: &AWSKeyData{
			Profile:         "testProfile",
			ConfigFile:      "testConfigFile",
			CredentialsFile: "testCredentialsFile",
			UseIMDS:         true,
			Region:          "us-east-11",
			KeyARN:          "arn:aws:kms:us-west-2:111122223333:key/1234abcd-12ab-34cd-56ef-1234567890ab",
		},
	}

	encoded, err = json.Marshal(awsKey)
	if err != nil {
		t.Errorf("Error marshalling aws key: %v", err)
	}

	expected = `{"type":"awskms-symmetric-key","name":"testAWSKey","usage":["KEK-encryption","bucket-encryption","config-encryption","log-encryption","audit-encryption"],"data":{"keyARN":"arn:aws:kms:us-west-2:111122223333:key/1234abcd-12ab-34cd-56ef-1234567890ab","region":"us-east-11","useIMDS":true,"credentialsFile":"testCredentialsFile","configFile":"testConfigFile","profile":"testProfile"}}`
	if string(encoded) != expected {
		t.Errorf("Expected: %s - Got: %s", expected, string(encoded))
	}

	kmipKey := EncryptionKey{
		Name: "testKMIPKey",
		Usage: []string{
			"KEK-encryption",
			"bucket-encryption",
			"config-encryption",
			"log-encryption",
			"audit-encryption",
		},
		Type: EncryptionKeyTypeKMIPAES256,
		KMIP: &KMIPKeyData{
			Host:          "testHost",
			Port:          1111,
			ReqTimeoutMs:  1900,
			KeyPath:       "testKeyPath",
			CertPath:      "testCertPath",
			KeyPassphrase: "testKeyPassphrase",
			ActiveKey: KMIPActiveKey{
				KmipID: "testKmipId",
			},
		},
	}

	encoded, err = json.Marshal(kmipKey)
	if err != nil {
		t.Errorf("Error marshalling kmip key: %v", err)
	}

	expected = `{"type":"kmip-aes-key-256","name":"testKMIPKey","usage":["KEK-encryption","bucket-encryption","config-encryption","log-encryption","audit-encryption"],"data":{"host":"testHost","port":1111,"reqTimeoutMs":1900,"activeKey":{"kmipId":"testKmipId"},"keyPath":"testKeyPath","certPath":"testCertPath","keyPassphrase":"testKeyPassphrase"}}`
	if string(encoded) != expected {
		t.Errorf("Expected: %s - Got: %s", expected, string(encoded))
	}
}
