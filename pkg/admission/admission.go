package admission

import (
	"crypto/tls"
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"net/http"
	"reflect"

	"github.com/golang/glog"

	couchbasev1 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v1"
	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/generated/clientset/versioned"
	"github.com/couchbase/couchbase-operator/pkg/revision"
	"github.com/couchbase/couchbase-operator/pkg/util/jsonpatch"
	"github.com/couchbase/couchbase-operator/pkg/validator"
	"github.com/couchbase/couchbase-operator/pkg/version"

	admissionv1beta1 "k8s.io/api/admission/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/client-go/kubernetes"
	clientscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
)

var (
	// scheme contains versioned resource types
	scheme = runtime.NewScheme()
	// codecs provides a way to decode raw json into a versioned resource
	codecs = serializer.NewCodecFactory(scheme)
)

// addToScheme registers types we need to be able to decode from the raw JSON
func addToScheme(scheme *runtime.Scheme) error {
	if err := clientscheme.AddToScheme(scheme); err != nil {
		return err
	}
	if err := couchbasev1.AddToScheme(scheme); err != nil {
		return err
	}
	return nil
}

// getClient returns a new Kubernetes client
func getClient() kubernetes.Interface {
	config, err := rest.InClusterConfig()
	if err != nil {
		glog.Fatal(err)
	}
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		glog.Fatal(err)
	}
	return clientset
}

// getCouchbaseClient returns a new Couchbase Kubernetes client.
func getCouchbaseClient() versioned.Interface {
	config, err := rest.InClusterConfig()
	if err != nil {
		glog.Fatal(err)
	}
	clientset, err := versioned.NewForConfig(config)
	if err != nil {
		glog.Fatal(err)
	}
	return clientset
}

// configTLS examines the configuration and creates a TLS configuration
func configTLS(config *Config) *tls.Config {
	cert, err := tls.LoadX509KeyPair(config.CertFile, config.KeyFile)
	if err != nil {
		glog.Fatal(err)
	}
	return &tls.Config{
		Certificates: []tls.Certificate{cert},
	}
}

// Config contains the server (the webhook) cert and key.
type Config struct {
	Addr     string
	CertFile string
	KeyFile  string
}

// addFlags parses command line parameters and adds them to a Config object
func (c *Config) AddFlags() {
	flag.StringVar(&c.Addr, "address", ":443", ""+
		"Address the server listens on (defaults to :443).")
	flag.StringVar(&c.CertFile, "tls-cert-file", c.CertFile, ""+
		"File containing the default x509 Certificate for HTTPS. (CA cert, if any, concatenated "+
		"after server cert).")
	flag.StringVar(&c.KeyFile, "tls-private-key-file", c.KeyFile, ""+
		"File containing the default x509 private key matching --tls-cert-file.")
}

// errorResponse takes an error and creates an admission response
func errorResponse(err error) *admissionv1beta1.AdmissionResponse {
	return &admissionv1beta1.AdmissionResponse{
		Allowed: false,
		Result: &metav1.Status{
			Message: err.Error(),
		},
	}
}

// decodeObject decodes a cluster from an admission review and returns a versioned
// structure.
func decodeObject(ar admissionv1beta1.AdmissionReview, raw runtime.RawExtension) (runtime.Object, error) {
	var object runtime.Object
	switch ar.Request.Kind.Kind {
	case couchbasev2.ClusterCRDResourceKind:
		switch ar.Request.Resource.Version {
		case "v1":
			object = &couchbasev1.CouchbaseCluster{}
		case "v2":
			object = &couchbasev2.CouchbaseCluster{}
		default:
			return nil, fmt.Errorf("unhandled resource version %s", ar.Request.Resource.Version)
		}
	case couchbasev2.BucketCRDResourceKind:
		object = &couchbasev2.CouchbaseBucket{}
	case couchbasev2.EphemeralBucketCRDResourceKind:
		object = &couchbasev2.CouchbaseEphemeralBucket{}
	case couchbasev2.MemcachedBucketCRDResourceKind:
		object = &couchbasev2.CouchbaseMemcachedBucket{}
	}

	deserializer := codecs.UniversalDeserializer()
	var err error
	object, _, err = deserializer.Decode(raw.Raw, nil, object)
	if err != nil {
		return nil, err
	}

	return object, nil
}

// getSpec returns the cluster specification from an abstract cluster type.
func getSpec(resource runtime.Object) (interface{}, error) {
	switch t := resource.(type) {
	case *couchbasev1.CouchbaseCluster:
		return t.Spec, nil
	case *couchbasev2.CouchbaseCluster:
		return t.Spec, nil
	case *couchbasev2.CouchbaseBucket:
		return t.Spec, nil
	case *couchbasev2.CouchbaseEphemeralBucket:
		return t.Spec, nil
	case *couchbasev2.CouchbaseMemcachedBucket:
		return t.Spec, nil
	default:
		return nil, fmt.Errorf("unhandled type %v", t)
	}
}

// deepCopy returns a clone of an abstract cluster type.
func deepCopy(resource runtime.Object) (runtime.Object, error) {
	switch t := resource.(type) {
	case *couchbasev1.CouchbaseCluster:
		return t.DeepCopy(), nil
	case *couchbasev2.CouchbaseCluster:
		return t.DeepCopy(), nil
	case *couchbasev2.CouchbaseBucket:
		return t.DeepCopy(), nil
	case *couchbasev2.CouchbaseEphemeralBucket:
		return t.DeepCopy(), nil
	case *couchbasev2.CouchbaseMemcachedBucket:
		return t.DeepCopy(), nil
	default:
		return nil, fmt.Errorf("unhandled type %v", t)
	}
}

// couchbaseClustersValidate validates a CouchbaseCluster object will work with the
// operator.  This is for things which cannot be achieved with JSON schema v3 only.
func couchbaseClustersValidate(ar admissionv1beta1.AdmissionReview) *admissionv1beta1.AdmissionResponse {
	if glog.V(1) {
		glog.Info("validating couchbasecluster")
	}

	// Temporary hack: explicitly validate the schema.
	if err := validator.SchemaValidate(scheme, ar.Request.Object); err != nil {
		glog.Error(err)
		return errorResponse(err)
	}

	// Decode the CouchbaseCluster object
	couchbaseCluster, err := decodeObject(ar, ar.Request.Object)
	if err != nil {
		glog.Error(err)
		return errorResponse(err)
	}

	// Build the response object
	reviewResponse := admissionv1beta1.AdmissionResponse{
		Allowed: true,
	}

	// Check that the CouchbaseCluster is correctly configured with respect to an existing resource
	if ar.Request.Operation == admissionv1beta1.Update {
		existingCouchbaseCluser, err := decodeObject(ar, ar.Request.OldObject)
		if err != nil {
			glog.Error(err)
			return errorResponse(err)
		}
		// Note: the we cannot raise warnings as the Result field is only consulted if Allowed is false
		if err := validator.CheckImmutableFields(existingCouchbaseCluser, couchbaseCluster); err != nil {
			glog.Error(err)
			return errorResponse(err)
		}
	}

	// Check that the CouchbaseCluster is correctly configured
	if err := validator.CheckConstraints(validator.New(getClient(), getCouchbaseClient()), couchbaseCluster); err != nil {
		glog.Error(err)
		return errorResponse(err)
	}

	return &reviewResponse
}

// couchbaseClustersMutate mutates a CouchbaseCluster object before validation.  This allows
// us to set sensible default values for various properties.
func couchbaseClustersMutate(ar admissionv1beta1.AdmissionReview) *admissionv1beta1.AdmissionResponse {
	if glog.V(1) {
		glog.Info("mutating couchbasecluster")
	}

	// Decode the CouchbaseCluster object
	couchbaseCluster, err := decodeObject(ar, ar.Request.Object)
	if err != nil {
		glog.Error(err)
		return errorResponse(err)
	}

	// Build the response object
	pt := admissionv1beta1.PatchTypeJSONPatch
	reviewResponse := admissionv1beta1.AdmissionResponse{
		Allowed:   true,
		PatchType: &pt,
	}

	// Duplicate the cluster and apply default values.  If the cluster has been
	// mutated send a patch pack to the server to be applied.
	mutatedCouchbaseCluster, err := deepCopy(couchbaseCluster)
	if err != nil {
		glog.Error(err)
		return errorResponse(err)
	}
	validator.ApplyDefaults(mutatedCouchbaseCluster)
	oldSpec, err := getSpec(couchbaseCluster)
	if err != nil {
		glog.Error(err)
		return errorResponse(err)
	}
	newSpec, err := getSpec(mutatedCouchbaseCluster)
	if err != nil {
		glog.Error(err)
		return errorResponse(err)
	}

	if !reflect.DeepEqual(oldSpec, newSpec) {
		patch := jsonpatch.PatchList{
			jsonpatch.Patch{
				Op:    jsonpatch.Replace,
				Path:  "/spec",
				Value: newSpec,
			},
		}
		data, err := json.Marshal(patch)
		if err != nil {
			glog.Error(err)
			return errorResponse(err)
		}
		reviewResponse.Patch = data
	}

	return &reviewResponse
}

// admitFunc defines a callback function which accepts an admission review and returns a response
type admitFunc func(admissionv1beta1.AdmissionReview) *admissionv1beta1.AdmissionResponse

// serve is the top level handler for all admission requests.  It decodes an admission review
// from the raw JSON and dispatches it to a specific handler.  The handler returns a response
// which is then marshalled back to JSON and sent back to the client.
func serve(w http.ResponseWriter, r *http.Request, admit admitFunc) {
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
		glog.Errorf("contentType=%s, expected application/json", contentType)
		w.WriteHeader(http.StatusUnsupportedMediaType)
		return
	}

	// Decode the admission review object and dispatch to the correct handler
	var response *admissionv1beta1.AdmissionResponse
	ar := admissionv1beta1.AdmissionReview{}
	deserializer := codecs.UniversalDeserializer()
	if _, _, err := deserializer.Decode(body, nil, &ar); err != nil {
		glog.Error(err)
		response = errorResponse(err)
	} else {
		response = admit(ar)
	}

	// Create the admission review response
	response.UID = ar.Request.UID
	review := admissionv1beta1.AdmissionReview{
		Response: response,
	}

	// Marshal to JSON and write the response
	resp, err := json.Marshal(review)
	if err != nil {
		glog.Error(err)
	}
	w.Header().Set("Content-Type", "application/json")
	if _, err := w.Write(resp); err != nil {
		glog.Error(err)
	}
}

// serveDefault is the default handler which logs the request URI and returns
// a 404 back to the client
func serveDefault(w http.ResponseWriter, r *http.Request) {
	glog.Errorf("Unexpected request %s", r.URL.String())
	w.WriteHeader(http.StatusNotFound)
}

// serveCouchbaseClustersValidate handles CouchbaseCluster validation requests
func serveCouchbaseClustersValidate(w http.ResponseWriter, r *http.Request) {
	serve(w, r, couchbaseClustersValidate)
}

// serveCouchbaseClustersValidate handles CouchbaseCluster mutation requests
func serveCouchbaseClustersMutate(w http.ResponseWriter, r *http.Request) {
	serve(w, r, couchbaseClustersMutate)
}

// main initializes the system then starts a HTTPS server to process requests
func Serve(config *Config) {
	glog.Infof("couchbase-operator-admission %s (%s)", version.Version, revision.Revision())

	if err := addToScheme(scheme); err != nil {
		glog.Error(err)
		return
	}

	http.HandleFunc("/", serveDefault)
	http.HandleFunc("/couchbaseclusters/validate", serveCouchbaseClustersValidate)
	http.HandleFunc("/couchbaseclusters/mutate", serveCouchbaseClustersMutate)

	server := &http.Server{
		Addr:      ":8443",
		TLSConfig: configTLS(config),
	}
	if err := server.ListenAndServeTLS("", ""); err != nil {
		glog.Error(err)
	}
}
