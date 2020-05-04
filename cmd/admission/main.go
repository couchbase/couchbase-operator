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
