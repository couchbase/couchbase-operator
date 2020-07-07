package framework

import (
	"context"
	"crypto/x509"
	"crypto/x509/pkix"
	"fmt"
	"time"

	"github.com/couchbase/couchbase-operator/pkg/config"
	"github.com/couchbase/couchbase-operator/pkg/util/retryutil"
	"github.com/couchbase/couchbase-operator/test/e2e/e2eutil"
	"github.com/couchbase/couchbase-operator/test/e2e/types"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
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
	client := k8s.KubeClient

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
	if _, err := client.CoreV1().ServiceAccounts(admissionNamespace).Create(serviceAccount); err != nil {
		return err
	}

	clusterRole := config.GetAdmissionClusterRole()
	if _, err := client.RbacV1().ClusterRoles().Create(clusterRole); err != nil {
		return err
	}

	clusterRoleBinding := config.GetAdmissionClusterRoleBinding(admissionNamespace)
	if _, err := client.RbacV1().ClusterRoleBindings().Create(clusterRoleBinding); err != nil {
		return err
	}

	secret := config.GetAdmissionSecret(admissionNamespace, key, cert)
	if _, err := client.CoreV1().Secrets(admissionNamespace).Create(secret); err != nil {
		return err
	}

	// This just picks the first, so ordering is important unless we sort out
	// cbopcfg...
	var pullSecret string

	if pullSecrets != nil && len(k8s.PullSecrets) > 0 {
		pullSecret = pullSecrets[0]
	}

	deployment := config.GetAdmissionDeployment(admissionNamespace, runtimeParams.AdmissionControllerImage, pullSecret, "-v", "1")

	if _, err := client.AppsV1().Deployments(admissionNamespace).Create(deployment); err != nil {
		return err
	}

	service := config.GetAdmissionService(admissionNamespace)
	if _, err := client.CoreV1().Services(admissionNamespace).Create(service); err != nil {
		return err
	}

	mutatingWebhook := config.GetAdmissionMutatingWebhook(admissionNamespace, ca.Certificate)
	if _, err := client.AdmissionregistrationV1beta1().MutatingWebhookConfigurations().Create(mutatingWebhook); err != nil {
		return err
	}

	validatingWebhook := config.GetAdmissionValidatingWebhook(admissionNamespace, ca.Certificate)
	if _, err := client.AdmissionregistrationV1beta1().ValidatingWebhookConfigurations().Create(validatingWebhook); err != nil {
		return err
	}

	if err := waitAdmissionController(client); err != nil {
		return err
	}

	return nil
}

// deleteAdmissionController removes any existing admission controller resources.  Just does
// this unconditionally rather than having to check whether it exists or not which is a lot
// of boiler plate zzz.
func deleteAdmissionController(client kubernetes.Interface) error {
	if err := client.CoreV1().ServiceAccounts(admissionNamespace).Delete(config.AdmissionResourceName, nil); err != nil && !errors.IsNotFound(err) {
		return err
	}

	if err := client.RbacV1().ClusterRoles().Delete(config.AdmissionResourceName, nil); err != nil && !errors.IsNotFound(err) {
		return err
	}

	if err := client.RbacV1().ClusterRoleBindings().Delete(config.AdmissionResourceName, nil); err != nil && !errors.IsNotFound(err) {
		return err
	}

	if err := client.CoreV1().Secrets(admissionNamespace).Delete(config.AdmissionResourceName, nil); err != nil && !errors.IsNotFound(err) {
		return err
	}

	if err := client.AppsV1().Deployments(admissionNamespace).Delete(config.AdmissionResourceName, nil); err != nil && !errors.IsNotFound(err) {
		return err
	}

	if err := client.CoreV1().Services(admissionNamespace).Delete(config.AdmissionResourceName, nil); err != nil && !errors.IsNotFound(err) {
		return err
	}

	if err := client.AdmissionregistrationV1beta1().MutatingWebhookConfigurations().Delete(config.AdmissionResourceName, nil); err != nil && !errors.IsNotFound(err) {
		return err
	}

	if err := client.AdmissionregistrationV1beta1().ValidatingWebhookConfigurations().Delete(config.AdmissionResourceName, nil); err != nil && !errors.IsNotFound(err) {
		return err
	}

	return nil
}

// waitAdmissionController polls the Kubernetes API for the admission controller deployment
// and waits for it to become ready.
func waitAdmissionController(client kubernetes.Interface) error {
	callback := func() (bool, error) {
		deployment, err := client.AppsV1().Deployments(admissionNamespace).Get(config.AdmissionResourceName, metav1.GetOptions{})
		if err != nil {
			return false, retryutil.RetryOkError(err)
		}

		if deployment.Status.ReadyReplicas != deployment.Status.Replicas {
			return false, retryutil.RetryOkError(fmt.Errorf("requested %d replicas, ready replicas %d", deployment.Status.Replicas, deployment.Status.ReadyReplicas))
		}

		return true, nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	return retryutil.Retry(ctx, time.Second, callback)
}
