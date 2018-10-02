package resource

import (
	"bufio"
	ctx "context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/couchbase/couchbase-operator/pkg/info/backend"
	"github.com/couchbase/couchbase-operator/pkg/info/context"
	"github.com/couchbase/couchbase-operator/pkg/info/util"
	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil"
	"github.com/couchbase/couchbase-operator/pkg/util/prettytable"

	"github.com/ghodss/yaml"

	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// persistentVolumeClaimResource represents a collection of persistentVolumeClaims
type persistentVolumeClaimResource struct {
	context *context.Context
	// persistentVolumeClaims is the raw output from listing persistentVolumeClaims
	persistentVolumeClaims *v1.PersistentVolumeClaimList
}

// NewPersistentVolumeClaimResource initializes a new persistentVolumeClaim resource
func NewPersistentVolumeClaimResource(context *context.Context) Resource {
	return &persistentVolumeClaimResource{
		context: context,
	}
}

func (r *persistentVolumeClaimResource) Kind() string {
	return "PersistentVolumeClaim"
}

// listDetachedLogPVCs returns an array of all PVCs that are annotated
// as being mounted as couchbase logs and also have been marked as detached
// from their pods.
func listDetachedLogPVCs(pvcs *v1.PersistentVolumeClaimList) ([]*v1.PersistentVolumeClaim, error) {
	logVolumes := []*v1.PersistentVolumeClaim{}
	for _, pvc := range pvcs.Items {
		if ok, err := k8sutil.IsLogPVC(&pvc); err != nil {
			return nil, err
		} else if !ok {
			continue
		}
		if _, ok := pvc.Annotations["pv.couchbase.com/detached"]; !ok {
			continue
		}
		p := pvc
		logVolumes = append(logVolumes, &p)
	}
	return logVolumes, nil
}

// parseRangeField looks for valid ranges e.g. 3-5 and will add these
// to the indices map e.g. 3, 4, 5.
func parseRangeField(field string, max int, indices map[int]interface{}) error {
	// Is it a range?
	parts := strings.Split(field, "-")
	if len(parts) != 2 {
		return fmt.Errorf("Invalid field value %s", field)
	}

	// Check the start and end are valid integers
	start, err := strconv.Atoi(parts[0])
	if err != nil {
		return fmt.Errorf("Invalid range start value %s", field)
	}
	end, err := strconv.Atoi(parts[1])
	if err != nil {
		return fmt.Errorf("Invalid range end value %s", field)
	}

	// Check that the limits are valid for iteration
	if start >= end {
		return fmt.Errorf("Invalid range value %s", field)
	}

	// Accumulate the range
	for index := start; index < end+1; index++ {
		// Check the index is in range
		if index < 0 || index >= max {
			return fmt.Errorf("Index %d out of range", index)
		}

		indices[index] = nil
	}

	return nil
}

// parseField parses and individual field which may be a single integer or
// a range of integers.
func parseField(field string, max int, indices map[int]interface{}) error {
	// Try parse as a stand alone index
	index, err := strconv.Atoi(field)
	if err != nil {
		// Failling that try to parse as a range
		return parseRangeField(field, max, indices)
	}

	// Check the index is in range
	if index < 0 || index >= max {
		return fmt.Errorf("Index %d out of range", index)
	}

	indices[index] = nil

	return nil
}

// parseIndices accepts a comma separated list of indexes and ranges of indexes
// and accumulates them in a map.
func parseIndices(input string, max int) (map[int]interface{}, error) {
	// Buffer up indices in a map as this guarantees we don't collect
	// the same index twice.
	indices := map[int]interface{}{}

	// Sanitise the input, it will probably have a trailling new line.
	input = strings.TrimSpace(input)

	// Split into individual indices or ranges and parse, accumulating the set
	fields := strings.Split(input, ",")
	for _, field := range fields {
		if err := parseField(field, max, indices); err != nil {
			return nil, err
		}
	}

	return indices, nil
}

// createEphemeralPod takes whatever information it can from the PVC and creates
// a pod with the log volume mounted, once up and running it will sit in a sleep
// while we perform the log collection.
func (r *persistentVolumeClaimResource) createEphemeralPod(pvc *v1.PersistentVolumeClaim) (*v1.Pod, func(), error) {
	// Create the pod with the correct name, it'll not get picked up by the operator
	// as the app is not "couchbase".  This has the knock on effect that the logs
	// will be named consistently.
	podName := pvc.Labels["couchbase_node"]
	serverImage := strings.Split(r.context.Config.ServerImage, ":")
	logVolumeName := "logs"

	// Create the pod.
	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: podName,
			Labels: map[string]string{
				// ensure it's ignored my the operator.
				"app": "cbopinfo",
				// needed by k8sutil.WaitForPod().
				"couchbase_node": podName,
			},
		},
		Spec: v1.PodSpec{
			Containers: []v1.Container{
				k8sutil.CouchbaseContainer(serverImage[0], serverImage[1]),
			},
			RestartPolicy: v1.RestartPolicyNever,
			Volumes: []v1.Volume{
				{
					Name: logVolumeName,
					VolumeSource: v1.VolumeSource{
						PersistentVolumeClaim: &v1.PersistentVolumeClaimVolumeSource{
							ClaimName: pvc.Name,
						},
					},
				},
			},
		},
	}

	// Attach the log volume.
	pod.Spec.Containers[0].VolumeMounts = []v1.VolumeMount{
		{
			Name:      logVolumeName,
			MountPath: k8sutil.CouchbaseVolumeMountLogsDir,
		},
	}

	// Run sleep while we are collecting the logs.  This is a day which is overly cautious.
	pod.Spec.Containers[0].Command = []string{
		"/bin/sleep",
	}
	pod.Spec.Containers[0].Args = []string{
		"86400",
	}

	var err error
	pod, err = k8sutil.CreatePod(r.context.KubeClient, pvc.Namespace, pod)
	if err != nil {
		return nil, nil, fmt.Errorf("ephemeral pod %s failed creation for log collection", podName)
	}

	// Wait for the pod to come alive, this should be ample time to pull down a docker image
	// and start the containers.  It's also necessary so we don't block indefinitly and hang.
	c, cancel := ctx.WithTimeout(ctx.Background(), 5*time.Minute)
	defer cancel()
	if err := k8sutil.WaitForPod(c, r.context.KubeClient, pvc.Namespace, podName, ""); err != nil {
		k8sutil.DeletePod(r.context.KubeClient, pvc.Namespace, podName, nil)
		return nil, nil, fmt.Errorf("ephemeral pod %s failed to come up for log collection", podName)
	}

	// Return a clean up closure which can be deferred by the caller.
	cleanup := func() {
		k8sutil.DeletePod(r.context.KubeClient, pvc.Namespace, podName, nil)
	}
	return pod, cleanup, nil
}

// sortPVCs sorts a PVC list based on when the volume was detached earliest
// first.
func sortPVCs(pvcs []*v1.PersistentVolumeClaim) {
	sorter := func(i, j int) bool {
		return pvcs[i].Annotations["pv.couchbase.com/detached"] < pvcs[j].Annotations["pv.couchbase.com/detached"]
	}
	sort.SliceStable(pvcs, sorter)
}

// renderPVCs takes a list of PVCs and renders them as a table containing their
// ID and associated pod name.
func renderPVCs(pvcs []*v1.PersistentVolumeClaim) {
	table := prettytable.Table{
		Header: prettytable.Row{
			"ID",
			"Pod",
			"Detached",
		},
	}

	for index, pvc := range pvcs {
		table.Rows = append(table.Rows, prettytable.Row{
			strconv.Itoa(index),
			pvc.Labels["couchbase_node"],
			pvc.Annotations["pv.couchbase.com/detached"],
		})
	}

	table.Write(os.Stdout)
}

// Fetch collects all persistentVolumeClaims as defined by the configuration
func (r *persistentVolumeClaimResource) Fetch() error {
	selector, err := GetResourceSelector(&r.context.Config)
	if err != nil {
		return err
	}
	r.persistentVolumeClaims, err = r.context.KubeClient.CoreV1().PersistentVolumeClaims(r.context.Config.Namespace).List(metav1.ListOptions{LabelSelector: selector.String()})
	if err != nil {
		return err
	}

	// TODO: This feels quite out of place here, but we literally have 2 days
	// before PM start chasing us

	// Only collect logs if the user has asked for it.
	if !r.context.Config.CollectInfo {
		return nil
	}

	// List detached PVCs.
	logVolumes, err := listDetachedLogPVCs(r.persistentVolumeClaims)
	if err != nil {
		return err
	}

	// Nothing to do, just ignore them
	if len(logVolumes) == 0 {
		return nil
	}

	// Render the PVCs as a time ordered table showing to what the logs belonged.
	fmt.Println("Detected detached log volumes:")
	sortPVCs(logVolumes)
	renderPVCs(logVolumes)

	// Interactively ask the user which ones to collect.
	var indices map[int]interface{}
	for {
		fmt.Print("Please select volumes to collect [e.g. 1,2,5-6 or leave blank for none]: ")
		reader := bufio.NewReader(os.Stdin)
		text, err := reader.ReadString('\n')
		if err != nil {
			continue
		}
		if indices, err = parseIndices(text, len(logVolumes)); err != nil {
			fmt.Println(err)
			continue
		}
		break
	}

	// Collect the logs in parallel.
	resultChan := make(chan *util.CollectInfoResult)
	for index, _ := range indices {
		go func(pvc *v1.PersistentVolumeClaim) {
			// Always return a result.
			result := &util.CollectInfoResult{}
			defer func() {
				resultChan <- result
			}()

			// Create the temporary pod.
			var cleanup func()
			result.Pod, cleanup, result.Err = r.createEphemeralPod(pvc)
			if result.Err != nil {
				return
			}

			// Ensure we clean up the pod in the event of an error.
			defer cleanup()

			// Collect the logs locally to the pod
			result = util.CollectInfo(r.context.KubeClient, r.context.KubeConfig, result.Pod)
			if result.Err != nil {
				return
			}

			// Download the logs to the local host
			paths := []string{result.FileName, result.FileNameRedacted}
			result.Err = util.CopyFromPod(r.context.KubeClient, r.context.KubeConfig, result.Pod, paths)
			if result.Err != nil {
				return
			}
		}(logVolumes[index])
	}

	// Fan in the results for display.
	results := []*util.CollectInfoResult{}
	for i := 0; i < len(indices); i++ {
		result := <-resultChan
		if result.Err != nil {
			fmt.Println(result.Err)
		}
		results = append(results, result)
	}

	fmt.Println("Plain text server logs downloaded to the following locations:")
	for _, result := range results {
		fmt.Println("   ", filepath.Base(result.FileName))
	}

	fmt.Println("Redacted server logs downloaded to the following locations:")
	for _, result := range results {
		fmt.Println("   ", filepath.Base(result.FileNameRedacted))
	}

	return nil
}

func (r *persistentVolumeClaimResource) Write(b backend.Backend) error {
	for _, persistentVolumeClaim := range r.persistentVolumeClaims.Items {
		data, err := yaml.Marshal(persistentVolumeClaim)
		if err != nil {
			return err
		}

		b.WriteFile(util.ArchivePath(r.context.Config.Namespace, r.Kind(), persistentVolumeClaim.Name, persistentVolumeClaim.Name+".yaml"), string(data))
	}
	return nil
}

func (r *persistentVolumeClaimResource) References() []ResourceReference {
	return []ResourceReference{}
}
