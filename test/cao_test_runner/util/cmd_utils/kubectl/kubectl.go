package kubectl

import (
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"syscall"

	cmdutils "github.com/couchbase/couchbase-operator/test/cao_test_runner/util/cmd_utils"
	"github.com/sirupsen/logrus"
)

var (
	kubectlRootCmd = "kubectl"
	kubeconfigPath = ""
)

type KubectlCmd struct {
	cmdutils.Cmd
}

func NewKubectlCmd(cmd string, args []string, flags map[string]string) *KubectlCmd {
	k := &KubectlCmd{cmdutils.Cmd{RootCommand: kubectlRootCmd, Command: cmd, Args: args, Flags: flags}}
	if kubeconfigPath != "" {
		k.WithKubeconfigPath(kubeconfigPath)
	}

	return k
}

// If the path of the kubectl binary is not in env, then we can use this function to explicitly set the path
func WithBinaryPath(path string) {
	kubectlRootCmd = path
}

// If the path of the kubeconfig is not in env, then we can use this function to explicitly set the path
func WithKubeonfigPath(path string) {
	kubeconfigPath = path
}

func (k *KubectlCmd) WithLabel(label string) *KubectlCmd {
	k.Args = append(k.Args, "-l", label)
	return k
}

func (k *KubectlCmd) InNamespace(namespace string) *KubectlCmd {
	return k.WithFlag("namespace", namespace)
}

func (k *KubectlCmd) WithContext(ctxt string) *KubectlCmd {
	return k.WithFlag("context", ctxt)
}

func (k *KubectlCmd) WithKubeconfigPath(path string) *KubectlCmd {
	return k.WithFlag("kubeconfig", path)
}

// FormatOutput adds the --output flag. Mostly used with `kubectl get`.
// Output format: json, yaml, name, go-template, go-template-file, template, templatefile, jsonpath,
//
//	jsonpath-as-json, jsonpath-file, custom-columns, custom-columns-file, wide
func (k *KubectlCmd) FormatOutput(outputType string) *KubectlCmd {
	return k.WithFlag("output", outputType)
}

// WithFlag adds a flag to the kubectl command. Flags are appended just after "kubectl" like: "kubectl --flag command".
func (k *KubectlCmd) WithFlag(name, value string) *KubectlCmd {
	if k.Flags == nil {
		k.Flags = make(map[string]string)
	}

	k.Flags[name] = value

	return k
}

// =============================================
// ========== Basic Kubectl Commands ===========
// =============================================

// ==============================
// kubectl create command(s)
// ==============================

func Create(args ...string) *KubectlCmd {
	cmd := "create"
	return NewKubectlCmd(cmd, args, nil)
}

func CreateNamespace(namespace string) *KubectlCmd {
	args := []string{"namespace", namespace}
	cmd := "create"
	return NewKubectlCmd(cmd, args, nil)
}

func CreateSecretLiteral(name, user, pw string) *KubectlCmd {
	args := []string{"secret", "generic", name}
	flags := map[string]string{
		"from-literal=username": user,
		"from-literal=password": pw,
	}

	cmd := "create"
	return NewKubectlCmd(cmd, args, flags)
}

func CreateSecretDockerRegistry(secretName, dockerServer, dockerUsername, dockerPassword, dockerEmail string) *KubectlCmd {
	args := []string{
		"secret", "docker-registry", secretName,
		"--docker-server", dockerServer,
		"--docker-username", dockerUsername,
		"--docker-password", dockerPassword,
		"--docker-email", dockerEmail,
	}

	cmd := "create"
	return NewKubectlCmd(cmd, args, nil)
}

func CreateFromFiles(paths ...string) *KubectlCmd {
	var args []string
	for _, p := range paths {
		args = append(args, "-f", p)
	}

	cmd := "create"
	return NewKubectlCmd(cmd, args, nil)
}

// ==============================
// kubectl delete command(s)
// ==============================

func Delete(args ...string) *KubectlCmd {
	cmd := "delete"
	return NewKubectlCmd(cmd, args, nil)
}

func DeleteNamespace(namespace string) *KubectlCmd {
	args := []string{"namespace", namespace}
	cmd := "delete"
	return NewKubectlCmd(cmd, args, nil)
}

func DeleteFromFiles(paths ...string) *KubectlCmd {
	var args []string
	for _, path := range paths {
		args = append(args, "-f", path)
	}

	cmd := "delete"
	return NewKubectlCmd(cmd, args, nil)
}

func DeleteByTypeAndName(resourceType string, names ...string) *KubectlCmd {
	var args []string
	for _, n := range names {
		args = append(args, fmt.Sprintf("%s/%s", resourceType, n))
	}

	cmd := "delete"
	return NewKubectlCmd(cmd, args, nil)
}

// ==============================
// kubectl get command(s)
// ==============================

func Get(args ...string) *KubectlCmd {
	cmd := "get"
	return NewKubectlCmd(cmd, args, nil)
}

func GetNamespaces() *KubectlCmd {
	args := []string{"namespaces", "-o", "name"}
	cmd := "get"
	return NewKubectlCmd(cmd, args, nil)
}

func GetByTypeAndName(resourceType string, names ...string) *KubectlCmd {
	var args []string
	for _, n := range names {
		args = append(args, fmt.Sprintf("%s/%s", resourceType, n))
	}

	cmd := "get"
	return NewKubectlCmd(cmd, args, nil)
}

func GetByFiles(paths ...string) *KubectlCmd {
	var args []string
	for _, path := range paths {
		args = append(args, "-f", path)
	}

	cmd := "get"
	return NewKubectlCmd(cmd, args, nil)
}

func GetSecretNames() *KubectlCmd {
	args := []string{"secrets", "-o", "name"}
	cmd := "get"
	return NewKubectlCmd(cmd, args, nil)
}

// =============================================
// ======= Cluster Mgmt Kubectl Commands =======
// =============================================

func ClusterInfoForContext(ctxt string) *KubectlCmd {
	args := []string{"--context", ctxt}
	cmd := "cluster-info"
	return NewKubectlCmd(cmd, args, nil)
}

func Taint(nodes []string, key, value, effect string) *KubectlCmd {
	args := []string{"nodes"}

	for _, nodeName := range nodes {
		if nodeName != "" {
			args = append(args, nodeName)
		}
	}

	if value != "" {
		args = append(args, fmt.Sprintf("%s=%s:%s", key, value, effect))
	} else {
		args = append(args, fmt.Sprintf("%s:%s", key, effect))
	}

	cmd := "taint"
	return NewKubectlCmd(cmd, args, nil)
}

func RemoveTaint(nodes []string, key, value, effect string) *KubectlCmd {
	args := []string{"nodes"}

	for _, nodeName := range nodes {
		if nodeName != "" {
			args = append(args, nodeName)
		}
	}

	if value != "" {
		args = append(args, fmt.Sprintf("%s=%s:%s-", key, value, effect))
	} else {
		args = append(args, fmt.Sprintf("%s:%s-", key, effect))
	}

	cmd := "taint"
	return NewKubectlCmd(cmd, args, nil)
}

func Cordon(nodeName string) *KubectlCmd {
	cmd := "cordon"
	args := []string{nodeName}
	return NewKubectlCmd(cmd, args, nil)
}

func Uncordon(nodeName string) *KubectlCmd {
	cmd := "uncordon"
	args := []string{nodeName}
	return NewKubectlCmd(cmd, args, nil)
}

func Drain(nodeName string) *KubectlCmd {
	cmd := "drain"
	args := []string{nodeName}
	return NewKubectlCmd(cmd, args, nil)
}

// =============================================
// ==== Troubleshoot/Debug Kubectl Commands ====
// =============================================

func Logs(args ...string) *KubectlCmd {
	cmd := "logs"
	return NewKubectlCmd(cmd, args, nil)
}

func Describe(args ...string) *KubectlCmd {
	cmd := "describe"
	return NewKubectlCmd(cmd, args, nil)
}

func Exec(podName, containerName string, commandArgs ...string) *KubectlCmd {
	args := []string{podName, "-c", containerName, "--"}
	args = append(args, commandArgs...)
	cmd := "exec"
	return NewKubectlCmd(cmd, args, nil)
}

// =============================================
// ========= Advanced Kubectl Commands =========
// =============================================

// ==============================
// kubectl apply command(s)
// ==============================

func Apply(args ...string) *KubectlCmd {
	cmd := "apply"
	return NewKubectlCmd(cmd, args, nil)
}

func ApplyFiles(paths ...string) *KubectlCmd {
	var args []string
	for _, path := range paths {
		args = append(args, "-f", path)
	}

	cmd := "apply"
	return NewKubectlCmd(cmd, args, nil)
}

// ==============================
// kubectl patch command(s)
// ==============================

func Patch(resource, data string) *KubectlCmd {
	args := []string{resource, "-p", data}
	cmd := "patch"
	return NewKubectlCmd(cmd, args, nil)
}

func PatchMerge(resource, data string) *KubectlCmd {
	args := []string{resource, "--patch", data, "--type", "merge"}
	cmd := "patch"
	return NewKubectlCmd(cmd, args, nil)
}

func PatchJSON(resource, data string) *KubectlCmd {
	args := []string{resource, "--patch", data, "--type", "json"}
	cmd := "patch"
	return NewKubectlCmd(cmd, args, nil)
}

// =============================================
// ========= Settings Kubectl Commands =========
// =============================================

func Label(names []string, resource, key, value string) *KubectlCmd {
	var args []string

	for _, name := range names {
		if name != "" {
			args = append(args, resource+"/"+name)
		}
	}

	label := fmt.Sprintf("%s=%s", key, value)
	args = append(args, label)
	args = append(args, "--overwrite")

	cmd := "label"
	return NewKubectlCmd(cmd, args, nil)
}

func Unlabel(names []string, resource, key string) *KubectlCmd {
	var args []string

	for _, name := range names {
		if name != "" {
			args = append(args, resource+"/"+name)
		}
	}

	label := key + "-"
	args = append(args, label)

	cmd := "label"
	return NewKubectlCmd(cmd, args, nil)
}

func Annotate(resource, name, key, value string) *KubectlCmd {
	args := []string{resource, name, fmt.Sprintf("%s=%s", key, value)}
	cmd := "annotate"
	return NewKubectlCmd(cmd, args, nil)
}

// =============================================
// ========== Config Kubectl Commands ==========
// =============================================

func GetCurrentContext() *KubectlCmd {
	args := []string{"current-context"}
	cmd := "config"
	return NewKubectlCmd(cmd, args, nil)
}

func DeleteContext(contextName string) *KubectlCmd {
	args := []string{"delete-context", contextName}
	cmd := "config"
	return NewKubectlCmd(cmd, args, nil)
}

func GetContexts() *KubectlCmd {
	args := []string{"get-contexts", "-o", "name"}
	cmd := "config"
	return NewKubectlCmd(cmd, args, nil)
}

func RenameContext(oldContextName, newContextName string) *KubectlCmd {
	args := []string{"rename-context", oldContextName, newContextName}
	cmd := "config"
	return NewKubectlCmd(cmd, args, nil)
}

func SetContext(args ...string) *KubectlCmd {
	panic("Not Implemented")
}

func UseContext(contextName string) *KubectlCmd {
	args := []string{"use-context", contextName}
	cmd := "config"
	return NewKubectlCmd(cmd, args, nil)
}

func DeleteUser(userName string) *KubectlCmd {
	args := []string{"delete-user", userName}
	cmd := "config"
	return NewKubectlCmd(cmd, args, nil)
}

func DeleteCluster(clusterName string) *KubectlCmd {
	args := []string{"delete-cluster", clusterName}
	cmd := "config"
	return NewKubectlCmd(cmd, args, nil)
}

// =============================================
// ========== Other Kubectl Commands ===========
// =============================================

// ===============================================
// ========== Kubectl Port-forward Util ==========
// ===============================================

func runPortForward(podName, localPort, remotePort string, stopCh <-chan struct{}, done chan<- struct{}) error {
	cmd := exec.Command(kubectlRootCmd, "port-forward", podName, fmt.Sprintf("%s:%s", localPort, remotePort), "--kubeconfig", kubeconfigPath)

	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start port-forward command: %w", err)
	}

	go func() {
		err := cmd.Wait()
		if err != nil {
			logrus.Errorf("kubectl port-forward process terminated: %v", err)
		}
		close(done)
	}()

	<-stopCh
	if err := cmd.Process.Kill(); err != nil {
		return fmt.Errorf("failed to kill the port-forward process: %w", err)
	}

	return nil
}

func PortForward(podName, localPort, remotePort string, stopCh <-chan struct{}, done chan<- struct{}) {
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		err := runPortForward(podName, localPort, remotePort, stopCh, done)
		if err != nil {
			panic(fmt.Errorf("error running port-forward: %w", err))
		}
	}()
}
