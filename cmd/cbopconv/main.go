package main

import (
	"bufio"
	"crypto/x509"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/couchbase/couchbase-operator/pkg/apis"
	couchbasev1 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v1"
	couchbasev2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	couchbaseclient "github.com/couchbase/couchbase-operator/pkg/generated/clientset/versioned"
	"github.com/couchbase/couchbase-operator/pkg/util/constants"
	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil"
	util_x509 "github.com/couchbase/couchbase-operator/pkg/util/x509"
	"github.com/couchbase/couchbase-operator/pkg/version"

	"github.com/ghodss/yaml"
	"github.com/spf13/pflag"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	_ "k8s.io/client-go/plugin/pkg/client/auth/azure"
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
	_ "k8s.io/client-go/plugin/pkg/client/auth/oidc"
	_ "k8s.io/client-go/plugin/pkg/client/auth/openstack"
)

// Configuration holds command line parameters
type Configuration struct {
	// ConfigFlags is a generic set of Kubernetes client flags
	ConfigFlags *genericclioptions.ConfigFlags
}

// parseYesNo accepts a 'y' or 'n' and returns a boolean.
func parseYesNo(input string, defaultValue bool) (bool, error) {
	// Sanitise the input, it will probably have a trailling new line.
	input = strings.TrimSpace(input)

	// No input, this is fine
	if input == "" {
		return defaultValue, nil
	}

	if len(input) != 1 {
		return false, fmt.Errorf("invalid input")
	}

	switch input[0] {
	case 'y', 'Y':
		return true, nil
	case 'n', 'N':
		return false, nil
	}

	return false, fmt.Errorf("invalid input")
}

// validateTLS checks that TLS certificates have been regenerated and rotated prior to upgrade.
func validateTLS(kubeclient kubernetes.Interface, cluster *couchbasev1.CouchbaseCluster) error {
	// We have some new rules that are enforced for 2.0 that need to be fixed
	// first by the user...
	serverSecret, err := kubeclient.CoreV1().Secrets(cluster.Namespace).Get(cluster.Spec.TLS.Static.Member.ServerSecret, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("failed to get TLS server secret for cluster %s/%s", cluster.Namespace, cluster.Name)
	}
	operatorSecret, err := kubeclient.CoreV1().Secrets(cluster.Namespace).Get(cluster.Spec.TLS.Static.OperatorSecret, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("failed to get TLS operator secret for cluster %s/%s", cluster.Namespace, cluster.Name)
	}

	ca, ok := operatorSecret.Data["ca.crt"]
	if !ok {
		return fmt.Errorf("failed to get TLS CA certiftcate for cluster %s/%s", cluster.Namespace, cluster.Name)
	}
	cert, ok := serverSecret.Data["chain.pem"]
	if !ok {
		return fmt.Errorf("failed to get TLS certiftcate chain for cluster %s/%s", cluster.Namespace, cluster.Name)
	}
	key, ok := serverSecret.Data["pkey.key"]
	if !ok {
		return fmt.Errorf("failed to get TLS private key for cluster %s/%s", cluster.Namespace, cluster.Name)
	}

	subjectAltNames := util_x509.MandatorySANs(cluster.Name, cluster.Namespace)
	errs := util_x509.Verify(ca, cert, key, x509.ExtKeyUsageServerAuth, subjectAltNames)
	if len(errs) != 0 {
		return fmt.Errorf("failed to verifiy TLS server secret for cluster %s/%s: %v", cluster.Namespace, cluster.Name, errs)
	}

	return nil
}

func main() {
	printVersion := false
	flag.BoolVar(&printVersion, "v", false, "Displays the version and exits")
	flag.Parse()

	if printVersion {
		fmt.Println("cbopconv", version.VersionWithBuildNumber())
		os.Exit(0)
	}

	// Parse the command line arguments.
	config := Configuration{
		ConfigFlags: genericclioptions.NewConfigFlags(),
	}
	flagSet := pflag.NewFlagSet("cbopconv", pflag.ExitOnError)
	config.ConfigFlags.AddFlags(flagSet)
	if err := flagSet.Parse(os.Args[1:]); err != nil {
		fmt.Println("failed to parse arguments:", err)
		os.Exit(1)
	}

	// Register our data types.
	if err := apis.AddToScheme(scheme.Scheme); err != nil {
		fmt.Println("failed to register types:", err)
		os.Exit(1)
	}

	// Create a client.
	kubeConfigLoader := config.ConfigFlags.ToRawKubeConfigLoader()
	kubeConfig, err := kubeConfigLoader.ClientConfig()
	if err != nil {
		fmt.Println("failed to get config:", err)
		os.Exit(1)
	}
	kubeclient, err := kubernetes.NewForConfig(kubeConfig)
	if err != nil {
		fmt.Println("failed to create Kubernetes client:", err)
	}
	clientset, err := couchbaseclient.NewForConfig(kubeConfig)
	if err != nil {
		fmt.Println("failed to create Couchbase client:", err)
		os.Exit(1)
	}

	// Extract the specified or default namespace.
	namespace, _, _ := kubeConfigLoader.Namespace()

	// Grab the existing clusters.  We will do this as raw JSON before unmarshalling
	// into an unstructured data type.  From here we can work out what version the JSON
	// is in, filtering out v2 CRs that have already been converted.  Remember, internally
	// there is only the storage version, so the API does not disciminate between v1 and v2.
	res := &unstructured.UnstructuredList{}
	err = clientset.CouchbaseV2().RESTClient().
		Get().
		Resource("couchbaseclusters").
		Namespace(namespace).
		Do().
		Into(res)
	if err != nil {
		fmt.Println("failed to perform unstructured list")
		os.Exit(1)
	}

	unstructuredClusters := &unstructured.UnstructuredList{}
	for _, item := range res.Items {
		if _, found, _ := unstructured.NestedFieldNoCopy(item.Object, "spec", "baseImage"); found {
			unstructuredClusters.Items = append(unstructuredClusters.Items, item)
		}
	}

	if len(unstructuredClusters.Items) == 0 {
		fmt.Println("No v1 CouchbaseClusters discovered in namespace", config.ConfigFlags.Namespace)
		os.Exit(0)
	}

	clusters := []*couchbasev1.CouchbaseCluster{}
	for _, cluster := range unstructuredClusters.Items {
		cluster, err := clientset.CouchbaseV1().CouchbaseClusters(namespace).Get(cluster.GetName(), metav1.GetOptions{})
		if err != nil {
			fmt.Println("failed to get structured cluster:", err)
			os.Exit(1)
		}
		clusters = append(clusters, cluster)
	}

	// Do safety checks, this is going to be irreversable.  We need to be sure that
	// when we migrate buckets out of their clusters we are not going to have a
	// namespace clash.  Note this doesn't handle if someone has already created
	// a bucket of the same name, this is your own fault as a result.
	bucketNames := map[string]string{}
	for _, cluster := range clusters {
		for _, bucket := range cluster.Spec.BucketSettings {
			if clusterName, ok := bucketNames[bucket.BucketName]; ok {
				fmt.Println("bucket name collision detected: bucket", bucket.BucketName, "already defined by cluster", clusterName)
				os.Exit(1)
			}
			bucketNames[bucket.BucketName] = cluster.Name
		}
	}

	// Work through the clusters one by one.  Then we need to, safely,
	// separate out the buckets and ensure they are only picked up by the correct cluster.
	// Unavoidably if you have multiple buckets of the same name in the same namespace you
	// will get a clash, we can at least detect this and allow the user to make some modifications.
	// Finally we show what we intend to do, before the user gives the go ahead.  This will
	// update and create resources as appropriate.  Also we need to be careful with dependant
	// pods, services etc because their owner reference will change from couchbase.com/v1 to
	// couchbase.com/v2 and needs patching.  This is espcially so for pods as we will ignore
	// them without this
	for _, cluster := range clusters {
		objects := []runtime.Object{}

		// Do like for like copies into the new locations where we can.
		newCluster := &couchbasev2.CouchbaseCluster{
			TypeMeta: metav1.TypeMeta{
				APIVersion: couchbasev2.Group,
				Kind:       couchbasev2.ClusterCRDResourceKind,
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:              cluster.Name,
				Namespace:         cluster.Namespace,
				ResourceVersion:   cluster.ResourceVersion,
				CreationTimestamp: cluster.CreationTimestamp,
				UID:               cluster.UID,
			},
			Spec: couchbasev2.ClusterSpec{
				Image:        cluster.Spec.BaseImage + ":" + cluster.Spec.Version,
				Paused:       cluster.Spec.Paused,
				AntiAffinity: cluster.Spec.AntiAffinity,
				ClusterSettings: couchbasev2.ClusterConfig{
					ClusterName:                            cluster.Spec.ClusterSettings.ClusterName,
					DataServiceMemQuota:                    k8sutil.NewResourceQuantityMi(int64(cluster.Spec.ClusterSettings.DataServiceMemQuota)),
					IndexServiceMemQuota:                   k8sutil.NewResourceQuantityMi(int64(cluster.Spec.ClusterSettings.IndexServiceMemQuota)),
					SearchServiceMemQuota:                  k8sutil.NewResourceQuantityMi(int64(cluster.Spec.ClusterSettings.SearchServiceMemQuota)),
					EventingServiceMemQuota:                k8sutil.NewResourceQuantityMi(int64(cluster.Spec.ClusterSettings.EventingServiceMemQuota)),
					AnalyticsServiceMemQuota:               k8sutil.NewResourceQuantityMi(int64(cluster.Spec.ClusterSettings.AnalyticsServiceMemQuota)),
					IndexStorageSetting:                    couchbasev2.CouchbaseClusterIndexStorageSetting(cluster.Spec.ClusterSettings.IndexStorageSetting),
					AutoFailoverTimeout:                    k8sutil.NewDurationS(int64(cluster.Spec.ClusterSettings.AutoFailoverTimeout)),
					AutoFailoverMaxCount:                   cluster.Spec.ClusterSettings.AutoFailoverMaxCount,
					AutoFailoverOnDataDiskIssues:           cluster.Spec.ClusterSettings.AutoFailoverOnDataDiskIssues,
					AutoFailoverOnDataDiskIssuesTimePeriod: k8sutil.NewDurationS(int64(cluster.Spec.ClusterSettings.AutoFailoverOnDataDiskIssuesTimePeriod)),
					AutoFailoverServerGroup:                cluster.Spec.ClusterSettings.AutoFailoverServerGroup,
				},
				SoftwareUpdateNotifications: cluster.Spec.SoftwareUpdateNotifications,
				VolumeClaimTemplates:        cluster.Spec.VolumeClaimTemplates,
				ServerGroups:                cluster.Spec.ServerGroups,
				SecurityContext:             cluster.Spec.SecurityContext,
				Platform:                    couchbasev2.PlatformType(cluster.Spec.Platform),
				Security: couchbasev2.CouchbaseClusterSecuritySpec{
					AdminSecret: cluster.Spec.AuthSecret,
				},
				Networking: couchbasev2.CouchbaseClusterNetworkingSpec{
					ExposeAdminConsole:        cluster.Spec.ExposeAdminConsole,
					ExposedFeatures:           couchbasev2.ExposedFeatureList(cluster.Spec.ExposedFeatures),
					ExposedFeatureServiceType: cluster.Spec.ExposedFeatureServiceType,
					AdminConsoleServiceType:   cluster.Spec.AdminConsoleServiceType,
				},
				Logging: couchbasev2.CouchbaseClusterLoggingSpec{
					LogRetentionTime:  cluster.Spec.LogRetentionTime,
					LogRetentionCount: cluster.Spec.LogRetentionCount,
				},
			},
		}

		// Do attributes that require type conversion or involve pointers.
		for _, service := range cluster.Spec.AdminConsoleServices {
			newCluster.Spec.Networking.AdminConsoleServices = append(newCluster.Spec.Networking.AdminConsoleServices, couchbasev2.Service(service))
		}
		if cluster.Spec.TLS != nil {
			newCluster.Spec.Networking.TLS = &couchbasev2.TLSPolicy{}
			if cluster.Spec.TLS.Static != nil {
				newCluster.Spec.Networking.TLS.Static = &couchbasev2.StaticTLS{
					OperatorSecret: cluster.Spec.TLS.Static.OperatorSecret,
				}
				if cluster.Spec.TLS.Static.Member != nil {
					newCluster.Spec.Networking.TLS.Static.ServerSecret = cluster.Spec.TLS.Static.Member.ServerSecret

					// We have some new rules that are enforced for 2.0 that need to be fixed
					// first by the user...
					if err := validateTLS(kubeclient, cluster); err != nil {
						fmt.Println(err)
						continue
					}
				}
			}
		}
		if cluster.Spec.DNS != nil {
			newCluster.Spec.Networking.DNS = &couchbasev2.DNS{
				Domain: cluster.Name + "." + cluster.Spec.DNS.Domain,
			}
		}
		for _, class := range cluster.Spec.ServerSettings {
			newClass := couchbasev2.ServerConfig{
				Size:         class.Size,
				Name:         class.Name,
				ServerGroups: class.ServerGroups,
			}
			for _, service := range class.Services {
				newClass.Services = append(newClass.Services, couchbasev2.Service(service))
			}
			if class.Pod != nil {
				newClass.Pod = &corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Labels:      class.Pod.Labels,
						Annotations: class.Pod.Annotations,
					},
					Spec: corev1.PodSpec{
						NodeSelector:                 class.Pod.NodeSelector,
						Tolerations:                  class.Pod.Tolerations,
						AutomountServiceAccountToken: class.Pod.AutomountServiceAccountToken,
						ImagePullSecrets:             class.Pod.ImagePullSecrets,
					},
				}
				newClass.Env = class.Pod.CouchbaseEnv
				newClass.EnvFrom = class.Pod.EnvFrom
				newClass.Resources = class.Pod.Resources
				if class.Pod.VolumeMounts != nil {
					newClass.VolumeMounts = &couchbasev2.VolumeMounts{
						DefaultClaim:    class.Pod.VolumeMounts.DefaultClaim,
						IndexClaim:      class.Pod.VolumeMounts.IndexClaim,
						DataClaim:       class.Pod.VolumeMounts.DataClaim,
						AnalyticsClaims: class.Pod.VolumeMounts.AnalyticsClaims,
						LogsClaim:       class.Pod.VolumeMounts.LogsClaim,
					}
				}
			}
			newCluster.Spec.Servers = append(newCluster.Spec.Servers, newClass)
		}

		objects = append(objects, newCluster)

		// Buckets is the major change here, we need to migrate existing ones into separate
		// resources.
		buckets := []*couchbasev2.CouchbaseBucket{}
		ephemeralBuckets := []*couchbasev2.CouchbaseEphemeralBucket{}
		memcachedBuckets := []*couchbasev2.CouchbaseMemcachedBucket{}
		if !cluster.Spec.DisableBucketManagement && len(cluster.Spec.BucketSettings) != 0 {
			newCluster.Spec.Buckets = couchbasev2.Buckets{
				Managed: true,
				Selector: &metav1.LabelSelector{
					MatchLabels: map[string]string{
						"cluster.couchbase.com": cluster.Name,
					},
				},
			}
			for _, bucket := range cluster.Spec.BucketSettings {
				switch bucket.BucketType {
				case "couchbase":
					newBucket := &couchbasev2.CouchbaseBucket{
						TypeMeta: metav1.TypeMeta{
							APIVersion: couchbasev2.Group,
							Kind:       "CouchbaseBucket",
						},
						ObjectMeta: metav1.ObjectMeta{
							Name: bucket.BucketName,
							Labels: map[string]string{
								"cluster.couchbase.com": cluster.Name,
							},
						},
						Spec: couchbasev2.CouchbaseBucketSpec{
							MemoryQuota:        k8sutil.NewResourceQuantityMi(int64(bucket.BucketMemoryQuota)),
							Replicas:           bucket.BucketReplicas,
							IoPriority:         couchbasev2.CouchbaseBucketIOPriority(bucket.IoPriority),
							EvictionPolicy:     couchbasev2.CouchbaseBucketEvictionPolicy(bucket.EvictionPolicy),
							ConflictResolution: couchbasev2.CouchbaseBucketConflictResolution(bucket.ConflictResolution),
							EnableFlush:        bucket.EnableFlush,
							EnableIndexReplica: bucket.EnableIndexReplica,
							CompressionMode:    couchbasev2.CouchbaseBucketCompressionMode(bucket.CompressionMode),
						},
					}
					buckets = append(buckets, newBucket)
					objects = append(objects, newBucket)
				case "ephemeral":
					newBucket := &couchbasev2.CouchbaseEphemeralBucket{
						TypeMeta: metav1.TypeMeta{
							APIVersion: couchbasev2.Group,
							Kind:       "CouchbaseEphemeralBucket",
						},
						ObjectMeta: metav1.ObjectMeta{
							Name: bucket.BucketName,
							Labels: map[string]string{
								"cluster.couchbase.com": cluster.Name,
							},
						},
						Spec: couchbasev2.CouchbaseEphemeralBucketSpec{
							MemoryQuota:        k8sutil.NewResourceQuantityMi(int64(bucket.BucketMemoryQuota)),
							Replicas:           bucket.BucketReplicas,
							IoPriority:         couchbasev2.CouchbaseBucketIOPriority(bucket.IoPriority),
							EvictionPolicy:     couchbasev2.CouchbaseEphemeralBucketEvictionPolicy(bucket.EvictionPolicy),
							ConflictResolution: couchbasev2.CouchbaseBucketConflictResolution(bucket.ConflictResolution),
							EnableFlush:        bucket.EnableFlush,
							CompressionMode:    couchbasev2.CouchbaseBucketCompressionMode(bucket.CompressionMode),
						},
					}
					ephemeralBuckets = append(ephemeralBuckets, newBucket)
					objects = append(objects, newBucket)
				case "memcached":
					newBucket := &couchbasev2.CouchbaseMemcachedBucket{
						TypeMeta: metav1.TypeMeta{
							APIVersion: couchbasev2.Group,
							Kind:       "CouchbaseMemcachedBucket",
						},
						ObjectMeta: metav1.ObjectMeta{
							Name: bucket.BucketName,
							Labels: map[string]string{
								"cluster.couchbase.com": cluster.Name,
							},
						},
						Spec: couchbasev2.CouchbaseMemcachedBucketSpec{
							MemoryQuota: k8sutil.NewResourceQuantityMi(int64(bucket.BucketMemoryQuota)),
							EnableFlush: bucket.EnableFlush,
						},
					}
					memcachedBuckets = append(memcachedBuckets, newBucket)
					objects = append(objects, newBucket)
				}
			}
		}

		configMap := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name: cluster.Name,
				OwnerReferences: []metav1.OwnerReference{
					newCluster.AsOwner(),
				},
			},
			Data: map[string]string{
				"phase":    string(cluster.Status.Phase),
				"podIndex": strconv.Itoa(cluster.Status.Members.Index),
				"uuid":     cluster.Status.ClusterID,
				"version":  cluster.Status.CurrentVersion,
			},
		}
		objects = append(objects, configMap)

		yamls := []string{}
		for _, object := range objects {
			data, err := yaml.Marshal(object)
			if err != nil {
				fmt.Println("Failed to marshal data structure:", err)
			}
			yamls = append(yamls, string(data))
		}

		fmt.Println("Proposed update for cluster", cluster.Name)
		fmt.Println(strings.Join(yamls, "\n---\n"))
		proceed := false
		for {
			fmt.Println("Okay to proceed? (Y/n)")
			reader := bufio.NewReader(os.Stdin)
			text, err := reader.ReadString('\n')
			if err != nil {
				continue
			}
			if proceed, err = parseYesNo(text, true); err != nil {
				fmt.Println(err)
				continue
			}
			break
		}
		if !proceed {
			continue
		}

		if _, err := clientset.CouchbaseV2().CouchbaseClusters(namespace).Update(newCluster); err != nil {
			fmt.Println("Update of CouchbaseCluster", cluster.Name, "failed - aborting:", err)
			os.Exit(1)
		}
		for _, bucket := range buckets {
			if _, err := clientset.CouchbaseV2().CouchbaseBuckets(namespace).Create(bucket); err != nil {
				fmt.Println("Creation of CouchbaseBucket", bucket.Name, "failed")
				fmt.Println("Bucket name may collide with existing bucket, either rename uniquely or reference existing bucket in the spec.buckets.selector.matchLabels attribute before restarting Operator")
			}
		}
		for _, bucket := range ephemeralBuckets {
			if _, err := clientset.CouchbaseV2().CouchbaseEphemeralBuckets(namespace).Create(bucket); err != nil {
				fmt.Println("Creation of CouchbaseEphemeralBucket", bucket.Name, "failed")
				fmt.Println("Bucket name may collide with existing bucket, either rename uniquely or reference existing bucket in the spec.buckets.selector.matchLabels attribute before restarting Operator")
			}
		}
		for _, bucket := range memcachedBuckets {
			if _, err := clientset.CouchbaseV2().CouchbaseMemcachedBuckets(namespace).Create(bucket); err != nil {
				fmt.Println("Creation of CouchbaseMemcachedBucket", bucket.Name, "failed")
				fmt.Println("Bucket name may collide with existing bucket, either rename uniquely or reference existing bucket in the spec.buckets.selector.matchLabels attribute before restarting Operator")
			}
		}
		if _, err := kubeclient.CoreV1().ConfigMaps(namespace).Create(configMap); err != nil {
			fmt.Println("Creation of ConfigMap failed:", err)
		}

		clusterRequirement, err := labels.NewRequirement(constants.LabelCluster, selection.Equals, []string{cluster.Name})
		if err != nil {
			fmt.Println("Failed to create label requirement:", err)
		}
		selector := labels.NewSelector()
		selector = selector.Add(*clusterRequirement)

		pods, err := kubeclient.CoreV1().Pods(namespace).List(metav1.ListOptions{LabelSelector: selector.String()})
		if err != nil {
			fmt.Println("Listing of Pods failed:", err)
		}

		for _, pod := range pods.Items {
			for i := range pod.OwnerReferences {
				pod.OwnerReferences[i].APIVersion = couchbasev2.Group
			}
			if _, err := kubeclient.CoreV1().Pods(namespace).Update(&pod); err != nil {
				fmt.Println("Update of Pod", pod.Name, "failed:", err)
			}
		}

		services, err := kubeclient.CoreV1().Services(namespace).List(metav1.ListOptions{LabelSelector: selector.String()})
		if err != nil {
			fmt.Println("Listing of Services failed:", err)
		}

		for _, service := range services.Items {
			for i := range service.OwnerReferences {
				service.OwnerReferences[i].APIVersion = couchbasev2.Group
			}
			if _, err := kubeclient.CoreV1().Services(namespace).Update(&service); err != nil {
				fmt.Println("Update of Service", service.Name, "failed:", err)
			}
		}

		pvcs, err := kubeclient.CoreV1().PersistentVolumeClaims(namespace).List(metav1.ListOptions{LabelSelector: selector.String()})
		if err != nil {
			fmt.Println("Listing of PersistentVolumeClaims failed:", err)
		}

		for _, pvc := range pvcs.Items {
			for i := range pvc.OwnerReferences {
				pvc.OwnerReferences[i].APIVersion = couchbasev2.Group
			}
			if _, err := kubeclient.CoreV1().PersistentVolumeClaims(namespace).Update(&pvc); err != nil {
				fmt.Println("Update of PersistentVolumeClaim", pvc.Name, "failed:", err)
			}
		}

		pdbs, err := kubeclient.PolicyV1beta1().PodDisruptionBudgets(namespace).List(metav1.ListOptions{LabelSelector: selector.String()})
		if err != nil {
			fmt.Println("Listing of PodDisruptionBudgets failed:", err)
		}

		for _, pdb := range pdbs.Items {
			for i := range pdb.OwnerReferences {
				pdb.OwnerReferences[i].APIVersion = couchbasev2.Group
			}
			if _, err := kubeclient.PolicyV1beta1().PodDisruptionBudgets(namespace).Update(&pdb); err != nil {
				fmt.Println("Update of PodDisruptionBudgets", pdb.Name, "failed:", err)
			}
		}
	}
}
