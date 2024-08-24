package managedk8sservices

import (
	"fmt"
	"sync"
)

type AKSSessionStore struct {
	AKSSessions map[string]*AKSSession
	lock        sync.Mutex
}

type AKSSession struct {
	Cred        *ManagedServiceCredentials
	Region      string
	ClusterName string
}

func NewAKSSession(managedSvcCred *ManagedServiceCredentials) (*AKSSession, error) {
	return &AKSSession{
		Cred:        managedSvcCred,
		Region:      managedSvcCred.AKSCredentials.aksRegion,
		ClusterName: managedSvcCred.ClusterName,
	}, nil
}

func ConfigAKSSessionStore() ManagedService {
	return &AKSSessionStore{
		AKSSessions: make(map[string]*AKSSession),
		lock:        sync.Mutex{},
	}
}

func NewAKSSessionStore() *AKSSessionStore {
	return &AKSSessionStore{
		AKSSessions: make(map[string]*AKSSession),
		lock:        sync.Mutex{},
	}
}

func getAKSKey(managedSvcCred *ManagedServiceCredentials) string {
	key := managedSvcCred.ClusterName + ":" + managedSvcCred.EKSCredentials.eksRegion
	return key
}

func (ass *AKSSessionStore) SetSession(managedSvcCred *ManagedServiceCredentials) error {
	defer ass.lock.Unlock()
	ass.lock.Lock()

	if _, ok := ass.AKSSessions[getAKSKey(managedSvcCred)]; !ok {
		aksSess, err := NewAKSSession(managedSvcCred)
		if err != nil {
			return fmt.Errorf("set eks session store: %w", err)
		}

		ass.AKSSessions[getAKSKey(managedSvcCred)] = aksSess
	}

	return nil
}

func (ass *AKSSessionStore) GetSession(managedSvcCred *ManagedServiceCredentials) (*AKSSession, error) {
	if _, ok := ass.AKSSessions[getAKSKey(managedSvcCred)]; !ok {
		err := ass.SetSession(managedSvcCred)
		if err != nil {
			return nil, fmt.Errorf("get aks session: %w", err)
		}
	}

	return ass.AKSSessions[getAKSKey(managedSvcCred)], nil
}

func (ass *AKSSessionStore) Check(managedSvcCred *ManagedServiceCredentials) error {
	// TODO implement me
	panic("implement me")
}

func (ass *AKSSessionStore) GetInstancesByK8sNodeName(managedSvcCred *ManagedServiceCredentials, nodeNames []string) ([]string, error) {
	// TODO implement me
	panic("implement me")
}

// ================================================
// ====== Methods implemented by AKSSession ======
// ================================================
