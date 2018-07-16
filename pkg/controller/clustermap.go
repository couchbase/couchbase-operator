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
