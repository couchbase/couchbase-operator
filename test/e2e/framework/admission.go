package framework

import (
	"context"
	"fmt"
	"time"

	couchbasev1 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v1"
	"github.com/couchbase/couchbase-operator/pkg/util/retryutil"

	admissionregistrationv1beta1 "k8s.io/api/admissionregistration/v1beta1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

const (
	// admissionControllerName is used to identify all admission related resources.
	// We keep the same name as used by development so we ensure only one is used
	// at once.
	admissionControllerName = "couchbase-operator-admission"
	// admissionControllerCA is used by the Kubernetes API to verfiy the admission
	// controller's certificate.
	admissionControllerCA = `-----BEGIN CERTIFICATE-----
MIIDcTCCAlmgAwIBAgIJAIpNmW0VNfieMA0GCSqGSIb3DQEBCwUAMCoxKDAmBgNV
BAMMH0NvdWNoYmFzZSBPcGVyYXRvciBBZG1pc3Npb24gQ0EwHhcNMTgwNzI2MTAw
NTQxWhcNMjgwNzIzMTAwNTQxWjAqMSgwJgYDVQQDDB9Db3VjaGJhc2UgT3BlcmF0
b3IgQWRtaXNzaW9uIENBMIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8AMIIBCgKCAQEA
xsY1shNc8YihMKKqJddnLr5UpiIgBzhn/46x/9VOjl+ycRPDzTF6Hyt2ivFMlW9B
vjyGQYJ1w2y9vUq962IeM4U56GZsHsEQfyh0O59aMKSj9pwn2gTJcrGFXOd2XFym
Cs9a4lpXm7eHDhxvat473LuoP66zlQZeMiIxgj7JXmAp9K8BVZZV8zvD1Rqha4b4
VuttGLAjitCB03kt3CU23shklcCK2YzYjGjLE/s1fa37G0ZY4fbGyCapTDEvCcel
JDhvwXloUjukj/Rsfyswn+kHN6mpd4iKCoHn5JHSCBV9u/FlDSOus+gR2tGq58El
hnwMY9p5TGVX6odMIOLhsQIDAQABo4GZMIGWMB0GA1UdDgQWBBTCGk+OuecuOfGw
R5+iCS28519lpTBaBgNVHSMEUzBRgBTCGk+OuecuOfGwR5+iCS28519lpaEupCww
KjEoMCYGA1UEAwwfQ291Y2hiYXNlIE9wZXJhdG9yIEFkbWlzc2lvbiBDQYIJAIpN
mW0VNfieMAwGA1UdEwQFMAMBAf8wCwYDVR0PBAQDAgEGMA0GCSqGSIb3DQEBCwUA
A4IBAQAU+sxGj1saLgbhXDAiWrxqp2IgyhulNPD8TCWLkSRnndgImq+BSWpjAEUc
0atlJg1Tpaj3WRvTNptUAmAHiMRUWPnCRzwCi1VD6s3UsggBAMdMwJeLbCUnG4Vo
Wg2Sb+QpX9n47d4YOb4BuEdPtL0/2WH+nlU6vsyZLNbR2w/1hzD9A+J98ZbKLpi2
xeb7G7UXXrarCx3Rlh1ET+g6lDiMFMu+JFBNMQVhJt2oFuW3ZL405tCGkMmKdjq+
COSWCBbrLBrx93euWb2jvMkj5IqIR1EWaESVePCCZtEfx8E2zadqp8aCYv//chNU
xG9tHYlxNMXEjkq3mAXT2eN0uPiL
-----END CERTIFICATE-----`
	// admissionControllerCert is presented by the admission controller to the
	// Kubernetes API.  It contains a valid DNS SAN for the admission controller
	// service e.g. ${admissionControllerName}.${namespace}.svc.
	admissionControllerCert = `-----BEGIN CERTIFICATE-----
MIIDyTCCArGgAwIBAgIRAI9ogA+tCn8YsvQwgvHoX7swDQYJKoZIhvcNAQELBQAw
KjEoMCYGA1UEAwwfQ291Y2hiYXNlIE9wZXJhdG9yIEFkbWlzc2lvbiBDQTAeFw0x
ODA3MjYxMTMzNTRaFw0yODA3MjMxMTMzNTRaMDMxMTAvBgNVBAMMKGNvdWNoYmFz
ZS1vcGVyYXRvci1hZG1pc3Npb24uZGVmYXVsdC5zdmMwggEiMA0GCSqGSIb3DQEB
AQUAA4IBDwAwggEKAoIBAQDKKjr5vVJ8L03xPpSR8L5WWybp+orgiS8WTRGWSwKw
g/PPKZkQTlPwYylvqdrFJDZOpU0L3Be5WFZlIsFgAJZ6Jfzmh2lFiIREDDhFkiUi
hwF1Irq+vCG/zAs7iFh2OucS3xy9YfgvhYP9k3tORDJzT8/Bj1+3BFzFcSdf/Dsw
ibusk2Z+KnFWT6rpDayQhOyhPicwVIqbufz95p5WAUU/XoeU/tlD1Bclbnpu2xvO
pFNbVLvyvcZldF6CnwnPAVNaFydoBwWPrzorYRufNT/VEwq2J0a0Z5XMut5aWJsx
Kd/FzKZI/7RXWKRSRSVMwhGraDAQFcGQVVRCNKek/z1hAgMBAAGjgeAwgd0wCQYD
VR0TBAIwADAdBgNVHQ4EFgQUG4nZfbXm092iXbRnOFfzno/qgqgwWgYDVR0jBFMw
UYAUwhpPjrnnLjnxsEefogktvOdfZaWhLqQsMCoxKDAmBgNVBAMMH0NvdWNoYmFz
ZSBPcGVyYXRvciBBZG1pc3Npb24gQ0GCCQCKTZltFTX4njATBgNVHSUEDDAKBggr
BgEFBQcDATALBgNVHQ8EBAMCBaAwMwYDVR0RBCwwKoIoY291Y2hiYXNlLW9wZXJh
dG9yLWFkbWlzc2lvbi5kZWZhdWx0LnN2YzANBgkqhkiG9w0BAQsFAAOCAQEAev60
RtnmZ99jdnsiFsK135LD24Xvtg2+2p7HLBOGOuGPSQDMHy8h3yZjyFP09+gkEhui
tZdcRbe/fm08e6B5XIH2motNB+Cob0BQGwFPUW7dWeuDmnepGXNGLyrpGQuBW+5y
YNJgR7+Psvf3dutIzdrp7tqOeOq90lNdtpipb8XmmX7vxHY7E4LGmlzEGyOy1rog
HLYvnZh2hNPWt9rYseekKsF0wMlbWYqKxmCcjVfWTcDRknuOMuBM+UwwZdhDbdZe
Ld645dP/71NEZJ+sWdinTCsg5vv3/GRkhukBUEXAktYMCW49AZuYlloqZhAV8HcK
YGMrqXWinfpcSYJwlg==
-----END CERTIFICATE-----`
	// admissionControllerKey is the private key associated with the admission
	// controller public certificate.
	admissionControllerKey = `-----BEGIN PRIVATE KEY-----
MIIEwAIBADANBgkqhkiG9w0BAQEFAASCBKowggSmAgEAAoIBAQDKKjr5vVJ8L03x
PpSR8L5WWybp+orgiS8WTRGWSwKwg/PPKZkQTlPwYylvqdrFJDZOpU0L3Be5WFZl
IsFgAJZ6Jfzmh2lFiIREDDhFkiUihwF1Irq+vCG/zAs7iFh2OucS3xy9YfgvhYP9
k3tORDJzT8/Bj1+3BFzFcSdf/Dswibusk2Z+KnFWT6rpDayQhOyhPicwVIqbufz9
5p5WAUU/XoeU/tlD1Bclbnpu2xvOpFNbVLvyvcZldF6CnwnPAVNaFydoBwWPrzor
YRufNT/VEwq2J0a0Z5XMut5aWJsxKd/FzKZI/7RXWKRSRSVMwhGraDAQFcGQVVRC
NKek/z1hAgMBAAECggEBAJPLEbhXnsCouHtf+69BZ3SsSKOPBQ4nXCQajXvpNHsk
zA2r5HlWOekoJTe73fJ3ibgvAkdkTHe0S9y97s6nP1rnAJ7raZtqtP8mS9EYiUtX
lUoz7H/Z+3ZCzgdkov80Co/ySgltYMok+pxbwC40jwlb1I81qIychNHW6ikytXbC
PBD5KjPjun0xaNss7fLDWx3GXNfVDkQxDMmFsjCrh8YGjQmYhHHnGLCBk+OP6GFI
6Ddb1AJuQf/mCeC4mU4mY3AhiUaS0t/NiuYflXWkVdWRF1H8Ve5L+HDg15DIeU7h
3tOIyldwb80anKo5PYrjo7GrexfVMFa3VxWLLOqUa1ECgYEA+xXj4F8ShdrO/dxR
cSBrcl4a5YWBomfXYiEXzOcGIKjV6uqYdTIZJavyXySzgukWJab2WWPU/PwIljXu
KTE6khMpTj0yG4IrfI4m/qcihlIOLgrJIyYc3Ra4go+3t4hVSBsCNUrtBzNUTB1q
hAQBY8DNMFgTPYy/vZX8g1M+gF0CgYEAzh83AvMwOsVtSloEBfbhkmhcjHzVyrek
d++hKWGdHZR/AoSHikbKhAEdviwZm+SquC22pIZF8aEpWIPR52HXx9+Jm2CjOZ74
rzdGgELijCXwrQM722sv9AsKP4ccb2pXHQQolagUfIUIMAVU7Wc+3LoWgPsU/z2F
1ndfjnbRMNUCgYEAmG+VxWZy7GkHOgBEQZYZJXoUgjwnk93PWXgV5wRrJ/DYzqJW
pPAhbEmUAEdb5KJ2G63d6i8948lvvSJI0SFeGckgTqvAfArvM9NpwTjfMQUoLrPF
oV1GMMPWiQ2P0BEpFXmwQYKXnMOA7iT9weBcp58p86vFIp0M26DviRtE2tECgYEA
sxIbUMzF0clDMZ0ScbwSLIfOH580fXEdybS9Zp4PSWuBDEbnGhJ2TkhJ9rWJag42
4tuUGUst6MYCjYu4CDTQqixh+EL0i1K46kAzV6rD9s3fUe/FSNLOTk5pENfotELG
e8bpG1tysNtCSbXYGoff7RMeCeAYVca1R6Vdtv8yriECgYEAyza28LgEKUnjAgs9
fVCAPewaheVKmZwS721ils7llP3hkhaXDvjbIGSe5Li7iOp2+iPVahEMZK7sOVhk
X1+GiZU0WON3DNOYgJA6viuH3iz9LqB9l3SpaO8YFIRhIp5aniYAsPgBVrGmy6+K
IpxpqvtgDIpnTxDqbCx6xJHTNwc=
-----END PRIVATE KEY-----`
)

var (
	// admissionControllerMutatePath is the path the Kubernetes API sends
	// mutation requests to.
	admissionControllerMutatePath = "/couchbaseclusters/mutate"
	// admissionControllerValidatePath is the path the Kubernetes API sends
	// validation requests to.
	admissionControllerValidatePath = "/couchbaseclusters/validate"
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
	serviceAccount := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name: admissionControllerName,
		},
	}
	if _, err := client.CoreV1().ServiceAccounts(Global.Namespace).Create(serviceAccount); err != nil {
		return err
	}

	clusterRole := &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{
			Name: admissionControllerName,
		},
		Rules: []rbacv1.PolicyRule{
			{
				APIGroups: []string{couchbasev1.GroupName},
				Resources: []string{couchbasev1.CRDResourcePlural},
				Verbs:     []string{"get"},
			},
			{
				APIGroups: []string{""},
				Resources: []string{"secrets"},
				Verbs:     []string{"get"},
			},
			{
				APIGroups: []string{"storage.k8s.io"},
				Resources: []string{"storageclasses"},
				Verbs:     []string{"get"},
			},
		},
	}
	if _, err := client.RbacV1().ClusterRoles().Create(clusterRole); err != nil {
		return err
	}

	clusterRoleBinding := &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: admissionControllerName,
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: "rbac.authorization.k8s.io",
			Kind:     "ClusterRole",
			Name:     admissionControllerName,
		},
		Subjects: []rbacv1.Subject{
			{
				Kind:      "ServiceAccount",
				Name:      admissionControllerName,
				Namespace: Global.Namespace,
			},
		},
	}
	if _, err := client.RbacV1().ClusterRoleBindings().Create(clusterRoleBinding); err != nil {
		return err
	}

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name: admissionControllerName,
		},
		Data: map[string][]byte{
			"tls-cert-file":        []byte(admissionControllerCert),
			"tls-private-key-file": []byte(admissionControllerKey),
		},
	}
	if _, err := client.CoreV1().Secrets(Global.Namespace).Create(secret); err != nil {
		return err
	}

	var deploymentReplicas int32 = 1
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name: admissionControllerName,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &deploymentReplicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app": admissionControllerName,
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app": admissionControllerName,
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:    admissionControllerName,
							Image:   runtimeParams.AdmissionControllerImage,
							Command: []string{"couchbase-operator-admission"},
							Args: []string{
								"--logtostderr",
								"--stderrthreshold", "0",
								"--tls-cert-file", "/var/run/secrets/couchbase.com/couchbase-operator-admission/tls-cert-file",
								"--tls-private-key-file", "/var/run/secrets/couchbase.com/couchbase-operator-admission/tls-private-key-file",
							},
							//ImagePullPolicy: "Always",
							Ports: []corev1.ContainerPort{
								{
									Name:          "https",
									ContainerPort: 443,
								},
							},
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      admissionControllerName,
									MountPath: "/var/run/secrets/couchbase.com/couchbase-operator-admission",
									ReadOnly:  true,
								},
							},
						},
					},
					Volumes: []corev1.Volume{
						{
							Name: admissionControllerName,
							VolumeSource: corev1.VolumeSource{
								Secret: &corev1.SecretVolumeSource{
									SecretName: admissionControllerName,
								},
							},
						},
					},
					ServiceAccountName: admissionControllerName,
				},
			},
		},
	}
	if runtimeParams.DockerServer != "" {
		deployment.Spec.Template.Spec.ImagePullSecrets = []corev1.LocalObjectReference{
			{Name: dockerPullSecretName},
		}
	}
	if _, err := client.AppsV1().Deployments(Global.Namespace).Create(deployment); err != nil {
		return err
	}

	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name: admissionControllerName,
		},
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{
				"app": admissionControllerName,
			},
			Ports: []corev1.ServicePort{
				{
					Protocol: corev1.ProtocolTCP,
					Port:     443,
				},
			},
		},
	}
	if _, err := client.CoreV1().Services(Global.Namespace).Create(service); err != nil {
		return err
	}

	mutatingWebhook := &admissionregistrationv1beta1.MutatingWebhookConfiguration{
		ObjectMeta: metav1.ObjectMeta{
			Name: admissionControllerName,
		},
		Webhooks: []admissionregistrationv1beta1.Webhook{
			{
				Name: admissionControllerName + "." + Global.Namespace + ".svc",
				Rules: []admissionregistrationv1beta1.RuleWithOperations{
					{
						Rule: admissionregistrationv1beta1.Rule{
							APIGroups:   []string{couchbasev1.GroupName},
							Resources:   []string{couchbasev1.CRDResourcePlural},
							APIVersions: []string{"v1"},
						},
						Operations: []admissionregistrationv1beta1.OperationType{
							admissionregistrationv1beta1.Create,
							admissionregistrationv1beta1.Update,
						},
					},
				},
				ClientConfig: admissionregistrationv1beta1.WebhookClientConfig{
					Service: &admissionregistrationv1beta1.ServiceReference{
						Namespace: Global.Namespace,
						Name:      admissionControllerName,
						Path:      &admissionControllerMutatePath,
					},
					CABundle: []byte(admissionControllerCA),
				},
			},
		},
	}
	if _, err := client.AdmissionregistrationV1beta1().MutatingWebhookConfigurations().Create(mutatingWebhook); err != nil {
		return err
	}

	validatingWebhook := &admissionregistrationv1beta1.ValidatingWebhookConfiguration{
		ObjectMeta: metav1.ObjectMeta{
			Name: admissionControllerName,
		},
		Webhooks: []admissionregistrationv1beta1.Webhook{
			{
				Name: admissionControllerName + "." + Global.Namespace + ".svc",
				Rules: []admissionregistrationv1beta1.RuleWithOperations{
					{
						Rule: admissionregistrationv1beta1.Rule{
							APIGroups:   []string{couchbasev1.GroupName},
							Resources:   []string{couchbasev1.CRDResourcePlural},
							APIVersions: []string{"v1"},
						},
						Operations: []admissionregistrationv1beta1.OperationType{
							admissionregistrationv1beta1.Create,
							admissionregistrationv1beta1.Update,
						},
					},
				},
				ClientConfig: admissionregistrationv1beta1.WebhookClientConfig{
					Service: &admissionregistrationv1beta1.ServiceReference{
						Namespace: Global.Namespace,
						Name:      admissionControllerName,
						Path:      &admissionControllerValidatePath,
					},
					CABundle: []byte(admissionControllerCA),
				},
			},
		},
	}
	if _, err := client.AdmissionregistrationV1beta1().ValidatingWebhookConfigurations().Create(validatingWebhook); err != nil {
		return err
	}

	return nil
}

// deleteAdmissionController removes any existing admission controller resources.  Just does
// this unconditionally rather than having to check whether it exists or not which is a lot
// of boiler plate zzz.
func deleteAdmissionController(client kubernetes.Interface) error {
	if err := client.CoreV1().ServiceAccounts(Global.Namespace).Delete(admissionControllerName, nil); err != nil && !errors.IsNotFound(err) {
		return err
	}
	if err := client.RbacV1().ClusterRoles().Delete(admissionControllerName, nil); err != nil && !errors.IsNotFound(err) {
		return err
	}
	if err := client.RbacV1().ClusterRoleBindings().Delete(admissionControllerName, nil); err != nil && !errors.IsNotFound(err) {
		return err
	}
	if err := client.CoreV1().Secrets(Global.Namespace).Delete(admissionControllerName, nil); err != nil && !errors.IsNotFound(err) {
		return err
	}
	if err := client.AppsV1().Deployments(Global.Namespace).Delete(admissionControllerName, nil); err != nil && !errors.IsNotFound(err) {
		return err
	}
	if err := client.CoreV1().Services(Global.Namespace).Delete(admissionControllerName, nil); err != nil && !errors.IsNotFound(err) {
		return err
	}
	if err := client.AdmissionregistrationV1beta1().MutatingWebhookConfigurations().Delete(admissionControllerName, nil); err != nil && !errors.IsNotFound(err) {
		return err
	}
	if err := client.AdmissionregistrationV1beta1().ValidatingWebhookConfigurations().Delete(admissionControllerName, nil); err != nil && !errors.IsNotFound(err) {
		return err
	}
	return nil
}

// waitAdmissionController polls the Kubernetes API for the admission controller deployment
// and waits for it to become ready.
func waitAdmissionController(client kubernetes.Interface) error {
	callback := func() (bool, error) {
		deployment, err := client.AppsV1().Deployments(Global.Namespace).Get(admissionControllerName, metav1.GetOptions{})
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
