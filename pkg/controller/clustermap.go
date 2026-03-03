/*
Copyright 2018-Present Couchbase, Inc.

Use of this software is governed by the Business Source License included in
the file licenses/BSL-Couchbase.txt.  As of the Change Date specified in that
file, in accordance with the Business Source License, use of this software will
be governed by the Apache License, Version 2.0, included in the file
licenses/APL2.txt.
*/

package controller

import (
	"sync"

	"github.com/couchbase/couchbase-operator/pkg/cluster"
)

type ManagedClusters struct {
	Values map[string]*cluster.Cluster
	Lock   sync.Mutex
}

func CreateManagedClusters() *ManagedClusters {
	return &ManagedClusters{
		Values: make(map[string]*cluster.Cluster),
		Lock:   sync.Mutex{},
	}
}

func (m *ManagedClusters) Delete(key string) {
	m.Lock.Lock()
	delete(m.Values, key)
	m.Lock.Unlock()
}

func (m *ManagedClusters) Load(key string) (value *cluster.Cluster, ok bool) {
	m.Lock.Lock()
	c, ok := m.Values[key]
	m.Lock.Unlock()
	return c, ok
}

func (m *ManagedClusters) Range(f func(key string, value *cluster.Cluster) bool) {
	m.Lock.Lock()

	for k, v := range m.Values {
		if !f(k, v) {
			break
		}
	}

	m.Lock.Unlock()
}

func (m *ManagedClusters) Store(key string, value *cluster.Cluster) {
	m.Lock.Lock()
	m.Values[key] = value
	m.Lock.Unlock()
}
