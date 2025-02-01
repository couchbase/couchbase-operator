package assets

import "sync"

type CouchbaseCRD struct {
	crdName string
	version string

	mu sync.Mutex
}

func NewCouchbaseCRD(crdName, version string) *CouchbaseCRD {
	return &CouchbaseCRD{
		crdName: crdName,
		version: version,
	}
}

/*
-----------------------------------------------------------------
-----------------------------------------------------------------
-----------------------------------------------------------------
--------Getter and GetterSetter Interface Definitions------------
-----------------------------------------------------------------
-----------------------------------------------------------------
-----------------------------------------------------------------
*/

type CouchbaseCRDGetter interface {
	GetCRDName() string
	GetVersion() string
}

type CouchbaseCRDGetterSetter interface {
	// Getters
	GetCRDName() string
	GetVersion() string

	// Setters
	SetCRDName(crdName string) error
	SetVersion(version string) error
}

/*
-----------------------------------------------------------------
-----------------------------------------------------------------
-----------------------------------------------------------------
-------------------CouchbaseCRD Getters--------------------------
-----------------------------------------------------------------
-----------------------------------------------------------------
-----------------------------------------------------------------
*/

func (crd *CouchbaseCRD) GetCRDName() string {
	crd.mu.Lock()
	defer crd.mu.Unlock()
	return crd.crdName
}

func (crd *CouchbaseCRD) GetVersion() string {
	crd.mu.Lock()
	defer crd.mu.Unlock()
	return crd.version
}

/*
-----------------------------------------------------------------
-----------------------------------------------------------------
-----------------------------------------------------------------
-------------------CouchbaseCRD Setters--------------------------
-----------------------------------------------------------------
-----------------------------------------------------------------
-----------------------------------------------------------------
*/

func (crd *CouchbaseCRD) SetCRDName(crdName string) error {
	crd.mu.Lock()
	defer crd.mu.Unlock()
	crd.crdName = crdName
	return nil
}

func (crd *CouchbaseCRD) SetVersion(version string) error {
	crd.mu.Lock()
	defer crd.mu.Unlock()
	crd.version = version
	return nil
}
