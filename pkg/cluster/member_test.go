package cluster

import (
	"testing"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/util/couchbaseutil"
)

func TestOfflineUpgradeCandidatesSkipsRollback(t *testing.T) {
	t.Parallel()

	const (
		oldImage = "couchbase/server:7.6.4"
		newImage = "couchbase/server:8.0.0"
	)

	members := func(images ...string) couchbaseutil.MemberSet {
		ms := couchbaseutil.MemberSet{}
		for i, image := range images {
			version := "7.6.4"
			if image == newImage {
				version = "8.0.0"
			}

			ms.Add(couchbaseutil.NewMember("", "", couchbaseutil.CreateMemberName("cb", i), version, "", false, image))
		}

		return ms
	}

	testcases := []struct {
		name    string
		target  string
		members couchbaseutil.MemberSet
		want    int
	}{
		{
			name:    "upgrade re-stamps old members",
			target:  newImage,
			members: members(oldImage, oldImage, newImage),
			want:    2,
		},
		{
			name:    "rollback after a mixed wake re-stamps nothing",
			target:  oldImage,
			members: members(newImage, newImage, newImage, oldImage),
			want:    0,
		},
	}

	for _, testcase := range testcases {
		c := &Cluster{
			cluster: &couchbasev2.CouchbaseCluster{Spec: couchbasev2.ClusterSpec{Image: testcase.target}},
			members: testcase.members,
		}

		candidates, blocked, err := c.offlineUpgradeCandidates()
		if err != nil || blocked {
			t.Fatalf("%s: unexpected err %v, blocked %v", testcase.name, err, blocked)
		}

		if len(candidates) != testcase.want {
			t.Errorf("%s: got %d candidates, want %d", testcase.name, len(candidates), testcase.want)
		}
	}
}

func TestOfflineTriggerNeedsVolumes(t *testing.T) {
	t.Parallel()

	offline := couchbasev2.OfflineUpgrade

	c := &Cluster{
		cluster: &couchbasev2.CouchbaseCluster{Spec: couchbasev2.ClusterSpec{
			Image:          "couchbase/server:8.0.0",
			Servers:        []couchbasev2.ServerConfig{{Name: "data", Size: 1, Services: []couchbasev2.Service{couchbasev2.DataService}}},
			UpgradeProcess: &offline,
		}},
		members: couchbaseutil.MemberSet{},
	}
	c.members.Add(couchbaseutil.NewMember("", "", "cb-0000", "7.6.4", "data", false, "couchbase/server:7.6.4"))

	// Past the guard it would go on to hibernate, i.e. wipe the cluster.
	if err := c.reconcileHibernation(); err != nil || c.cluster.HasCondition(couchbasev2.ClusterConditionHibernating) {
		t.Errorf("expected no hibernation without volumes, got err %v", err)
	}
}

func TestOfflineUpgradeCandidatesSpanAllOrderGroups(t *testing.T) {
	t.Parallel()

	c := &Cluster{
		cluster: &couchbasev2.CouchbaseCluster{Spec: couchbasev2.ClusterSpec{
			Image: "couchbase/server:8.0.0",
			Servers: []couchbasev2.ServerConfig{
				{Name: "data", Size: 2, Services: []couchbasev2.Service{couchbasev2.DataService}},
				{Name: "index", Size: 2, Services: []couchbasev2.Service{couchbasev2.IndexService}},
			},
			Upgrade: &couchbasev2.UpgradeSpec{
				PreviousVersionPodCount: 1,
				UpgradeOrderType:        couchbasev2.UpgradeOrderTypeServerClasses,
				UpgradeOrder:            []string{"data", "index"},
			},
		}},
		members: couchbaseutil.MemberSet{},
	}

	for i, class := range []string{"data", "data", "index", "index"} {
		c.members.Add(couchbaseutil.NewMember("", "", couchbaseutil.CreateMemberName("cb", i), "7.6.4", class, false, "couchbase/server:7.6.4"))
	}

	candidates, blocked, err := c.offlineUpgradeCandidates()
	if err != nil || blocked {
		t.Fatalf("unexpected err %v, blocked %v", err, blocked)
	}

	// One outage covers every group; only the last in order is held back.
	if len(candidates) != 3 {
		t.Fatalf("got %d candidates, want 3", len(candidates))
	}

	for i, class := range []string{"data", "data", "index"} {
		if candidates[i].Config() != class {
			t.Errorf("candidate %d: got class %s, want %s", i, candidates[i].Config(), class)
		}
	}
}
