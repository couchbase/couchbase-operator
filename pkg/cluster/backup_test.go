package cluster

import (
	"sort"
	"testing"

	v1 "k8s.io/api/batch/v1"
)

func TestBackupResourceListUnique(t *testing.T) {
	cronjob := &v1.CronJob{}

	cronjobs := backupResourcesList{
		backupResources{
			name: "cows",
		},
		backupResources{
			name: "go",
		},
		backupResources{
			name:        "moo",
			fullCronJob: cronjob,
		},
	}

	jobs := backupResourcesList{
		backupResources{
			name: "foo",
		},
		backupResources{
			name: "bar",
		},
		backupResources{
			name:               "moo",
			immediateBackupJob: &v1.Job{},
		},
	}

	expected := backupResourcesList{
		backupResources{
			name: "foo",
		},
		backupResources{
			name: "bar",
		},
		backupResources{
			name: "cows",
		},
		backupResources{
			name: "go",
		},
		backupResources{
			name:        "moo",
			fullCronJob: cronjob,
		},
	}

	unique := cronjobs.unique(jobs)

	// sort them
	sort.Slice(unique, func(i int, j int) bool {
		return unique[i].name > unique[j].name
	})
	sort.Slice(expected, func(i int, j int) bool {
		return expected[i].name > expected[j].name
	})

	for i := range unique {
		if unique[i] != expected[i] {
			t.Fatalf("unexpected element, expected %v but found %v", expected[i], unique[i])
		}
	}
}
