/*
Copyright 2018-Present Couchbase, Inc.

Use of this software is governed by the Business Source License included in
the file licenses/BSL-Couchbase.txt.  As of the Change Date specified in that
file, in accordance with the Business Source License, use of this software will
be governed by the Apache License, Version 2.0, included in the file
licenses/APL2.txt.
*/

package admission

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"reflect"
	"time"

	"github.com/couchbase/couchbase-operator/pkg/apis"
	"github.com/couchbase/couchbase-operator/pkg/generated/clientset/versioned"
	"github.com/couchbase/couchbase-operator/pkg/logging"
	"github.com/couchbase/couchbase-operator/pkg/revision"
	"github.com/couchbase/couchbase-operator/pkg/validator"
	"github.com/couchbase/couchbase-operator/pkg/validator/types"
	"github.com/couchbase/couchbase-operator/pkg/version"

	admissionv1 "k8s.io/api/admission/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/client-go/kubernetes"
	clientscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

var log = logf.Log.WithName("main")

var (
	// scheme contains versioned resource types.
	scheme = runtime.NewScheme()
	// codecs provides a way to decode raw json into a versioned resource.
	codecs = serializer.NewCodecFactory(scheme)
)

// addToScheme registers types we need to be able to decode from the raw JSON.
func addToScheme(scheme *runtime.Scheme) error {
	if err := clientscheme.AddToScheme(scheme); err != nil {
		return err
	}

	if err := apis.AddToScheme(scheme); err != nil {
		return err
	}

	return nil
}

// getClient returns a new Kubernetes client.
func getClient() kubernetes.Interface {
	config, err := rest.InClusterConfig()
	if err != nil {
		log.Error(err, "Kubernetes configuration load failed")
		os.Exit(1)
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		log.Error(err, "Kubernetes client failed")
		os.Exit(1)
	}

	return clientset
}

// getCouchbaseClient returns a new Couchbase Kubernetes client.
func getCouchbaseClient() versioned.Interface {
	config, err := rest.InClusterConfig()
	if err != nil {
		log.Error(err, "Kubernetes configuration failed")
		os.Exit(1)
	}

	clientset, err := versioned.NewForConfig(config)
	if err != nil {
		log.Error(err, "Kubernetes couchbase client failed")
		os.Exit(1)
	}

	return clientset
}

// configTLS examines the configuration and creates a TLS configuration.
func configTLS(config *Config) *tls.Config {
	cert, err := tls.LoadX509KeyPair(config.CertFile, config.KeyFile)
	if err != nil {
		log.Error(err, "TLS load failed")
		os.Exit(1)
	}

	return &tls.Config{
		Certificates: []tls.Certificate{cert},
	}
}

// Config contains the server (the webhook) cert and key.
type Config struct {
	// Addr is the address to listen on.
	Addr string

	// CertFile is the certificate, and any intermediate CAs, to serve up.
	CertFile string

	// KeyFile is the private key for the certificate.
	KeyFile string

	// ValidateSecrets allows opt-in to read/validate secrets.
	ValidateSecrets bool

	// ValidateStorageClasses allows opt-in to read/validate secrets.
	ValidateStorageClasses bool

	// DefaultFileSystemGroup allows opt-in to fs group defaulting.
	DefaultFileSystemGroup bool

	// LogOptions allow everything about logging to be set.
	LogOptions logging.Options
}

// addFlags parses command line parameters and adds them to a Config object.
func (c *Config) AddFlags() {
	flag.StringVar(&c.Addr, "address", ":8443", ""+
		"Address the server listens on.")
	flag.StringVar(&c.CertFile, "tls-cert-file", c.CertFile, ""+
		"File containing the default x509 Certificate for HTTPS, including any intermediate certificates."+
		"after server cert).")
	flag.StringVar(&c.KeyFile, "tls-private-key-file", c.KeyFile, ""+
		"File containing the default x509 private key matching --tls-cert-file.")
	flag.BoolVar(&c.ValidateSecrets, "validate-secrets", true, ""+
		"Validate referenced secrets")
	flag.BoolVar(&c.ValidateStorageClasses, "validate-storage-classes", true, ""+
		"Validate referenced storage classes")
	flag.BoolVar(&c.DefaultFileSystemGroup, "default-file-system-group", true, ""+
		"Default file system group information")

	c.LogOptions.AddFlagSet(flag.CommandLine)
}

// errorResponse takes an error and creates an admission response.
func errorResponse(err error) *admissionv1.AdmissionResponse {
	return &admissionv1.AdmissionResponse{
		Allowed: false,
		Result: &metav1.Status{
			Message: err.Error(),
		},
	}
}

// decodeObject decodes a cluster from an admission review and returns a versioned
// structure.
func decodeObject(ar admissionv1.AdmissionReview, raw runtime.RawExtension) (runtime.Object, error) {
	gvk := schema.GroupVersionKind{
		Group:   ar.Request.Kind.Group,
		Version: ar.Request.Kind.Version,
		Kind:    ar.Request.Kind.Kind,
	}

	object, err := scheme.New(gvk)
	if err != nil {
		return nil, err
	}

	deserializer := codecs.UniversalDeserializer()

	object, _, err = deserializer.Decode(raw.Raw, nil, object)
	if err != nil {
		return nil, err
	}

	return object, nil
}

// couchbaseClustersValidate validates a CouchbaseCluster object will work with the
// operator.  This is for things which cannot be achieved with JSON schema v3 only.
func couchbaseClustersValidate(config *Config, ar admissionv1.AdmissionReview) *admissionv1.AdmissionResponse {
	fields := []interface{}{
		"operation", ar.Request.Operation,
		"kind", ar.Request.Kind,
		"namespace", ar.Request.Namespace,
		"name", ar.Request.Name,
	}

	if log.V(1).Enabled() {
		fields = append(fields, "resource", ar.Request.Object)
	}

	log.Info("Validating resource", fields...)

	// Decode the CouchbaseCluster object
	couchbaseCluster, err := decodeObject(ar, ar.Request.Object)
	if err != nil {
		log.Error(err, "Resource decode failed")
		return errorResponse(err)
	}

	// Build the response object
	reviewResponse := admissionv1.AdmissionResponse{
		Allowed: true,
	}

	// Check that the CouchbaseCluster is correctly configured with respect to an existing resource
	if ar.Request.Operation == admissionv1.Update {
		// Ignore errors here as we could be upgrading from v1 to v2.  In this scenario
		// all CRDs served by the API will appear as v2 regardless of what's actually
		// on disk.
		log.V(1).Info("Previous resource", "resource", ar.Request.OldObject)

		existingCouchbaseCluser, err := decodeObject(ar, ar.Request.OldObject)
		if err != nil {
			log.Error(err, "Resource decode failed")
		} else if err := validator.CheckImmutableFields(existingCouchbaseCluser, couchbaseCluster); err != nil {
			log.Error(err, "Rejecting resource")
			return errorResponse(err)
		}
	}

	options := &types.ValidatorOptions{
		ValidateSecrets:        config.ValidateSecrets,
		ValidateStorageClasses: config.ValidateStorageClasses,
		DefaultFileSystemGroup: config.DefaultFileSystemGroup,
	}

	// Check that the CouchbaseCluster is correctly configured
	if err := validator.CheckConstraints(validator.New(getClient(), getCouchbaseClient(), options), couchbaseCluster); err != nil {
		log.Error(err, "Rejecting resource")
		return errorResponse(err)
	}

	return &reviewResponse
}

// couchbaseClustersMutate mutates a CouchbaseCluster object before validation.  This allows
// us to set sensible default values for various properties.
func couchbaseClustersMutate(config *Config, ar admissionv1.AdmissionReview) *admissionv1.AdmissionResponse {
	fields := []interface{}{
		"operation", ar.Request.Operation,
		"kind", ar.Request.Kind,
		"namespace", ar.Request.Namespace,
		"name", ar.Request.Name,
	}

	if log.V(1).Enabled() {
		fields = append(fields, "resource", ar.Request.Object)
	}

	log.Info("Mutating resource", fields...)

	// Decode the object as an unstructured data type.  Defaulting happens before
	// schema validation, so we mustn't try decode until this occurs.
	object := &unstructured.Unstructured{}
	if err := json.Unmarshal(ar.Request.Object.Raw, object); err != nil {
		log.Error(err, "Resource decode failed")
		return errorResponse(err)
	}

	// Build the response object
	pt := admissionv1.PatchTypeJSONPatch
	reviewResponse := admissionv1.AdmissionResponse{
		Allowed: true,
	}

	options := &types.ValidatorOptions{
		ValidateSecrets:        config.ValidateSecrets,
		ValidateStorageClasses: config.ValidateStorageClasses,
		DefaultFileSystemGroup: config.DefaultFileSystemGroup,
	}

	patch := validator.ApplyDefaults(validator.New(getClient(), getCouchbaseClient(), options), object)
	if patch != nil {
		log.V(1).Info("Applying patch", "patch", patch)

		data, err := json.Marshal(patch)
		if err != nil {
			log.Error(err, "Patch encode failed")
			return errorResponse(err)
		}

		reviewResponse.PatchType = &pt
		reviewResponse.Patch = data
	}

	return &reviewResponse
}

// admitFunc defines a callback function which accepts an admission review and returns a response.
type admitFunc func(*Config, admissionv1.AdmissionReview) *admissionv1.AdmissionResponse

// serve is the top level handler for all admission requests.  It decodes an admission review
// from the raw JSON and dispatches it to a specific handler.  The handler returns a response
// which is then marshalled back to JSON and sent back to the client.
func serve(config *Config, w http.ResponseWriter, r *http.Request, admit admitFunc) {
	// Read the POST body content
	var body []byte

	if r.Body != nil {
		if data, err := ioutil.ReadAll(r.Body); err == nil {
			body = data
		}
	}

	// Ensure the content is JSON before docoding it
	contentType := r.Header.Get("Content-Type")
	if contentType != "application/json" {
		log.Error(fmt.Errorf("media error"), "content-type", contentType)
		w.WriteHeader(http.StatusUnsupportedMediaType)

		return
	}

	// Decode the admission review object and dispatch to the correct handler
	var response *admissionv1.AdmissionResponse

	ar := admissionv1.AdmissionReview{}

	deserializer := codecs.UniversalDeserializer()
	if _, _, err := deserializer.Decode(body, nil, &ar); err != nil {
		log.Error(err, "Admission review decode failed")
		response = errorResponse(err)
	} else {
		response = admit(config, ar)
	}

	// Create the admission review response
	response.UID = ar.Request.UID

	review := admissionv1.AdmissionReview{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "admission.k8s.io/v1",
			Kind:       "AdmissionReview",
		},
		Response: response,
	}

	// Marshal to JSON and write the response
	resp, err := json.Marshal(review)
	if err != nil {
		log.Error(err, "Admission review encode failed")
	}

	w.Header().Set("Content-Type", "application/json")

	if _, err := w.Write(resp); err != nil {
		log.Error(err, "Admission response reply failed")
	}
}

// serveDefault is the default handler which logs the request URI and returns
// a 404 back to the client.
func serveDefault(w http.ResponseWriter, r *http.Request) {
	log.Error(fmt.Errorf("unexpected request"), "Unexpected request", "path", r.URL.String())
	w.WriteHeader(http.StatusNotFound)
}

// serveReadiness reports that the server is running.
func serveReadiness(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
}

// serveCouchbaseClustersValidate handles CouchbaseCluster validation requests.
func serveCouchbaseClustersValidate(config *Config) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		serve(config, w, r, couchbaseClustersValidate)
	}
}

// serveCouchbaseClustersValidate handles CouchbaseCluster mutation requests.
func serveCouchbaseClustersMutate(config *Config) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		serve(config, w, r, couchbaseClustersMutate)
	}
}

// Server wraps up a HTTP server and gives it restart capabilities.
type Server struct {
	// server is the server instance, it is replaced each time the server
	// is restarted.
	server *http.Server

	// err is used to communicate the error condition asynchronously back from
	// the server instance.
	err chan error

	// config is the static configuration for the application.
	config *Config
}

// Start launches the server in its own routine as it's a blocking call.
func (s *Server) Start(tlsConfig *tls.Config) {
	s.server = &http.Server{
		Addr:      s.config.Addr,
		TLSConfig: tlsConfig,
	}

	s.err = make(chan error)

	go func() {
		s.err <- s.server.ListenAndServeTLS("", "")
	}()
}

// Restart restarts the server so it picks up new configuration.
func (s *Server) Restart(tlsConfig *tls.Config) {
	log.Info("configuration modified, restarting server")

	if err := s.server.Shutdown(context.TODO()); err != nil {
		log.Error(err, "Server shutdown failed")
	} else {
		// Wait for the old server to stop.  You do get an error
		// condition on shutdown, so just ignore the value.
		<-s.err

		s.Start(tlsConfig)
	}
}

// main initializes the system then starts a HTTPS server to process requests.
func Serve(config *Config) {
	logf.SetLogger(logging.New(&config.LogOptions))

	log.Info(version.Application+"-admission-controller", "version", version.WithBuildNumber(), "revision", revision.Revision())

	if err := addToScheme(scheme); err != nil {
		log.Error(err, "Kubernetes resource scheme update failed")
		return
	}

	http.HandleFunc("/", serveDefault)
	http.HandleFunc("/readyz", serveReadiness)
	http.HandleFunc("/couchbaseclusters/validate", serveCouchbaseClustersValidate(config))
	http.HandleFunc("/couchbaseclusters/mutate", serveCouchbaseClustersMutate(config))

	tlsConfig := configTLS(config)

	server := &Server{
		config: config,
	}
	server.Start(tlsConfig)

	for {
		// Periodically poll the TLS configuration...
		select {
		case err := <-server.err:
			// Something went wrong with the server, start it up again.
			log.Error(err, "Server failed unexpectedly")

			server.Start(tlsConfig)
		case <-time.After(time.Minute):
		}

		// ... check if the TLS has updated, if so, restart the server.
		// Given the config can be modified by other calls (caching etc.)
		// we only consider the certificate.
		newTLSConfig := configTLS(config)

		if reflect.DeepEqual(tlsConfig.Certificates, newTLSConfig.Certificates) {
			continue
		}

		tlsConfig = newTLSConfig

		server.Restart(tlsConfig)
	}
}
