package admission

import (
	"crypto/tls"
	"encoding/json"
	"flag"
	"io/ioutil"
	"net/http"

	"github.com/golang/glog"

	"github.com/couchbase/couchbase-operator/pkg/apis"
	"github.com/couchbase/couchbase-operator/pkg/generated/clientset/versioned"
	"github.com/couchbase/couchbase-operator/pkg/revision"
	"github.com/couchbase/couchbase-operator/pkg/validator"
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
)

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

// configTLS examines the configuration and creates a TLS configuration.
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

// addFlags parses command line parameters and adds them to a Config object.
func (c *Config) AddFlags() {
	flag.StringVar(&c.Addr, "address", ":443", ""+
		"Address the server listens on (defaults to :443).")
	flag.StringVar(&c.CertFile, "tls-cert-file", c.CertFile, ""+
		"File containing the default x509 Certificate for HTTPS. (CA cert, if any, concatenated "+
		"after server cert).")
	flag.StringVar(&c.KeyFile, "tls-private-key-file", c.KeyFile, ""+
		"File containing the default x509 private key matching --tls-cert-file.")
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
func couchbaseClustersValidate(ar admissionv1.AdmissionReview) *admissionv1.AdmissionResponse {
	glog.Infof("Validating resource: %v %v %v/%v",
		ar.Request.Operation,
		ar.Request.Kind,
		ar.Request.Namespace,
		ar.Request.Name)
	glog.V(1).Infof("Validating resource: %s", string(ar.Request.Object.Raw))

	// Decode the CouchbaseCluster object
	couchbaseCluster, err := decodeObject(ar, ar.Request.Object)
	if err != nil {
		glog.Error(err)
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
		glog.V(1).Infof("Previous resource: %s", string(ar.Request.OldObject.Raw))

		existingCouchbaseCluser, err := decodeObject(ar, ar.Request.OldObject)
		if err != nil {
			glog.Error(err)
		} else if err := validator.CheckImmutableFields(existingCouchbaseCluser, couchbaseCluster); err != nil {
			glog.Errorf("Rejecting resource: %v", err)
			return errorResponse(err)
		}
	}

	// Check that the CouchbaseCluster is correctly configured
	if err := validator.CheckConstraints(validator.New(getClient(), getCouchbaseClient()), couchbaseCluster); err != nil {
		glog.Errorf("Rejecting resource: %v", err)
		return errorResponse(err)
	}

	return &reviewResponse
}

// couchbaseClustersMutate mutates a CouchbaseCluster object before validation.  This allows
// us to set sensible default values for various properties.
func couchbaseClustersMutate(ar admissionv1.AdmissionReview) *admissionv1.AdmissionResponse {
	glog.Infof("Mutating resource: %v %v %v/%v",
		ar.Request.Operation,
		ar.Request.Kind,
		ar.Request.Namespace,
		ar.Request.Name)
	glog.V(1).Infof("Mutating resource: %s", string(ar.Request.Object.Raw))

	// Decode the object as an unstructured data type.  Defaulting happens before
	// schema validation, so we mustn't try decode until this occurs.
	object := &unstructured.Unstructured{}
	if err := json.Unmarshal(ar.Request.Object.Raw, object); err != nil {
		glog.Error(err)
		return errorResponse(err)
	}

	// Build the response object
	pt := admissionv1.PatchTypeJSONPatch
	reviewResponse := admissionv1.AdmissionResponse{
		Allowed: true,
	}

	patch := validator.ApplyDefaults(validator.New(getClient(), getCouchbaseClient()), object)
	if patch != nil {
		data, err := json.Marshal(patch)
		if err != nil {
			glog.Error(err)
			return errorResponse(err)
		}

		glog.V(1).Infof("Applying patch: %v", string(data))

		reviewResponse.PatchType = &pt
		reviewResponse.Patch = data
	}

	return &reviewResponse
}

// admitFunc defines a callback function which accepts an admission review and returns a response.
type admitFunc func(admissionv1.AdmissionReview) *admissionv1.AdmissionResponse

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
	var response *admissionv1.AdmissionResponse

	ar := admissionv1.AdmissionReview{}

	deserializer := codecs.UniversalDeserializer()
	if _, _, err := deserializer.Decode(body, nil, &ar); err != nil {
		glog.Error(err)
		response = errorResponse(err)
	} else {
		response = admit(ar)
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
		glog.Error(err)
	}

	w.Header().Set("Content-Type", "application/json")

	if _, err := w.Write(resp); err != nil {
		glog.Error(err)
	}
}

// serveDefault is the default handler which logs the request URI and returns
// a 404 back to the client.
func serveDefault(w http.ResponseWriter, r *http.Request) {
	glog.Errorf("Unexpected request %s", r.URL.String())
	w.WriteHeader(http.StatusNotFound)
}

// serveReadiness reports that the server is running.
func serveReadiness(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
}

// serveCouchbaseClustersValidate handles CouchbaseCluster validation requests.
func serveCouchbaseClustersValidate(w http.ResponseWriter, r *http.Request) {
	serve(w, r, couchbaseClustersValidate)
}

// serveCouchbaseClustersValidate handles CouchbaseCluster mutation requests.
func serveCouchbaseClustersMutate(w http.ResponseWriter, r *http.Request) {
	serve(w, r, couchbaseClustersMutate)
}

// main initializes the system then starts a HTTPS server to process requests.
func Serve(config *Config) {
	glog.Infof("couchbase-operator-admission %s (%s)", version.WithBuildNumber(), revision.Revision())

	if err := addToScheme(scheme); err != nil {
		glog.Error(err)
		return
	}

	http.HandleFunc("/", serveDefault)
	http.HandleFunc("/readyz", serveReadiness)
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
