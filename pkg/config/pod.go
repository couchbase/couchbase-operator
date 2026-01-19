package config

import (
	"context"
	"fmt"
	"strconv"

	v2 "github.com/couchbase/couchbase-operator/pkg/apis/couchbase/v2"
	"github.com/couchbase/couchbase-operator/pkg/client"
	"github.com/couchbase/couchbase-operator/pkg/cluster/persistence"
	"github.com/couchbase/couchbase-operator/pkg/util/constants"
	"github.com/couchbase/couchbase-operator/pkg/util/couchbaseutil"
	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil"
	"github.com/couchbase/couchbase-operator/pkg/util/scheduler"
	"github.com/spf13/cobra"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/cli-runtime/pkg/genericclioptions"
)

type podOptions struct {
	cluster     string
	serverClass string
	index       int
	autoIndex   bool
}

func newPodOptions() *podOptions {
	return &podOptions{}
}

func (o *podOptions) registerPodGenerateFlags(cmd *cobra.Command) {
	cmd.Flags().StringVar(&o.cluster, "couchbase-cluster", "", "The cluster from which to create a pod definition for.")
	cmd.Flags().StringVar(&o.serverClass, "server-class", "", "The server class from which to create a pod definition for.")
	cmd.Flags().IntVar(&o.index, "index", 0, "The index of the pod to create")
	cmd.Flags().BoolVar(&o.autoIndex, "auto-index", false, "Use the persistence secret's to generate a new pod with a new index")

	_ = cmd.MarkFlagRequired("couchbase-cluster")
	_ = cmd.MarkFlagRequired("server-class")
}

func getGeneratePodCommand(command string, flags *genericclioptions.ConfigFlags) *cobra.Command {
	o := newPodOptions()

	cmd := &cobra.Command{
		Use:          "pod",
		Short:        "Generates YAML for a pod of a specific cluster.",
		SilenceUsage: true,
		Hidden:       true,
		Long: normalize(`This command is for debug and recovery purposes only.  It is intended
							to generate a pod definition for a since removed pod, or a new pod when
							support needs to do so.`),
		Example: normalize(fmt.Sprintf(`
			# Create pod scoped to the cluster with a specific index.
			%[1]s generate pod --couchbase-cluster cb-example --server-class all_services --index 3

			# Create pod scoped to a cluster with the next available index.
			%[1]s generate pod --couchbase-cluster cb-example --server-class all_services --auto-index
		`, command)),
		RunE: func(cmd *cobra.Command, args []string) error {
			resources, err := o.generate(flags)
			if err != nil {
				return err
			}
			return dumpResources(resources)
		},
	}
	o.registerPodGenerateFlags(cmd)

	return cmd
}

func getCreatePodCommand(command string, flags *genericclioptions.ConfigFlags) *cobra.Command {
	o := newPodOptions()

	cmd := &cobra.Command{
		Use:          "pod",
		Short:        "Creates a pod of a specific cluster.",
		SilenceUsage: true,
		Hidden:       true,
		Long: normalize(`This command is for debug and recovery purposes only.  It is intended
							to create a pod for a since removed pod, or a new pod when
							support needs to do so.

Note: The Couchbase Operator watches CouchbaseCluster resources and may immediately delete pods it considers unclustered.  Pause or stop the Operator before using this command.`),
		Example: normalize(fmt.Sprintf(`
			# Create pod scoped to the cluster with a specific index.
			%[1]s create pod --couchbase-cluster cb-example --server-class all_services --index 3

			# Create pod scoped to a cluster with the next available index.
			%[1]s create pod --couchbase-cluster cb-example --server-class all_services --auto-index
		`, command)),
		RunE: func(cmd *cobra.Command, args []string) error {
			resources, err := o.generate(flags)
			if err != nil {
				return err
			}
			return createResources(flags, resources)
		},
	}
	o.registerPodGenerateFlags(cmd)

	return cmd
}

func (o *podOptions) generate(flags *genericclioptions.ConfigFlags) ([]runtime.Object, error) {
	namespace, _, err := flags.ToRawKubeConfigLoader().Namespace()
	if err != nil {
		return nil, err
	}

	config, err := flags.ToRESTConfig()
	if err != nil {
		return nil, err
	}

	dynamicClient, _, err := getDynamicClient(flags)
	if err != nil {
		return nil, err
	}

	gvr := schema.GroupVersionResource{
		Group:    "couchbase.com",
		Version:  "v2",
		Resource: "couchbaseclusters",
	}

	res, err := dynamicClient.Resource(gvr).Namespace(namespace).Get(context.TODO(), o.cluster, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}

	converter := runtime.DefaultUnstructuredConverter

	cbc := &v2.CouchbaseCluster{}

	err = converter.FromUnstructured(res.Object, cbc)
	if err != nil {
		return nil, err
	}

	// Check if server class exists
	serverClass := v2.ServerConfig{}
	for _, sc := range cbc.Spec.Servers {
		if sc.Name == o.serverClass {
			serverClass = sc
			break
		}
	}

	if serverClass.Name != o.serverClass {
		return nil, fmt.Errorf("specified server class does not exist")
	}

	k8sClient, err := client.NewClient(context.Background(), namespace, labels.SelectorFromSet(k8sutil.LabelsForCluster(cbc)), config)
	if err != nil {
		return nil, err
	}

	index := o.index

	if o.autoIndex {
		index, err = getPersistenceIndex(k8sClient, cbc)
		if err != nil {
			return nil, err
		}
	}

	resources := []runtime.Object{}
	memberName := couchbaseutil.CreateMemberName(cbc.Name, index)
	m := couchbaseutil.NewMember(namespace, cbc.Name, memberName, cbc.Spec.Image, o.serverClass, cbc.IsTLSEnabled())
	pvcState := &k8sutil.PersistentVolumeClaimState{}

	if serverClass.GetVolumeMounts() != nil {
		// Need to populate pvc state
		pvcState, err = k8sutil.GetPodVolumes(k8sClient, m, cbc, serverClass)
		if err != nil {
			return nil, err
		}

		for _, pvc := range pvcState.List() {
			// We add this whenever we create PVCs
			pvc.Spec.AccessModes = []v1.PersistentVolumeAccessMode{v1.ReadWriteOnce}

			resources = append(resources, pvc)
		}
	}

	// Get server group to create it in
	serverGroup, image, err := getServerGroupAndImageFromScheduler(k8sClient, cbc, m, pvcState, serverClass)
	if err != nil {
		return nil, err
	}

	//TODO:  Pull this from the Couchbase Operator Deployment?
	readinessConfig := k8sutil.PodReadinessConfig{
		PodReadinessDelay:  30,
		PodReadinessPeriod: 30,
	}

	pod, err := k8sutil.CreateCouchbasePodSpec(m, cbc, serverClass, serverGroup, pvcState, image, readinessConfig)
	if err != nil {
		return nil, err
	}

	resources = append(resources, pod)

	return resources, nil
}

func getPersistenceIndex(k8sClient *client.Client, cbc *v2.CouchbaseCluster) (int, error) {
	persistenceSecret, err := persistence.New(k8sClient, cbc)
	if err != nil {
		return 0, err
	}

	indexString, err := persistenceSecret.Get(persistence.PodIndex)
	if err != nil {
		return 0, err
	}

	i, err := strconv.Atoi(indexString)
	if err != nil {
		return 0, err
	}

	return i, nil
}

func getServerGroupAndImageFromScheduler(k8sClient *client.Client, cbc *v2.CouchbaseCluster, m couchbaseutil.Member, pvcState *k8sutil.PersistentVolumeClaimState, serverClass v2.ServerConfig) (string, string, error) {
	pods := k8sClient.Pods.List(constants.LabelServer)

	filtered := []*v1.Pod{}

	for _, pod := range pods {
		if len(pod.OwnerReferences) < 1 {
			continue
		}

		if pod.OwnerReferences[0].UID != cbc.UID {
			continue
		}

		filtered = append(filtered, pod)
	}

	scheduler, err := scheduler.New(filtered, cbc)
	if err != nil {
		return "", "", err
	}

	image := cbc.Spec.ServerClassCouchbaseImage(&serverClass)
	serverGroup := pvcState.AvailabilityZone

	if pvcState.Image != "" {
		image = pvcState.Image
	}

	serverGroup, err = scheduler.Create(serverClass.Name, m.Name(), serverGroup)
	if err != nil {
		return "", "", err
	}

	return serverGroup, image, nil
}
