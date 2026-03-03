/*
Copyright 2021-Present Couchbase, Inc.

Use of this software is governed by the Business Source License included in
the file licenses/BSL-Couchbase.txt.  As of the Change Date specified in that
file, in accordance with the Business Source License, use of this software will
be governed by the Apache License, Version 2.0, included in the file
licenses/APL2.txt.
*/

package couchbaseutil

import (
	"encoding/json"
	"testing"
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
