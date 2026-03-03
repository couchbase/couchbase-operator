/*
Copyright 2018-Present Couchbase, Inc.

Use of this software is governed by the Business Source License included in
the file licenses/BSL-Couchbase.txt.  As of the Change Date specified in that
file, in accordance with the Business Source License, use of this software will
be governed by the Apache License, Version 2.0, included in the file
licenses/APL2.txt.
*/

package backend

import (
	"github.com/couchbase/couchbase-operator/pkg/info/config"
)

// Backend is an abstraction of a backend file writer service.  It may be
// implemented as a native file system, an archive, or compressed archive.
type Backend interface {
	// WriteFile writes a file to the backend.  The file is immutable once
	// created
	WriteFile(path, data string) error
	// Close indicates the backend should perform any shutdown operations,
	// free any resources and print any information
	Close() error
}

// New is a factory function which returns the selected backend based on
// configuration parameters.
func New(config *config.Configuration) (Backend, error) {
	return NewTGZ(config)
}
