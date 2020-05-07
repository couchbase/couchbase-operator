// logs implements a unified server log collection user interface
package logs

import (
	"bufio"
	ctx "context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/couchbase/couchbase-operator/pkg/info/context"
	"github.com/couchbase/couchbase-operator/pkg/info/resource"
	"github.com/couchbase/couchbase-operator/pkg/info/util"
	"github.com/couchbase/couchbase-operator/pkg/util/k8sutil"
	"github.com/couchbase/couchbase-operator/pkg/util/prettytable"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Defines the type of log that needs collecting.
type LogType string

const (
	LogTypePod                   LogType = "pod"
	LogTypePersistentVolumeClaim LogType = "persistentVolumeClaim"
)

// LogEntry is a generic log which may either be a running pod or a log volume.
type LogEntry struct {
	// Name is the name of the member.
	Name string `json:"name"`
	// Type determines what kind of collection to perform.
	Type LogType `json:"type"`
	// Detached tells when "log" type logs were detected as being orphaned.
	Detached string `json:"detached,omitempty"`
	// pod is the pod to collect logs from when Type is LogTypePod
	pod *v1.Pod
	// pvc is the pvc to collect logs from when Type is LogTypePersistentVolumeClaim
	pvc *v1.PersistentVolumeClaim
}

// LogEntryList is an ordered sequence of log entries.
type LogEntryList []LogEntry

// LogList is used to generate JSON output for log entries.
type LogList struct {
	Items LogEntryList `json:"items,omitempty"`
}

// listDetachedLogPVCs returns an array of all PVCs that are annotated
// as being mounted as couchbase logs and also have been marked as detached
// from their pods.
func listDetachedLogPVCs(context *context.Context) ([]*v1.PersistentVolumeClaim, error) {
	selector, err := resource.GetResourceSelectorForCluster(&context.Config)
	if err != nil {
		return nil, err
	}

	pvcs, err := context.KubeClient.CoreV1().PersistentVolumeClaims(context.Namespace()).List(metav1.ListOptions{LabelSelector: selector.String()})
	if err != nil {
		return nil, err
	}

	logVolumes := []*v1.PersistentVolumeClaim{}

	for i := range pvcs.Items {
		pvc := pvcs.Items[i]

		if !k8sutil.IsLogPVC(&pvc) {
			continue
		}

		if _, ok := pvc.Annotations["pv.couchbase.com/detached"]; !ok {
			continue
		}

		temp := pvc
		logVolumes = append(logVolumes, &temp)
	}

	return logVolumes, nil
}

// listRunningPods returns an array of all pods.
func listRunningPods(context *context.Context) ([]*v1.Pod, error) {
	selector, err := resource.GetResourceSelectorForCluster(&context.Config)
	if err != nil {
		return nil, err
	}

	pods, err := context.KubeClient.CoreV1().Pods(context.Namespace()).List(metav1.ListOptions{LabelSelector: selector.String()})
	if err != nil {
		return nil, err
	}

	p := []*v1.Pod{}

	for _, pod := range pods.Items {
		temp := pod
		p = append(p, &temp)
	}

	return p, nil
}

// indexSet holds a unique set of indices to collect.
type indexSet map[int]interface{}

// parseRangeField looks for valid ranges e.g. 3-5 and will add these
// to the indices map e.g. 3, 4, 5.
func parseRangeField(field string, max int, indices indexSet) error {
	// Is it a range?
	parts := strings.Split(field, "-")
	if len(parts) != 2 {
		return fmt.Errorf("invalid field value %s", field)
	}

	// Check the start and end are valid integers
	start, err := strconv.Atoi(parts[0])
	if err != nil {
		return fmt.Errorf("invalid range start value %s", field)
	}

	end, err := strconv.Atoi(parts[1])
	if err != nil {
		return fmt.Errorf("invalid range end value %s", field)
	}

	// Check that the limits are valid for iteration
	if start >= end {
		return fmt.Errorf("invalid range value %s", field)
	}

	// Accumulate the range
	for index := start; index < end+1; index++ {
		// Check the index is in range
		if index < 0 || index >= max {
			return fmt.Errorf("index %d out of range", index)
		}

		indices[index] = nil
	}

	return nil
}

// parseField parses and individual field which may be a single integer or
// a range of integers.
func parseField(field string, max int, indices indexSet) error {
	// Try parse as a stand alone index
	index, err := strconv.Atoi(field)
	if err != nil {
		// Failling that try to parse as a range
		return parseRangeField(field, max, indices)
	}

	// Check the index is in range
	if index < 0 || index >= max {
		return fmt.Errorf("index %d out of range", index)
	}

	indices[index] = nil

	return nil
}

// parseIndices accepts a comma separated list of indexes and ranges of indexes
// and accumulates them in a map.
func parseIndices(input string, max int) (indexSet, error) {
	// Buffer up indices in a map as this guarantees we don't collect
	// the same index twice.
	indices := indexSet{}

	// Sanitise the input, it will probably have a trailling new line.
	input = strings.TrimSpace(input)

	// No input, select all, explicitly check as Split always returns a single
	// empty string :/
	if input == "" || input == "all" {
		for i := 0; i < max; i++ {
			indices[i] = nil
		}

		return indices, nil
	}

	// Split into individual indices or ranges and parse, accumulating the set
	fields := strings.Split(input, ",")
	for _, field := range fields {
		if err := parseField(field, max, indices); err != nil {
			return nil, err
		}
	}

	return indices, nil
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

// createEphemeralPod takes whatever information it can from the PVC and creates
// a pod with the log volume mounted, once up and running it will sit in a sleep
// while we perform the log collection.
func createEphemeralPod(context *context.Context, pvc *v1.PersistentVolumeClaim) (*v1.Pod, func(), error) {
	// Create the pod with the correct name, it'll not get picked up by the operator
	// as the app is not "couchbase".  This has the knock on effect that the logs
	// will be named consistently.
	podName := pvc.Labels["couchbase_node"]
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
				k8sutil.CouchbaseContainer(context.Config.ServerImage),
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

	pod, err = context.KubeClient.CoreV1().Pods(pvc.Namespace).Create(pod)
	if err != nil {
		return nil, nil, fmt.Errorf("ephemeral pod %s failed creation for log collection", podName)
	}

	// Wait for the pod to come alive, this should be ample time to pull down a docker image
	// and start the containers.  It's also necessary so we don't block indefinitely and hang.
	c, cancel := ctx.WithTimeout(ctx.Background(), 5*time.Minute)
	defer cancel()

	if err := k8sutil.WaitForPod(c, context.KubeClient, pvc.Namespace, podName, ""); err != nil {
		_ = context.KubeClient.CoreV1().Pods(pvc.Namespace).Delete(podName, nil)
		return nil, nil, fmt.Errorf("ephemeral pod %s failed to come up for log collection", podName)
	}

	// Return a clean up closure which can be deferred by the caller.
	cleanup := func() {
		_ = context.KubeClient.CoreV1().Pods(pvc.Namespace).Delete(podName, nil)
	}

	return pod, cleanup, nil
}

// sortPods sorts a pod list based on name.
func sortPods(pods []*v1.Pod) {
	sorter := func(i, j int) bool {
		return pods[i].Name < pods[j].Name
	}
	sort.SliceStable(pods, sorter)
}

// sortPVCs sorts a PVC list based on when the volume was detached latest
// first.  This ensures when we concatenate with the pods the most relevant
// stuff comes first and can be captured by a single range potentially.
func sortPVCs(pvcs []*v1.PersistentVolumeClaim) {
	sorter := func(i, j int) bool {
		return pvcs[i].Annotations["pv.couchbase.com/detached"] >= pvcs[j].Annotations["pv.couchbase.com/detached"]
	}
	sort.SliceStable(pvcs, sorter)
}

// gatherLogs collects log entries for resources found in the cluster.
func gatherLogs(context *context.Context) (LogEntryList, error) {
	// List running pods.
	pods, err := listRunningPods(context)
	if err != nil {
		return nil, err
	}

	// List detached PVCs.
	pvcs, err := listDetachedLogPVCs(context)
	if err != nil {
		return nil, err
	}

	// Sort so things make sense to a user.
	sortPods(pods)
	sortPVCs(pvcs)

	logEntries := LogEntryList{}

	for _, pod := range pods {
		logEntries = append(logEntries, LogEntry{
			Name: pod.Name,
			Type: LogTypePod,
			pod:  pod,
		})
	}

	for _, pvc := range pvcs {
		logEntries = append(logEntries, LogEntry{
			Name:     pvc.Labels["couchbase_node"],
			Type:     LogTypePersistentVolumeClaim,
			Detached: pvc.Annotations["pv.couchbase.com/detached"],
			pvc:      pvc,
		})
	}

	// If we are just listing then echo out the data and exit.
	if context.Config.CollectInfoList {
		list := LogList{
			Items: logEntries,
		}

		data, err := json.Marshal(&list)
		if err != nil {
			fmt.Println("unable to marshal log listing")
			os.Exit(1)
		}

		fmt.Println(string(data))

		os.Exit(0)
	}

	return logEntries, nil
}

// renderPVCs takes a list of PVCs and renders them as a table containing their
// ID and associated pod name.
func render(logEntries LogEntryList) {
	table := prettytable.Table{
		Header: prettytable.Row{
			"ID",
			"Pod",
			"Type",
			"Detached",
		},
	}

	for index, logEntry := range logEntries {
		// Format the detached time based on timestamp and elapsed duration, 'cos we are nice
		detached := ""

		if logEntry.Type == LogTypePersistentVolumeClaim {
			t, _ := time.Parse(time.RFC3339, logEntry.Detached)
			detached = fmt.Sprintf("%v (%v ago)", t, time.Since(t).Round(time.Second))
		}

		table.Rows = append(table.Rows, prettytable.Row{
			strconv.Itoa(index),
			logEntry.Name,
			string(logEntry.Type),
			detached,
		})
	}

	_ = table.Write(os.Stdout)
}

// configureInteractive prompts the user for which logs to collect, then if not defined
// on the command line, prompt for log redaction.
func configureInteractive(context *context.Context, logEntries LogEntryList) (indices indexSet) {
	// Echo out the list of logs.
	render(logEntries)

	// Get the requested set of indices to collect.
	for {
		fmt.Print("Please select volumes to collect [e.g. 1,2,5-6 or leave blank for all]: ")

		reader := bufio.NewReader(os.Stdin)

		text, err := reader.ReadString('\n')
		if err != nil {
			continue
		}

		if indices, err = parseIndices(text, len(logEntries)); err != nil {
			fmt.Println(err)
			continue
		}

		break
	}

	// This wasn't specified on the command line, be safe and remind the user.
	if !context.Config.CollectInfoRedact {
		for {
			fmt.Print("Please select whether to enable log redaction [Y/n]: ")

			reader := bufio.NewReader(os.Stdin)

			text, err := reader.ReadString('\n')
			if err != nil {
				continue
			}

			if context.Config.CollectInfoRedact, err = parseYesNo(text, true); err != nil {
				fmt.Println(err)
				continue
			}

			break
		}
	}

	return
}

// configureNonInteractive allows the indices string to be specified on the command line.
func configureNonInteractive(context *context.Context, logEntries LogEntryList) indexSet {
	indices, err := parseIndices(context.Config.CollectInfoCollect, len(logEntries))
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	return indices
}

// configure performs either an interactive dialog by default or non-interactive
// if specified on the CLI.
func configure(context *context.Context, logEntries LogEntryList) indexSet {
	if context.Config.CollectInfoCollect == "" {
		return configureInteractive(context, logEntries)
	}

	return configureNonInteractive(context, logEntries)
}

// Collect is the top level server log collection function.
func Collect(context *context.Context) error {
	// Only collect logs if the user has asked for it.
	if !context.Config.CollectInfo {
		return nil
	}

	// Gather a unified lost of pods and persistent log volumes.
	logList, err := gatherLogs(context)
	if err != nil {
		return err
	}

	// Nothing to do, just ignore them
	if len(logList) == 0 {
		return nil
	}

	// Configure collection and return a sete of indices into the log list to
	// collect and download.
	indices := configure(context, logList)

	// Collect the logs in parallel.
	resultChan := make(chan *util.CollectInfoResult)

	for index := range indices {
		go func(entry LogEntry) {
			// Always return a result.
			result := &util.CollectInfoResult{
				Pod: entry.pod,
			}

			defer func() {
				resultChan <- result
			}()

			// Handle log volumes specially, they need an ephemeral pod creating
			// and destroying once complete.
			if entry.Type == LogTypePersistentVolumeClaim {
				// Create the temporary pod.
				var cleanup func()

				result.Pod, cleanup, result.Err = createEphemeralPod(context, entry.pvc)
				if result.Err != nil {
					return
				}

				// Ensure we clean up the pod in the event of an error.
				defer cleanup()
			}

			// Collect the logs locally to the pod
			result = util.CollectInfo(context, result.Pod)
			if result.Err != nil {
				return
			}

			// Download the logs to the local host
			result.Err = util.CopyFromPod(context, result.Pod, []string{result.FileName})
			if result.Err != nil {
				return
			}

			// Clean up so as to not exhaust space
			result.Err = util.CleanLogs(context, result.Pod)
			if result.Err != nil {
				return
			}
		}(logList[index])
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

	fmt.Println("Server logs downloaded to the following files:")

	for _, result := range results {
		fmt.Println("   ", filepath.Base(result.FileName))
	}

	return nil
}
