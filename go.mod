module github.com/couchbase/couchbase-operator

go 1.16

require (
	github.com/Masterminds/semver v1.5.0
	github.com/aws/aws-sdk-go v1.38.40
	github.com/couchbase/gocb/v2 v2.3.0
	github.com/evanphx/json-patch v4.12.0+incompatible
	github.com/ghodss/yaml v1.0.0
	github.com/go-logr/logr v1.2.0
	github.com/go-openapi/errors v0.20.0
	github.com/golang/glog v1.0.0
	github.com/golang/groupcache v0.0.0-20210331224755-41bb18bfe9da
	github.com/golangci/golangci-lint v1.42.1 // indirect
	github.com/google/go-cmp v0.5.6
	github.com/google/uuid v1.2.0
	github.com/gregjones/httpcache v0.0.0-20190611155906-901d90724c79 // indirect
	github.com/imdario/mergo v0.3.12
	github.com/prometheus/client_golang v1.11.0
	github.com/prometheus/common v0.28.0
	github.com/robfig/cron/v3 v3.0.1
	github.com/sirupsen/logrus v1.8.1
	github.com/spf13/cobra v1.2.1
	github.com/spf13/pflag v1.0.5
	golang.org/x/time v0.0.0-20210723032227-1f47c861a9ac
	k8s.io/api v0.23.2
	k8s.io/apiextensions-apiserver v0.23.2
	k8s.io/apimachinery v0.23.2
	k8s.io/cli-runtime v0.23.2
	k8s.io/client-go v0.23.2
	k8s.io/code-generator v0.23.2 // indirect
	k8s.io/klog/v2 v2.30.0
	k8s.io/kube-aggregator v0.23.2
	sigs.k8s.io/controller-runtime v0.11.0
	sigs.k8s.io/controller-tools v0.8.0 // indirect
)
