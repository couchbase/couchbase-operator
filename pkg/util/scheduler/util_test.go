package scheduler

import (
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
