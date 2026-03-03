/*
Copyright 2022-Present Couchbase, Inc.

Use of this software is governed by the Business Source License included in
the file licenses/BSL-Couchbase.txt.  As of the Change Date specified in that
file, in accordance with the Business Source License, use of this software will
be governed by the Apache License, Version 2.0, included in the file
licenses/APL2.txt.
*/

package e2eutil

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/couchbase/couchbase-operator/pkg/certification/util"
	"github.com/couchbase/couchbase-operator/test/e2e/types"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

type Minio struct {
	Namespace   string
	Service     *v1.Service
	Pod         *v1.Pod
	tls         bool
	CASecret    *v1.Secret
	name        string
	kubernetes  *types.Cluster
	Endpoint    string
	credentials *minioCredentials
}

type minioCredentials struct {
	accessKey string
	secretID  string
	region    string
}

func (minio *Minio) WithCredentials(accessKey string, secretID string, region string) *Minio {
	minio.credentials = &minioCredentials{
		accessKey: accessKey,
		secretID:  secretID,
		region:    region,
	}

	return minio
}

func (minio *Minio) WithName(name string) *Minio {
	minio.name = name
	return minio
}

func (minio *Minio) WithTLS(enabled bool) *Minio {
	minio.tls = enabled
	return minio
}

func MinioOptions(kubernetes *types.Cluster) *Minio {
	minio := &Minio{kubernetes: kubernetes, Namespace: kubernetes.Namespace}
	return minio
}

func (minio *Minio) Create() (*Minio, error) {
	if len(minio.name) == 0 {
		minio.name = "minio"
	}

	if err := minio.createService(); err != nil {
		return nil, err
	}

	if err := minio.createPod(); err != nil {
		return nil, err
	}

	return minio, nil
}

func (minio *Minio) WaitUntilReady(waitTime time.Duration) error {
	client := http.Client{}

	if minio.tls {
		cert := minio.CASecret.Data["tls.crt"]
		caCertPool := x509.NewCertPool()
		caCertPool.AppendCertsFromPEM(cert)

		t := &http.Transport{
			TLSClientConfig: &tls.Config{
				RootCAs: caCertPool,
			},
		}
		client = http.Client{Transport: t, Timeout: 15 * time.Second}
	}

	callback := func() error {
		response, err := client.Get(minio.Endpoint + "/minio/health/live")
		if err != nil {
			return err
		}

		defer response.Body.Close()

		if response.StatusCode != http.StatusOK {
			return util.ErrConditionUnready
		}

		return nil
	}

	return util.WaitFor(callback, waitTime)
}

func (minio *Minio) MustWaitUntilReady(t *testing.T, waitTime time.Duration) {
	err := minio.WaitUntilReady(waitTime)
	if err != nil {
		Die(t, err)
	}
}

func (minio *Minio) createService() error {
	minioService := &v1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name: minio.name,
		},
		Spec: v1.ServiceSpec{
			Selector: map[string]string{
				"app": minio.name,
			},
			Ports: []v1.ServicePort{
				{
					Name:       "api",
					Protocol:   v1.ProtocolTCP,
					Port:       9000,
					TargetPort: intstr.FromInt(9000),
				},
			},
		},
	}

	var err error
	if minio.Service, err = minio.kubernetes.KubeClient.CoreV1().Services(minio.Namespace).Create(context.TODO(), minioService, metav1.CreateOptions{}); err != nil {
		return err
	}

	minio.Endpoint = fmt.Sprintf("http://%s.%s:9000", minio.Service.Name, minio.Service.Namespace)

	if minio.tls {
		minio.Endpoint = fmt.Sprintf("https://%s.%s:9000", minio.Service.Name, minio.Service.Namespace)
		minio.CASecret, err = CreateTLSPair(minio.kubernetes, fmt.Sprintf("%s.%s", minio.Service.Name, minio.Service.Namespace))

		if err != nil {
			return err
		}
	}

	return nil
}

func (minio *Minio) createPod() error {
	args := []string{"server", "/data"}

	var port int32 = 9000

	minioPod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: minio.name,
			Labels: map[string]string{
				"app": minio.name,
			},
		},
		Spec: v1.PodSpec{
			RestartPolicy: v1.RestartPolicyNever,
			Containers: []v1.Container{
				{
					Name:  minio.name,
					Image: "minio/minio",
					Args:  args,
					VolumeMounts: []v1.VolumeMount{
						{
							Name:      "datavolume",
							MountPath: "/data",
						},
					},
					Ports: []v1.ContainerPort{
						{
							Name:          "api",
							ContainerPort: port,
						},
					},
					Env: []v1.EnvVar{
						{
							Name:  "MINIO_ROOT_USER",
							Value: minio.credentials.accessKey,
						},
						{
							Name:  "MINIO_ROOT_PASSWORD",
							Value: minio.credentials.secretID,
						},
						{
							Name:  "MINIO_REGION_NAME",
							Value: minio.credentials.region,
						},
					},
				},
			},
			Volumes: []v1.Volume{
				{
					Name: "datavolume",
					VolumeSource: v1.VolumeSource{
						EmptyDir: &v1.EmptyDirVolumeSource{},
					},
				},
			},
		},
	}

	if minio.tls {
		volName := "tls-certs"
		tlsVol := v1.Volume{
			Name: volName,
			VolumeSource: v1.VolumeSource{
				Secret: &v1.SecretVolumeSource{
					SecretName: minio.CASecret.Name,
				},
			},
		}

		tlsVolMount := []v1.VolumeMount{
			{
				Name:      volName,
				MountPath: "/root/.minio/certs/private.key",
				SubPath:   "tls.key",
			}, {
				Name:      volName,
				MountPath: "/root/.minio/certs/public.crt",
				SubPath:   "tls.crt",
			},
		}

		minioPod.Spec.Volumes = append(minioPod.Spec.Volumes, tlsVol)
		minioPod.Spec.Containers[0].VolumeMounts = append(minioPod.Spec.Containers[0].VolumeMounts, tlsVolMount...)
	}

	var err error
	if minioPod, err = minio.kubernetes.KubeClient.CoreV1().Pods(minio.Namespace).Create(context.TODO(), minioPod, metav1.CreateOptions{}); err != nil {
		return err
	}

	minio.Pod = minioPod

	return nil
}

func (minio *Minio) CleanUp() {
	_ = minio.kubernetes.KubeClient.CoreV1().Pods(minio.Namespace).Delete(context.TODO(), minio.Pod.Name, metav1.DeleteOptions{})
	_ = minio.kubernetes.KubeClient.CoreV1().Services(minio.Namespace).Delete(context.TODO(), minio.Service.Name, metav1.DeleteOptions{})
}

func CreateTLSPair(kubernetes *types.Cluster, host string) (*v1.Secret, error) {
	validFrom := time.Now().In(time.UTC)
	validTo := validFrom.AddDate(10, 0, 0)

	CA, err := NewCertificateAuthority(KeyTypeRSA, "Couchbase CA", validFrom, validTo, CertTypeCA)
	if err != nil {
		return nil, err
	}

	req := CreateKeyPairReqData(KeyTypeRSA, KeyEncodingPKCS8, CertTypeServer, CreateCertReqDNS("Couchbase CA", []string{host}))

	_, key, cert, err := req.Generate(CA, validFrom, validTo)
	if err != nil {
		return nil, err
	}

	secret := &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name: "minio-secret",
		},
		Data: map[string][]byte{
			v1.TLSCertKey:       cert,
			v1.TLSPrivateKeyKey: key,
		},
	}

	secret, err = CreateSecret(kubernetes, secret)
	if err != nil {
		return nil, err
	}

	return secret, nil
}
