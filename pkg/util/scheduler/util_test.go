package scheduler

import (
	"slices"
	"testing"
)

func TestLexicalServerGroups(t *testing.T) {
	lsg := newLexicalServerGroups()

	sg := []string{serverGroup2, serverGroup3, serverGroup1}
	for _, sg := range sg {
		lsg.addGroup(sg)
	}

	if len(lsg.sizes()) != 3 {
		t.Errorf("Expected 3, got %d", lsg.sizes())
	}

	lsg.addGroupIfDoesntExist(serverGroup1)

	if len(lsg.sizes()) != 3 {
		t.Errorf("Expected 3, got %d", lsg.sizes())
	}

	for _, sg := range sg {
		if !lsg.groupExists(sg) {
			t.Errorf("Expected true, got false")
		}
	}

	if lsg.smallestGroup() != serverGroup1 {
		t.Errorf("Expected %s, got %s", serverGroup1, lsg.smallestGroup())
	}

	if lsg.largestGroup() != serverGroup3 {
		t.Errorf("Expected %s, got %s", serverGroup3, lsg.largestGroup())
	}
}

func TestLexicalServerGroupsSizeOrderedGroups(t *testing.T) {
	lsg := newLexicalServerGroups()

	sg := []string{serverGroup2, serverGroup3, serverGroup1}
	for _, sg := range sg {
		lsg.addGroup(sg)
	}

	if !slices.Equal(lsg.sizeOrderedGroups(), []string{serverGroup1, serverGroup2, serverGroup3}) {
		t.Errorf("Expected %v, got %v", sg, lsg.sizeOrderedGroups())
	}

	lsg.getGroup(serverGroup1).push(podName1)

	if !slices.Equal(lsg.sizeOrderedGroups(), []string{serverGroup2, serverGroup3, serverGroup1}) {
		t.Errorf("Expected %v, got %v", sg, lsg.sizeOrderedGroups())
	}

	lsg.getGroup(serverGroup2).push(podName3)

	if !slices.Equal(lsg.sizeOrderedGroups(), []string{serverGroup3, serverGroup1, serverGroup2}) {
		t.Errorf("Expected %v, got %v", sg, lsg.sizeOrderedGroups())
	}

	lsg.getGroup(serverGroup1).push(podName2)

	if !slices.Equal(lsg.sizeOrderedGroups(), []string{serverGroup3, serverGroup2, serverGroup1}) {
		t.Errorf("Expected %v, got %v", sg, lsg.sizeOrderedGroups())
	}
}

func TestOrderedServerGroups(t *testing.T) {
	lsg := newOrderedServerGroups()

	sg := []string{serverGroup4, serverGroup2, serverGroup3, serverGroup1}
	for _, sg := range sg {
		lsg.addGroup(sg)
	}

	if l := len(lsg.sizes()); l != len(sg) {
		t.Errorf("Expected %d, got %d", len(sg), l)
	}

	lsg.addGroupIfDoesntExist(serverGroup1)

	if l := len(lsg.sizes()); l != len(sg) {
		t.Errorf("Expected %d, got %d", len(sg), l)
	}

	for _, sg := range sg {
		if !lsg.groupExists(sg) {
			t.Errorf("Expected true, got false")
		}
	}

	if lsg.smallestGroup() != sg[0] {
		t.Errorf("Expected %s, got %s", sg[0], lsg.smallestGroup())
	}

	if lsg.largestGroup() != sg[len(sg)-1] {
		t.Errorf("Expected %s, got %s", sg[len(sg)-1], lsg.largestGroup())
	}
}

func TestOrderedServerGroupsSizeOrderedGroups(t *testing.T) {
	lsg := newOrderedServerGroups()

	sg := []string{serverGroup4, serverGroup1, serverGroup3, serverGroup2}
	for _, sg := range sg {
		lsg.addGroup(sg)
	}

	expected := []string{serverGroup4, serverGroup1, serverGroup3, serverGroup2}
	if !slices.Equal(lsg.sizeOrderedGroups(), expected) {
		t.Errorf("Expected %v, got %v", expected, lsg.sizeOrderedGroups())
	}

	lsg.getGroup(serverGroup1).push(podName1)

	expected = []string{serverGroup4, serverGroup3, serverGroup2, serverGroup1}
	if !slices.Equal(lsg.sizeOrderedGroups(), expected) {
		t.Errorf("Expected %v, got %v", expected, lsg.sizeOrderedGroups())
	}

	lsg.getGroup(serverGroup2).push(podName2)

	expected = []string{serverGroup4, serverGroup3, serverGroup1, serverGroup2}
	if !slices.Equal(lsg.sizeOrderedGroups(), expected) {
		t.Errorf("Expected %v, got %v", expected, lsg.sizeOrderedGroups())
	}

	lsg.getGroup(serverGroup3).push(podName3)
	lsg.getGroup(serverGroup3).push(podName4)

	expected = []string{serverGroup4, serverGroup1, serverGroup2, serverGroup3}
	if !slices.Equal(lsg.sizeOrderedGroups(), expected) {
		t.Errorf("Expected %v, got %v", expected, lsg.sizeOrderedGroups())
	}
}
