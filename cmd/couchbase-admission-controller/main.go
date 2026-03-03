/*
Copyright 2022-Present Couchbase, Inc.

Use of this software is governed by the Business Source License included in
the file licenses/BSL-Couchbase.txt.  As of the Change Date specified in that
file, in accordance with the Business Source License, use of this software will
be governed by the Apache License, Version 2.0, included in the file
licenses/APL2.txt.
*/

package main

import (
	"flag"

	"github.com/couchbase/couchbase-operator/pkg/admission"
)

// main initializes the system then starts a HTTPS server to process requests.
func main() {
	// Parse command line parameters
	config := &admission.Config{}
	config.AddFlags()

	flag.Parse()

	// FIre up the server
	admission.Serve(config)
}
