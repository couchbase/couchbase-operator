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

	couchbaseclusterv1 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v1"
	couchbaseclusterclientv1 "github.com/couchbase/couchbase-operator/pkg/generated/clientset/versioned"
	"github.com/couchbase/couchbase-operator/pkg/revision"
	"github.com/couchbase/couchbase-operator/pkg/util/jsonpatch"
	"github.com/couchbase/couchbase-operator/pkg/validator"
	"github.com/couchbase/couchbase-operator/pkg/version"

	admissionv1beta1 "k8s.io/api/admission/v1beta1"
	admissionregistrationv1beta1 "k8s.io/api/admissionregistration/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

var (
	// scheme contains versioned resource types
	scheme = runtime.NewScheme()
	// codecs provides a way to decode raw json into a versioned resource
	codecs = serializer.NewCodecFactory(scheme)
)

// Global initialization routines
func init() {
	addToScheme(scheme)
}

// addToScheme registers types we need to be able to decode from the raw JSON
func addToScheme(scheme *runtime.Scheme) {
	// TODO: admissionv1beta1 surely?
	admissionregistrationv1beta1.AddToScheme(scheme)
	couchbaseclusterv1.AddToScheme(scheme)
}

// getClient returns a new Kubernetes client
func getClient() *kubernetes.Clientset {
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

// getCouchbaseClusterClient returns a new CouchbaseCluster client
func getCouchbaseClusterClient() *couchbaseclusterclientv1.Clientset {
	config, err := rest.InClusterConfig()
	if err != nil {
		glog.Fatal(err)
	}
	clientset, err := couchbaseclusterclientv1.NewForConfig(config)
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

// couchbaseClustersValidate validates a CouchbaseCluster object will work with the
// operator.  This is for things which cannot be acheived with JSON schema v3 only.
func couchbaseClustersValidate(ar admissionv1beta1.AdmissionReview) *admissionv1beta1.AdmissionResponse {
	glog.Info("validating couchbasecluster")

	// Check the resource is valid
	couchbaseClustersResource := metav1.GroupVersionResource{
		Group:    couchbaseclusterv1.GroupName,
		Version:  "v1",
		Resource: couchbaseclusterv1.CRDResourcePlural,
	}
	if ar.Request.Resource != couchbaseClustersResource {
		err := fmt.Errorf("expect resource %s to be %s", ar.Request.Resource, couchbaseClustersResource)
		glog.Error(err)
		return errorResponse(err)
	}

	// Decode the CouchbaseCluster object
	raw := ar.Request.Object.Raw
	couchbaseCluster := couchbaseclusterv1.CouchbaseCluster{}
	deserializer := codecs.UniversalDeserializer()
	if _, _, err := deserializer.Decode(raw, nil, &couchbaseCluster); err != nil {
		glog.Error(err)
		return errorResponse(err)
	}

	// Build the response object
	reviewResponse := admissionv1beta1.AdmissionResponse{
		Allowed: true,
	}

	// Check that the CouchbaseCluster is correctly configured with respect to an existing resource
	if ar.Request.Operation == admissionv1beta1.Update {
		existingCouchbaseCluser, err := getCouchbaseClusterClient().CouchbaseV1().CouchbaseClusters(couchbaseCluster.Namespace).Get(couchbaseCluster.Name, metav1.GetOptions{})
		if err != nil {
			glog.Error(err)
			return errorResponse(err)
		}
		// Note: the we cannot raise warnings as the Result field is only consulted if Allowed is false
		if err, _ := validator.CheckImmutableFields(existingCouchbaseCluser, &couchbaseCluster); err != nil {
			glog.Error(err)
			return errorResponse(err)
		}
	}

	v := validator.New(getClient())

	// Check that the CouchbaseCluster is correctly configured
	if err := v.CheckConstraints(&couchbaseCluster); err != nil {
		glog.Error(err)
		return errorResponse(err)
	}

	return &reviewResponse
}

// couchbaseClustersMutate mutates a CouchbaseCluster object before validation.  This allows
// us to set sensible default values for various properties.
func couchbaseClustersMutate(ar admissionv1beta1.AdmissionReview) *admissionv1beta1.AdmissionResponse {
	glog.Info("mutating couchbasecluster")

	// Check the resource is valid
	couchbaseClustersResource := metav1.GroupVersionResource{
		Group:    couchbaseclusterv1.GroupName,
		Version:  "v1",
		Resource: couchbaseclusterv1.CRDResourcePlural,
	}
	if ar.Request.Resource != couchbaseClustersResource {
		err := fmt.Errorf("expect resource %s to be %s", ar.Request.Resource, couchbaseClustersResource)
		glog.Error(err)
		return errorResponse(err)
	}

	// Decode the CouchbaseCluster object
	raw := ar.Request.Object.Raw
	couchbaseCluster := couchbaseclusterv1.CouchbaseCluster{}
	deserializer := codecs.UniversalDeserializer()
	if _, _, err := deserializer.Decode(raw, nil, &couchbaseCluster); err != nil {
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
	mutatedCouchbaseCluster := couchbaseCluster.DeepCopy()
	validator.ApplyDefaults(mutatedCouchbaseCluster)
	if !reflect.DeepEqual(couchbaseCluster.Spec, mutatedCouchbaseCluster.Spec) {
		patch := jsonpatch.PatchList{
			jsonpatch.Patch{
				Op:    jsonpatch.Replace,
				Path:  "/spec",
				Value: mutatedCouchbaseCluster.Spec,
			},
		}
		data, err := json.Marshal(patch)
		if err != nil {
			glog.Error(err)
			return errorResponse(err)
		}
		reviewResponse.Patch = []byte(data)
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

	http.HandleFunc("/", serveDefault)
	http.HandleFunc("/couchbaseclusters/validate", serveCouchbaseClustersValidate)
	http.HandleFunc("/couchbaseclusters/mutate", serveCouchbaseClustersMutate)

	server := &http.Server{
		Addr:      ":443",
		TLSConfig: configTLS(config),
	}
	server.ListenAndServeTLS("", "")
}
