package framework

import (
	"context"
	"crypto/x509"
	"crypto/x509/pkix"
	"fmt"
	"time"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/config"
	"github.com/couchbase/couchbase-operator/pkg/util/retryutil"
	"github.com/couchbase/couchbase-operator/test/e2e/e2eutil"
	"github.com/couchbase/couchbase-operator/test/e2e/types"

	appsv1 "k8s.io/api/apps/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
)

const (
	// admissionNamespace must be fixed, this prevents the case where we use
	// the same physical cluster for testing two different operators in different
	// namespaces and we end up with two instances.
	admissionNamespace = "default"
)

// createAdmissionController creates all the necessary resources to deploy the
// admission controller.
func createAdmissionController(k8s *types.Cluster, pullSecrets []string) error {
	validFrom := time.Now()
	validTo := time.Now().Add(24 * 365 * 10 * time.Hour)
	ca, _ := e2eutil.NewCertificateAuthority(e2eutil.KeyTypeRSA, config.AdmissionResourceName+" CA", validFrom, validTo, e2eutil.CertTypeCA)
	req := e2eutil.CreateKeyPairReqData(e2eutil.KeyTypeRSA, e2eutil.CertTypeServer, &x509.CertificateRequest{
		Subject: pkix.Name{
			CommonName: config.AdmissionResourceName,
		},
		DNSNames: []string{
			config.AdmissionResourceName + "." + admissionNamespace + ".svc",
		},
	})
	key, cert, _ := req.Generate(ca, validFrom, validTo)

	serviceAccount := config.GetAdmissionServiceAccount(admissionNamespace)
	if _, err := k8s.KubeClient.CoreV1().ServiceAccounts(admissionNamespace).Create(serviceAccount); err != nil {
		return err
	}

	roleObject := config.GetAdmissionRole(admissionNamespace, true)

	clusterRole, ok := roleObject.(*rbacv1.ClusterRole)
	if !ok {
		return fmt.Errorf("DAC role is not cluster scoped")
	}

	if _, err := k8s.KubeClient.RbacV1().ClusterRoles().Create(clusterRole); err != nil {
		return err
	}

	roleBindingObject := config.GetAdmissionRoleBinding(admissionNamespace, true)

	clusterRoleBinding, ok := roleBindingObject.(*rbacv1.ClusterRoleBinding)
	if !ok {
		return fmt.Errorf("DAC role binding is not cluster scoped")
	}

	if _, err := k8s.KubeClient.RbacV1().ClusterRoleBindings().Create(clusterRoleBinding); err != nil {
		return err
	}

	secret := config.GetAdmissionSecret(admissionNamespace, key, cert)
	if _, err := k8s.KubeClient.CoreV1().Secrets(admissionNamespace).Create(secret); err != nil {
		return err
	}

	deployment := config.GetAdmissionDeployment(admissionNamespace, runtimeParams.AdmissionControllerImage, pullSecrets, "-v", "1")

	if _, err := k8s.KubeClient.AppsV1().Deployments(admissionNamespace).Create(deployment); err != nil {
		return err
	}

	service := config.GetAdmissionService(admissionNamespace)
	if _, err := k8s.KubeClient.CoreV1().Services(admissionNamespace).Create(service); err != nil {
		return err
	}

	mutatingWebhook := config.GetAdmissionMutatingWebhook(admissionNamespace, ca.Certificate, true, nil)
	if _, err := k8s.KubeClient.AdmissionregistrationV1beta1().MutatingWebhookConfigurations().Create(mutatingWebhook); err != nil {
		return err
	}

	validatingWebhook := config.GetAdmissionValidatingWebhook(admissionNamespace, ca.Certificate, true, nil)
	if _, err := k8s.KubeClient.AdmissionregistrationV1beta1().ValidatingWebhookConfigurations().Create(validatingWebhook); err != nil {
		return err
	}

	if err := waitAdmissionController(k8s); err != nil {
		return err
	}

	return nil
}

// deleteAdmissionController removes any existing admission controller resources.  Just does
// this unconditionally rather than having to check whether it exists or not which is a lot
// of boiler plate zzz.
func deleteAdmissionController(k8s *types.Cluster) error {
	if err := k8s.KubeClient.CoreV1().ServiceAccounts(admissionNamespace).Delete(config.AdmissionResourceName, nil); err != nil && !errors.IsNotFound(err) {
		return err
	}

	if err := k8s.KubeClient.RbacV1().ClusterRoles().Delete(config.AdmissionResourceName, nil); err != nil && !errors.IsNotFound(err) {
		return err
	}

	if err := k8s.KubeClient.RbacV1().ClusterRoleBindings().Delete(config.AdmissionResourceName, nil); err != nil && !errors.IsNotFound(err) {
		return err
	}

	if err := k8s.KubeClient.CoreV1().Secrets(admissionNamespace).Delete(config.AdmissionResourceName, nil); err != nil && !errors.IsNotFound(err) {
		return err
	}

	if err := k8s.KubeClient.AppsV1().Deployments(admissionNamespace).Delete(config.AdmissionResourceName, nil); err != nil && !errors.IsNotFound(err) {
		return err
	}

	if err := k8s.KubeClient.CoreV1().Services(admissionNamespace).Delete(config.AdmissionResourceName, nil); err != nil && !errors.IsNotFound(err) {
		return err
	}

	if err := k8s.KubeClient.AdmissionregistrationV1beta1().MutatingWebhookConfigurations().Delete(config.AdmissionResourceName, nil); err != nil && !errors.IsNotFound(err) {
		return err
	}

	if err := k8s.KubeClient.AdmissionregistrationV1beta1().ValidatingWebhookConfigurations().Delete(config.AdmissionResourceName, nil); err != nil && !errors.IsNotFound(err) {
		return err
	}

	return nil
}

// waitAdmissionController polls the Kubernetes API for the admission controller deployment
// and waits for it to become ready.
func waitAdmissionController(k8s *types.Cluster) error {
	var deployment *appsv1.Deployment

	callback := func() error {
		var err error

		deployment, err = k8s.KubeClient.AppsV1().Deployments(admissionNamespace).Get(config.AdmissionResourceName, metav1.GetOptions{})
		if err != nil {
			return err
		}

		if deployment.Status.ReadyReplicas != deployment.Status.Replicas {
			return fmt.Errorf("requested %d replicas, ready replicas %d", deployment.Status.Replicas, deployment.Status.ReadyReplicas)
		}

		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	if err := retryutil.RetryOnErr(ctx, time.Second, callback); err != nil {
		return err
	}

	// Retry an operation that requires the DAC so we know for sure it's up and running.
	callback = func() error {
		bucket := &couchbasev2.CouchbaseBucket{
			ObjectMeta: metav1.ObjectMeta{
				Name: "dry-run",
			},
		}

		// TODO: Upgrade to 1.18 and this will be available by default.
		newBucket := &couchbasev2.CouchbaseBucket{}
		createOptions := &metav1.CreateOptions{
			DryRun: []string{
				"All",
			},
		}

		err := k8s.CRClient.CouchbaseV2().RESTClient().
			Post().
			Namespace(admissionNamespace).
			Resource("couchbasebuckets").
			VersionedParams(createOptions, scheme.ParameterCodec).
			Body(bucket).
			Do().
			Into(newBucket)

		if err != nil {
			return err
		}

		if newBucket.Spec.MemoryQuota == nil {
			return fmt.Errorf("admission defaulting not functional: %v", newBucket)
		}

		return nil
	}

	if err := retryutil.RetryOnErr(ctx, time.Second, callback); err != nil {
		return err
	}

	return nil
}
