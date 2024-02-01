package couchbaseutil

import (
	"fmt"
	"reflect"
	"testing"
)

func TestGroupByServerConfigs(t *testing.T) {
	ms := NewMemberSet()
	expectedGroupings := map[string]MemberSet{}
	expectedGroupings["config1"] = addMembersWithConfig(ms, "config1", 3)
	expectedGroupings["config2"] = addMembersWithConfig(ms, "config2", 1)
	expectedGroupings["config3"] = addMembersWithConfig(ms, "config3", 2)

	if groupings := ms.GroupByServerConfigs(); !reflect.DeepEqual(expectedGroupings, groupings) {
		t.Errorf("expected groupings to be: %v , but got: %v", expectedGroupings, groupings)
	}
}

func addMembersWithConfig(ms MemberSet, configName string, numMembers int) MemberSet {
	rv := NewMemberSet()

	for i := 0; i < numMembers; i++ {
		m := NewMember("test-namespace", "test-cluster", fmt.Sprintf("member-%v", len(ms)), "7.3.2", configName, false)
		ms.Add(m)
		rv.Add(m)
	}

	return rv
}
