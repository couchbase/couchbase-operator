package main

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"

	api "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v1beta1"
	"github.com/couchbase/couchbase-operator/pkg/util/decoder"

	"github.com/couchbase/cbflag"

	"k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	"k8s.io/client-go/kubernetes/scheme"
)

var (
	path string
)

var versionId string = "0.8"
var versionHash string = "unknown"

type CbopctlContext struct {
	version bool
}

func (c *CbopctlContext) Run() {
	if c.version {
		fmt.Printf("cbopctl version %s (%s)\n", versionId, versionHash)
	}
}

func main() {
	cbopctlCtx := &CbopctlContext{}
	createCtx := &CreateContext{}
	deleteCtx := &DeleteContext{}

	cmdline := &cbflag.CLI{
		Name:    "cbopctl",
		Desc:    "Couchbase Kuberentes Operator Utility",
		ManPath: "",
		ManPage: "",
		Run:     cbopctlCtx.Run,
		Commands: []*cbflag.Command{
			&cbflag.Command{
				Name:     "create",
				Desc:     "Create a new Couchbase Cluster",
				ManPage:  "",
				Run:      createCtx.Run,
				Commands: []*cbflag.Command{},
				Flags: []*cbflag.Flag{
					cbflag.StringFlag(
						/* Destination  */ &createCtx.filename,
						/* Default      */ "",
						/* Short Option */ "f",
						/* Long Option  */ "filename",
						/* Env Variable */ "",
						/* Usage        */ "Filename or the resource to create",
						/* Deprecated   */ []string{},
						/* Validator    */ nil,
						/* Required     */ true,
						/* Hidden       */ false,
					),
					cbflag.BoolFlag(
						/* Destination  */ &createCtx.dryRun,
						/* Default      */ false,
						/* Short Option */ "",
						/* Long Option  */ "dry-run",
						/* Env Variable */ "",
						/* Usage        */ "If true, only print the object that would be sent, without sending it",
						/* Deprecated   */ []string{},
						/* Hidden       */ false,
					),
					cbflag.StringFlag(
						/* Destination  */ &createCtx.kubeconfig,
						/* Default      */ filepath.Join(os.Getenv("HOME"), ".kube", "config"),
						/* Short Option */ "",
						/* Long Option  */ "kubeconfig",
						/* Env Variable */ "",
						/* Usage        */ "The path to your kubernetes configuration",
						/* Deprecated   */ []string{},
						/* Validator    */ nil,
						/* Required     */ false,
						/* Hidden       */ false,
					),
				},
			},
			&cbflag.Command{
				Name:     "delete",
				Desc:     "Delete a new Couchbase Cluster",
				ManPage:  "",
				Run:      deleteCtx.Run,
				Commands: []*cbflag.Command{},
				Flags: []*cbflag.Flag{
					cbflag.StringFlag(
						/* Destination  */ &deleteCtx.filename,
						/* Default      */ "",
						/* Short Option */ "f",
						/* Long Option  */ "filename",
						/* Env Variable */ "",
						/* Usage        */ "Filename or the resource to create",
						/* Deprecated   */ []string{},
						/* Validator    */ nil,
						/* Required     */ true,
						/* Hidden       */ false,
					),
					cbflag.StringFlag(
						/* Destination  */ &deleteCtx.kubeconfig,
						/* Default      */ filepath.Join(os.Getenv("HOME"), ".kube", "config"),
						/* Short Option */ "",
						/* Long Option  */ "kubeconfig",
						/* Env Variable */ "",
						/* Usage        */ "The path to your kubernetes configuration",
						/* Deprecated   */ []string{},
						/* Validator    */ nil,
						/* Required     */ false,
						/* Hidden       */ false,
					),
				},
			},
		},
		Flags: []*cbflag.Flag{
			cbflag.BoolFlag(
				/* Destination  */ &cbopctlCtx.version,
				/* Default      */ false,
				/* Short Option */ "",
				/* Long Option  */ "version",
				/* Env Variable */ "",
				/* Usage        */ "Prints version information",
				/* Deprecated   */ []string{},
				/* Hidden       */ false,
			),
		},
		Writer: os.Stdout,
	}

	cmdline.Parse(os.Args)
}

func decodeCouchbaseCluster(path string) (*api.CouchbaseCluster, error) {
	raw, err := ioutil.ReadFile(path)
	if err != nil {
		return nil, err
	}

	err = v1beta1.SchemeBuilder.AddToScheme(scheme.Scheme)
	if err != nil {
		return nil, err
	}

	err = api.AddToScheme(scheme.Scheme)
	if err != nil {
		return nil, err
	}

	return decoder.DecodeCouchbaseCluster(raw)
}
