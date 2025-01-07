package kubectl

import (
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"

	cmdutils "github.com/couchbase/couchbase-operator/test/cao_test_runner/util/cmd_utils"
	"github.com/sirupsen/logrus"
)

var (
	kubectlRootCmd = "kubectl"
)

type KubectlCmd struct {
	cmdutils.Cmd
}

// If the path of the kubectl binary is not in env, then we can use this function to explicitly set the path
func WithBinaryPath(path string) {
	kubectlRootCmd = path
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

// FormatOutput adds the --output flag. Mostly used with `kubectl get`.
// Output format: json, yaml, name, go-template, go-template-file, template, templatefile, jsonpath,
//
//	jsonpath-as-json, jsonpath-file, custom-columns, custom-columns-file, wide
func (k *KubectlCmd) FormatOutput(outputType string) *KubectlCmd {
	return k.WithFlag("output", outputType)
}

// WithFlag adds a flag to the kubectl command. Flags are appended just after "kubectl" like: "kubectl --flag command".
func (k *KubectlCmd) WithFlag(name string, value string) *KubectlCmd {
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
	return &KubectlCmd{cmdutils.Cmd{RootCommand: kubectlRootCmd, Command: "create", Args: args}}
}

func CreateNamespace(namespace string) *KubectlCmd {
	args := []string{"namespace", namespace}
	return &KubectlCmd{cmdutils.Cmd{RootCommand: kubectlRootCmd, Command: "create", Args: args}}
}

func CreateSecretLiteral(name string, user string, pw string) *KubectlCmd {
	args := []string{"secret", "generic", name}
	flags := map[string]string{
		"from-literal=username": user,
		"from-literal=password": pw,
	}

	return &KubectlCmd{cmdutils.Cmd{RootCommand: kubectlRootCmd, Command: "create", Args: args, Flags: flags}}
}

func CreateSecretDockerRegistry(secretName, dockerServer, dockerUsername, dockerPassword, dockerEmail string) *KubectlCmd {
	args := []string{
		"secret", "docker-registry", secretName,
		"--docker-server", dockerServer,
		"--docker-username", dockerUsername,
		"--docker-password", dockerPassword,
		"--docker-email", dockerEmail,
	}
	return &KubectlCmd{cmdutils.Cmd{RootCommand: kubectlRootCmd, Command: "create", Args: args}}
}

func CreateFromFiles(paths ...string) *KubectlCmd {
	var args []string
	for _, p := range paths {
		args = append(args, "-f", p)
	}

	return &KubectlCmd{cmdutils.Cmd{RootCommand: kubectlRootCmd, Command: "create", Args: args}}
}

// ==============================
// kubectl delete command(s)
// ==============================

func Delete(args ...string) *KubectlCmd {
	return &KubectlCmd{cmdutils.Cmd{RootCommand: kubectlRootCmd, Command: "delete", Args: args}}
}

func DeleteNamespace(namespace string) *KubectlCmd {
	args := []string{"namespace", namespace}
	return &KubectlCmd{cmdutils.Cmd{RootCommand: kubectlRootCmd, Command: "delete", Args: args}}
}

func DeleteFromFiles(paths ...string) *KubectlCmd {
	var args []string
	for _, path := range paths {
		args = append(args, "-f", path)
	}

	return &KubectlCmd{cmdutils.Cmd{RootCommand: kubectlRootCmd, Command: "delete", Args: args}}
}

func DeleteByTypeAndName(resourceType string, names ...string) *KubectlCmd {
	var args []string
	for _, n := range names {
		args = append(args, fmt.Sprintf("%s/%s", resourceType, n))
	}

	return &KubectlCmd{cmdutils.Cmd{RootCommand: kubectlRootCmd, Command: "delete", Args: args}}
}

// ==============================
// kubectl get command(s)
// ==============================

func Get(args ...string) *KubectlCmd {
	return &KubectlCmd{cmdutils.Cmd{RootCommand: kubectlRootCmd, Command: "get", Args: args}}
}

func GetNamespaces() *KubectlCmd {
	args := []string{"namespaces", "-o", "name"}
	return &KubectlCmd{cmdutils.Cmd{RootCommand: kubectlRootCmd, Command: "get", Args: args}}
}

func GetByTypeAndName(resourceType string, names ...string) *KubectlCmd {
	var args []string
	for _, n := range names {
		args = append(args, fmt.Sprintf("%s/%s", resourceType, n))
	}

	return &KubectlCmd{cmdutils.Cmd{RootCommand: kubectlRootCmd, Command: "get", Args: args}}
}

func GetByFiles(paths ...string) *KubectlCmd {
	var args []string
	for _, path := range paths {
		args = append(args, "-f", path)
	}

	return &KubectlCmd{cmdutils.Cmd{RootCommand: kubectlRootCmd, Command: "get", Args: args}}
}

func GetSecretNames() *KubectlCmd {
	args := []string{"secrets", "-o", "name"}
	return &KubectlCmd{cmdutils.Cmd{RootCommand: kubectlRootCmd, Command: "get", Args: args}}
}

// =============================================
// ======= Cluster Mgmt Kubectl Commands =======
// =============================================

func ClusterInfoForContext(ctxt string) *KubectlCmd {
	args := []string{"--context", ctxt}
	return &KubectlCmd{cmdutils.Cmd{RootCommand: kubectlRootCmd, Command: "cluster-info", Args: args}}
}

func Taint(node string, key string, value string, effect string) *KubectlCmd {
	var args []string
	if value != "" {
		args = []string{"nodes", node, fmt.Sprintf("%s=%s:%s", key, value, effect)}
	} else {
		args = []string{"nodes", node, fmt.Sprintf("%s:%s", key, effect)}
	}

	return &KubectlCmd{cmdutils.Cmd{RootCommand: kubectlRootCmd, Command: "taint", Args: args}}
}

func RemoveTaint(node string, key string, value string, effect string) *KubectlCmd {
	var args []string
	if value != "" {
		args = []string{"nodes", node, fmt.Sprintf("%s=%s:%s-", key, value, effect)}
	} else {
		args = []string{"nodes", node, fmt.Sprintf("%s:%s-", key, effect)}
	}

	return &KubectlCmd{cmdutils.Cmd{RootCommand: kubectlRootCmd, Command: "taint", Args: args}}
}

func Cordon(nodeName string) *KubectlCmd {
	return &KubectlCmd{cmdutils.Cmd{RootCommand: kubectlRootCmd, Command: "cordon", Args: []string{nodeName}}}
}

func Uncordon(nodeName string) *KubectlCmd {
	return &KubectlCmd{cmdutils.Cmd{RootCommand: kubectlRootCmd, Command: "uncordon", Args: []string{nodeName}}}
}

func Drain(nodeName string) *KubectlCmd {
	return &KubectlCmd{cmdutils.Cmd{RootCommand: kubectlRootCmd, Command: "drain", Args: []string{nodeName}}}
}

// =============================================
// ==== Troubleshoot/Debug Kubectl Commands ====
// =============================================

func Logs(args ...string) *KubectlCmd {
	return &KubectlCmd{cmdutils.Cmd{RootCommand: kubectlRootCmd, Command: "logs", Args: args}}
}

func Describe(args ...string) *KubectlCmd {
	return &KubectlCmd{cmdutils.Cmd{RootCommand: kubectlRootCmd, Command: "describe", Args: args}}
}

func Exec(podName, containerName string, commandArgs ...string) *KubectlCmd {
	args := []string{podName, "-c", containerName, "--"}

	args = append(args, commandArgs...)

	return &KubectlCmd{cmdutils.Cmd{RootCommand: kubectlRootCmd, Command: "exec", Args: args}}
}

// =============================================
// ========= Advanced Kubectl Commands =========
// =============================================

// ==============================
// kubectl apply command(s)
// ==============================

func Apply(args ...string) *KubectlCmd {
	return &KubectlCmd{cmdutils.Cmd{RootCommand: kubectlRootCmd, Command: "apply", Args: args}}
}

func ApplyFiles(paths ...string) *KubectlCmd {
	var args []string
	for _, path := range paths {
		args = append(args, "-f", path)
	}

	return &KubectlCmd{cmdutils.Cmd{RootCommand: kubectlRootCmd, Command: "apply", Args: args}}
}

// ==============================
// kubectl patch command(s)
// ==============================

func Patch(resource string, data string) *KubectlCmd {
	args := []string{resource, "-p", data}
	return &KubectlCmd{cmdutils.Cmd{RootCommand: kubectlRootCmd, Command: "patch", Args: args}}
}

func PatchMerge(resource string, data string) *KubectlCmd {
	args := []string{resource, "--patch", data, "--type", "merge"}
	return &KubectlCmd{cmdutils.Cmd{RootCommand: kubectlRootCmd, Command: "patch", Args: args}}
}

func PatchJSON(resource string, data string) *KubectlCmd {
	args := []string{resource, "--patch", data, "--type", "json"}
	return &KubectlCmd{cmdutils.Cmd{RootCommand: kubectlRootCmd, Command: "patch", Args: args}}
}

// =============================================
// ========= Settings Kubectl Commands =========
// =============================================

func Label(nodes string, key string, value string) *KubectlCmd {
	var args []string

	tokens := strings.Split(nodes, " ")
	for _, t := range tokens {
		if t != "" {
			args = append(args, "nodes/"+t)
		}
	}

	label := fmt.Sprintf("%s=%s", key, value)
	args = append(args, label)
	args = append(args, "--overwrite")

	return &KubectlCmd{cmdutils.Cmd{RootCommand: kubectlRootCmd, Command: "label", Args: args}}
}

func Unlabel(nodes string, key string) *KubectlCmd {
	var args []string

	tokens := strings.Split(nodes, " ")
	for _, t := range tokens {
		if t != "" {
			args = append(args, "nodes/"+t)
		}
	}

	label := "%s-" + key
	args = append(args, label)

	return &KubectlCmd{cmdutils.Cmd{RootCommand: kubectlRootCmd, Command: "label", Args: args}}
}

func Annotate(resource string, name string, key string, value string) *KubectlCmd {
	args := []string{resource, name, fmt.Sprintf("%s=%s", key, value)}
	return &KubectlCmd{cmdutils.Cmd{RootCommand: kubectlRootCmd, Command: "annotate", Args: args}}
}

// =============================================
// ========== Config Kubectl Commands ==========
// =============================================

func GetCurrentContext() *KubectlCmd {
	args := []string{"current-context"}
	return &KubectlCmd{cmdutils.Cmd{RootCommand: kubectlRootCmd, Command: "config", Args: args}}
}

func DeleteContext(contextName string) *KubectlCmd {
	args := []string{"delete-context", contextName}
	return &KubectlCmd{cmdutils.Cmd{RootCommand: kubectlRootCmd, Command: "config", Args: args}}
}

func GetContexts() *KubectlCmd {
	args := []string{"get-contexts", "-o", "name"}
	return &KubectlCmd{cmdutils.Cmd{RootCommand: kubectlRootCmd, Command: "config", Args: args}}
}

func RenameContext(oldContextName, newContextName string) *KubectlCmd {
	args := []string{"rename-context", oldContextName, newContextName}
	return &KubectlCmd{cmdutils.Cmd{RootCommand: kubectlRootCmd, Command: "config", Args: args}}
}

func SetContext(args ...string) *KubectlCmd {
	panic("Not Implemented")
}

func UseContext(contextName string) *KubectlCmd {
	args := []string{"use-context", contextName}
	return &KubectlCmd{cmdutils.Cmd{RootCommand: kubectlRootCmd, Command: "config", Args: args}}
}

func DeleteUser(userName string) *KubectlCmd {
	args := []string{"delete-user", userName}
	return &KubectlCmd{cmdutils.Cmd{RootCommand: kubectlRootCmd, Command: "config", Args: args}}
}

func DeleteCluster(clusterName string) *KubectlCmd {
	args := []string{"delete-cluster", clusterName}
	return &KubectlCmd{cmdutils.Cmd{RootCommand: kubectlRootCmd, Command: "config", Args: args}}
}

// =============================================
// ========== Other Kubectl Commands ===========
// =============================================

// ===============================================
// ========== Kubectl Port-forward Util ==========
// ===============================================

func runPortForward(podName, localPort, remotePort string, stopCh <-chan struct{}, done chan<- struct{}) error {
	cmd := exec.Command(kubectlRootCmd, "port-forward", podName, fmt.Sprintf("%s:%s", localPort, remotePort))

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
