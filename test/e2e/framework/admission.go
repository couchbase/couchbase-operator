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

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// createAdmissionController creates all the necessary resources to deploy the
// admission controller.  These are:
//
// * Service account
// * Role
// * Role binding
// * Deployment
// * Service
// * Mutating webhook
// * Validating webhook
func createAdmissionController(client kubernetes.Interface) error {
	validFrom := time.Now()
	validTo := time.Now().Add(24 * 365 * 10 * time.Hour)
	ca, _ := e2eutil.NewCertificateAuthority(e2eutil.KeyTypeRSA, config.AdmissionResourceName+" CA", validFrom, validTo, e2eutil.CertTypeCA)
	req := e2eutil.CreateKeyPairReqData(e2eutil.KeyTypeRSA, e2eutil.CertTypeServer, &x509.CertificateRequest{
		Subject: pkix.Name{
			CommonName: config.AdmissionResourceName,
		},
		DNSNames: []string{
			config.AdmissionResourceName + "." + Global.Namespace + ".svc",
		},
	})
	key, cert, _ := req.Generate(ca, validFrom, validTo)

	serviceAccount := config.GetAdmissionServiceAccount()
	if _, err := client.CoreV1().ServiceAccounts(Global.Namespace).Create(serviceAccount); err != nil {
		return err
	}
	clusterRole := config.GetAdmissionClusterRole()
	if _, err := client.RbacV1().ClusterRoles().Create(clusterRole); err != nil {
		return err
	}
	clusterRoleBinding := config.GetAdmissionClusterRoleBinding(Global.Namespace)
	if _, err := client.RbacV1().ClusterRoleBindings().Create(clusterRoleBinding); err != nil {
		return err
	}
	secret := config.GetAdmissionSecret(key, cert)
	if _, err := client.CoreV1().Secrets(Global.Namespace).Create(secret); err != nil {
		return err
	}
	deployment := config.GetAdmissionDeployment(runtimeParams.AdmissionControllerImage, dockerPullSecretName, "-v", "1")
	if _, err := client.AppsV1().Deployments(Global.Namespace).Create(deployment); err != nil {
		return err
	}
	service := config.GetAdmissionService()
	if _, err := client.CoreV1().Services(Global.Namespace).Create(service); err != nil {
		return err
	}
	mutatingWebhook := config.GetAdmissionMutatingWebhook(Global.Namespace, ca.Certificate)
	if _, err := client.AdmissionregistrationV1beta1().MutatingWebhookConfigurations().Create(mutatingWebhook); err != nil {
		return err
	}
	validatingWebhook := config.GetAdmissionValidatingWebhook(Global.Namespace, ca.Certificate)
	if _, err := client.AdmissionregistrationV1beta1().ValidatingWebhookConfigurations().Create(validatingWebhook); err != nil {
		return err
	}

	return nil
}

// deleteAdmissionController removes any existing admission controller resources.  Just does
// this unconditionally rather than having to check whether it exists or not which is a lot
// of boiler plate zzz.
func deleteAdmissionController(client kubernetes.Interface) error {
	if err := client.CoreV1().ServiceAccounts(Global.Namespace).Delete(config.AdmissionResourceName, nil); err != nil && !errors.IsNotFound(err) {
		return err
	}
	if err := client.RbacV1().ClusterRoles().Delete(config.AdmissionResourceName, nil); err != nil && !errors.IsNotFound(err) {
		return err
	}
	if err := client.RbacV1().ClusterRoleBindings().Delete(config.AdmissionResourceName, nil); err != nil && !errors.IsNotFound(err) {
		return err
	}
	if err := client.CoreV1().Secrets(Global.Namespace).Delete(config.AdmissionResourceName, nil); err != nil && !errors.IsNotFound(err) {
		return err
	}
	if err := client.AppsV1().Deployments(Global.Namespace).Delete(config.AdmissionResourceName, nil); err != nil && !errors.IsNotFound(err) {
		return err
	}
	if err := client.CoreV1().Services(Global.Namespace).Delete(config.AdmissionResourceName, nil); err != nil && !errors.IsNotFound(err) {
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
		deployment, err := client.AppsV1().Deployments(Global.Namespace).Get(config.AdmissionResourceName, metav1.GetOptions{})
		if err != nil {
			return false, retryutil.RetryOkError(err)
		}
		if deployment.Status.ReadyReplicas != deployment.Status.Replicas {
			return false, retryutil.RetryOkError(fmt.Errorf("requested %d replicas, ready replicas %d", deployment.Status.Replicas, deployment.Status.ReadyReplicas))
		}
		return true, nil
	}
	return retryutil.Retry(context.Background(), time.Second, 30, callback)
}
