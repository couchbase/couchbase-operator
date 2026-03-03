/*
Copyright 2019-Present Couchbase, Inc.

Use of this software is governed by the Business Source License included in
the file licenses/BSL-Couchbase.txt.  As of the Change Date specified in that
file, in accordance with the Business Source License, use of this software will
be governed by the Apache License, Version 2.0, included in the file
licenses/APL2.txt.
*/

package e2espec

import (
	"fmt"

	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/test/e2e/constants"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// LDAP Settings.
var (
	// pod.
	openLDAPImage     = "osixia/openldap:1.2.4"
	openLDAPInitImage = "busybox:1.28"
	commonName        = "admin"
	ldapHostName      = "ldap-test"
	ldapSubdomain     = constants.LDAPDomain

	// tls.
	ldapCrtFile   = "tls.crt"
	ldapKeyFile   = "tls.key"
	ldapCACrtFile = "ca.crt"

	// network.
	ldapPortName    = "ldap-port"
	ldapPort        = 389
	ldapSSLPortName = "ssl-ldap-port"
	ldapSSLPort     = 636

	// volume.
	ldapCerDirtMount = "certdir"
	ldapSecretMount  = "certsecret"
)

// LDAP Settings.
func newLDAPSettings(namespace string) *couchbasev2.CouchbaseClusterLDAPSpec {
	basedn := fmt.Sprintf("dc=%s,dc=%s,dc=svc", ldapSubdomain, namespace)
	binddn := fmt.Sprintf("cn=%s,%s", commonName, basedn)
	userDnTemplate := fmt.Sprintf("uid=%%u,ou=People,%s", basedn)
	dnMapping := couchbasev2.LDAPUserDNMapping{
		Template: userDnTemplate,
	}

	host := fmt.Sprintf("%s.%s.%s.svc", ldapHostName, ldapSubdomain, namespace)

	return &couchbasev2.CouchbaseClusterLDAPSpec{
		Hosts:                 []string{host},
		BindDN:                binddn,
		AuthenticationEnabled: true,
		AuthorizationEnabled:  false,
		Port:                  389,
		Encryption:            couchbasev2.LDAPEncryptionStartTLS,
		EnableCertValidation:  true,
		UserDNMapping:         dnMapping,
		NestedGroupsEnabled:   false,
		NestedGroupsMaxDepth:  10,
		CacheValueLifetime:    300000,
	}
}

// NewLDAPClusterBasic creates a new cluster with LDAP configured.
func NewLDAPClusterBasic(options *ClusterOptions, namespace string, secretCa string, secretPassword string) *couchbasev2.CouchbaseCluster {
	clusterSpec := NewBasicCluster(options)
	clusterSpec.Spec.Security.LDAP = newLDAPSettings(namespace)
	clusterSpec.Spec.Security.LDAP.BindSecret = secretPassword
	clusterSpec.Spec.Security.LDAP.TLSSecret = secretCa

	return clusterSpec
}

// ldapPodLabels are labels applied to LDAP Pod and as service selector.
func ldapPodLabels() map[string]string {
	return map[string]string{
		"app":   ldapSubdomain,
		"group": constants.LDAPLabelSelector,
	}
}

// LDAP Server Pod Spec.
func NewLDAPServer(namespace string) *v1.Pod {
	container := openLDAPContainer(namespace)

	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:   ldapHostName,
			Labels: ldapPodLabels(),
		},
		Spec: v1.PodSpec{
			Containers:    []v1.Container{container},
			RestartPolicy: v1.RestartPolicyNever,
			Hostname:      ldapHostName,
			Subdomain:     ldapSubdomain,
		},
	}

	return pod
}

// LDAP Server Pod Spec with TLS.
func NewLDAPServerTLS(namespace, ldapSecret string) *v1.Pod {
	pod := NewLDAPServer(namespace)

	// add init container to copy certs
	initContainer := openLDAPInitContainer()
	pod.Spec.InitContainers = []v1.Container{initContainer}

	// Mount certs from secret to sldap path
	pod.Spec.Volumes = []v1.Volume{
		{
			Name: ldapCerDirtMount,
			VolumeSource: v1.VolumeSource{
				EmptyDir: &v1.EmptyDirVolumeSource{},
			},
		},
		{
			Name: ldapSecretMount,
			VolumeSource: v1.VolumeSource{
				Secret: &v1.SecretVolumeSource{
					SecretName: ldapSecret,
				},
			},
		},
	}

	return pod
}

// OpenLDAP container spec.
func openLDAPContainer(namespace string) v1.Container {
	ldapDomain := fmt.Sprintf("%s.%s.svc", ldapSubdomain, namespace)
	ldapFQDN := fmt.Sprintf("%s.%s", ldapHostName, ldapDomain)

	return v1.Container{
		Name:  ldapHostName,
		Image: openLDAPImage,
		Env: []v1.EnvVar{
			{
				Name:  "HOSTNAME",
				Value: ldapFQDN,
			},
			{
				Name:  "LDAP_DOMAIN",
				Value: ldapDomain,
			},
			{
				Name:  "LDAP_TLS",
				Value: "true",
			},
			{
				Name:  "LDAP_ADMIN_PASSWORD",
				Value: "password",
			},
			{
				Name:  "LDAP_ORGANISATION",
				Value: "Couchbase",
			},
			{
				Name:  "LDAP_REMOVE_CONFIG_AFTER_SETUP",
				Value: "true",
			},
			{
				Name:  "LDAP_TLS_ENFORCE",
				Value: "false",
			},
			{
				Name:  "LDAP_TLS_VERIFY_CLIENT",
				Value: "try",
			},
			{
				Name:  "LDAP_TLS_CRT_FILENAME",
				Value: ldapCrtFile,
			},
			{
				Name:  "LDAP_TLS_KEY_FILENAME",
				Value: ldapKeyFile,
			},
			{
				Name:  "LDAP_TLS_CA_CRT_FILENAME",
				Value: ldapCACrtFile,
			},
		},
		Ports: []v1.ContainerPort{
			{
				Name:          ldapPortName,
				ContainerPort: int32(ldapPort),
				Protocol:      v1.ProtocolTCP,
			},
			{
				Name:          ldapSSLPortName,
				ContainerPort: int32(ldapSSLPort),
				Protocol:      v1.ProtocolTCP,
			},
		},
		VolumeMounts: []v1.VolumeMount{
			{
				Name:      ldapCerDirtMount,
				MountPath: "/container/service/slapd/assets/certs",
			},
		},
	}
}

// Init container copies certs from secret into volume mount.
func openLDAPInitContainer() v1.Container {
	command := []string{"sh", "-c", "cp /tmp/certs/* /certs; chown -R 999:999 /certs"}

	return v1.Container{
		Name:    "init-" + ldapHostName,
		Image:   openLDAPInitImage,
		Command: command,
		VolumeMounts: []v1.VolumeMount{
			{
				Name:      ldapCerDirtMount,
				MountPath: "/certs",
			},
			{
				Name:      ldapSecretMount,
				MountPath: "/tmp/certs",
			},
		},
	}
}

// Headless service exposing ldap container.
func NewLDAPService() *v1.Service {
	return &v1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:   ldapSubdomain,
			Labels: ldapPodLabels(),
		},
		Spec: v1.ServiceSpec{
			Selector:  ldapPodLabels(),
			Type:      v1.ServiceTypeClusterIP,
			ClusterIP: "None",
			Ports: []v1.ServicePort{
				{
					Name:     ldapPortName,
					Port:     int32(ldapPort),
					Protocol: v1.ProtocolTCP,
				},
				{
					Name:     ldapSSLPortName,
					Port:     int32(ldapSSLPort),
					Protocol: v1.ProtocolTCP,
				},
			},
		},
	}
}

// LDAPAltNames specify dns name accepted from client requests.
func LDAPAltNames(namespace string) []string {
	return []string{
		"localhost",
		ldapHostName,
		fmt.Sprintf("%s.%s.%s.svc", ldapHostName, ldapSubdomain, namespace),
	}
}
