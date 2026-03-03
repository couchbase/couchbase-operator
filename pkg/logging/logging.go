/*
Copyright 2021-Present Couchbase, Inc.

Use of this software is governed by the Business Source License included in
the file licenses/BSL-Couchbase.txt.  As of the Change Date specified in that
file, in accordance with the Business Source License, use of this software will
be governed by the Apache License, Version 2.0, included in the file
licenses/APL2.txt.
*/

package logging

import (
	"flag"

	"github.com/go-logr/logr"

	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

// Options is a simple wrapper around the controller runtime logging.
type Options struct {
	zap.Options
}

// AddFlagSet binds options to the flagset.
func (o *Options) AddFlagSet(f *flag.FlagSet) {
	o.Options.BindFlags(f)
}

// New creates a new Zap logr.
func New(o *Options) logr.Logger {
	return zap.New(zap.UseFlagOptions(&o.Options))
}
