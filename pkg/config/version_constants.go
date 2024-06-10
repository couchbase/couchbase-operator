package config

import "k8s.io/apimachinery/pkg/version"

var (
	// Note: these should be updated every release.
	technicalLowerBound = &version.Info{Major: "1", Minor: "23", GitVersion: "v1.23.0"}
	supportedLowerBound = &version.Info{Major: "1", Minor: "25", GitVersion: "v1.25.0"}
	supportedUpperBound = &version.Info{Major: "1", Minor: "30", GitVersion: "v1.30.0"}
)
