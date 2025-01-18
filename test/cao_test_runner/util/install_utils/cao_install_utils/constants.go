package caoinstallutils

import "github.com/couchbase/couchbase-operator/test/cao_test_runner/assets"

const (
	latestBuildsURL string = "http://latestbuilds.service.couchbase.com/builds/latestbuilds/couchbase-operator"
	releasesURL     string = "http://latestbuilds.service.couchbase.com/builds/releases/couchbase-operator/"
)

const (
	operatorPath string = "couchbase-autonomous-operator_%s-%s-%s-%s-%s%s"
)

var ext = map[assets.OperatingSystemType]string{
	assets.Linux:   ".tar.gz",
	assets.MacOS:   ".zip",
	assets.Windows: ".zip",
}

var releasedBuilds = map[string]string{
	"1.2.1": "505",
	"1.2.2": "513",
	"2.0.0": "317",
	"2.0.1": "130",
	"2.0.2": "121",
	"2.0.3": "115",
	"2.1.0": "259",
	"2.2.0": "250",
	"2.2.1": "126",
	"2.2.2": "110",
	"2.2.3": "102",
	"2.2.4": "106",
	"2.3.0": "301",
	"2.3.1": "118",
	"2.3.2": "104",
	"2.4.0": "194",
	"2.4.1": "130",
	"2.4.2": "105",
	"2.4.3": "119",
	"2.5.0": "180",
	"2.5.1": "112",
	"2.5.2": "107",
	"2.6.0": "157",
	"2.6.1": "120",
	"2.6.2": "104",
	"2.6.3": "103",
	"2.6.4": "126",
	"2.7.0": "230",
}
