package config

import "k8s.io/apimachinery/pkg/version"

var (
	// Note: these should be updated every release.
	technicalLowerBound = &version.Info{Major: "1", Minor: "26", GitVersion: "v1.23.0"}
	supportedLowerBound = &version.Info{Major: "1", Minor: "26", GitVersion: "v1.23.0"}
	supportedUpperBound = &version.Info{Major: "1", Minor: "29", GitVersion: "v1.29.0"}
)
