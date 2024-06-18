package kubectl

import (
	"fmt"
	"strings"

	"github.com/couchbase/couchbase-operator/test/cao_test_runner/util/shell"
)

type Cmd struct {
	Command string
	Args    []string
	Flags   map[string]string
}

// ============================================================
// ================ Execute Cmd in Shell ===============
// ============================================================

func (k Cmd) ExecWithoutCapturePrint() error {
	return shell.RunWithoutCapturePrint("kubectl", k.ToCliArgs()...)
}

// ExecWithoutOutputCapture runs Cmd via `kubectl`, where Cmd is a struct holding the kubectl command to run (not including
// `kubectl` itself), the args, and any flags.
// Returns error only (no capture of results) and also logs the log output.
func (k Cmd) ExecWithoutOutputCapture() error {
	return shell.RunWithoutOutputCapture("kubectl", k.ToCliArgs()...)
}

// ExecWithOutputCapture runs Cmd via `kubectl`, where Cmd is a struct holding the kubectl command to run
// (not including `kubectl` itself), the args, and any flags.
// Returns (stdout, stderr, error) and also logs the log output.
func (k Cmd) ExecWithOutputCapture() (string, string, error) {
	return shell.RunWithOutputCapture("kubectl", k.ToCliArgs()...)
}

// func (k Cmd) ExecVPanic() {
//	shutil.RunVPanic("kubectl", k.ToCliArgs()...)
// }

func (k Cmd) Output() (string, error) {
	return shell.Output("kubectl", k.ToCliArgs()...)
}

// func (k Cmd) OutputPanic() string {
//	return shutil.OutputPanic("kubectl", k.ToCliArgs()...)
// }

// ============================================================
// ============= Utility Functions for Cmd =============
// ============================================================

// ToCliArgs adds all the kubectl flags, commands and args and returns a slice ready to be executed in shell along with kubectl.
func (k Cmd) ToCliArgs() []string {
	var args []string
	// Write out flags first because we don't know if the command args will have a -- in them or not
	// and prevent our flags from working.
	for k, v := range k.Flags {
		args = append(args, fmt.Sprintf("--%s=%s", k, v))
	}

	args = append(args, k.Command)
	args = append(args, k.Args...)

	return args
}

// WithFlag adds a flag to the kubectl command. Flags are appended just after "kubectl" like: "kubectl --flag command".
func (k Cmd) WithFlag(name string, value string) Cmd {
	if k.Flags == nil {
		k.Flags = make(map[string]string)
	}

	k.Flags[name] = value

	return k
}

func (k Cmd) WithLabel(label string) Cmd {
	k.Args = append(k.Args, "-l", label)
	return k
}

func (k Cmd) InNamespace(namespace string) Cmd {
	return k.WithFlag("namespace", namespace)
}

func (k Cmd) WithContext(ctxt string) Cmd {
	return k.WithFlag("context", ctxt)
}

// FormatOutput adds the --output flag. Mostly used with `kubectl get`.
// Output format: json, yaml, name, go-template, go-template-file, template, templatefile, jsonpath,
//
//	jsonpath-as-json, jsonpath-file, custom-columns, custom-columns-file, wide
func (k Cmd) FormatOutput(outputType string) Cmd {
	return k.WithFlag("output", outputType)
}

// =============================================
// ========== Basic Kubectl Commands ===========
// =============================================

// ==============================
// kubectl create command(s)
// ==============================

func Create(args ...string) Cmd {
	return Cmd{Command: "create", Args: args}
}

func CreateNamespace(namespace string) Cmd {
	args := []string{"namespace", namespace}
	return Cmd{Command: "create", Args: args}
}

func CreateSecretLiteral(name string, user string, pw string) Cmd {
	args := []string{"secret", "generic", name}
	flags := map[string]string{
		"from-literal=username": user,
		"from-literal=password": pw,
	}

	return Cmd{Command: "create", Args: args, Flags: flags}
}

func CreateFromFiles(paths ...string) Cmd {
	var args []string
	for _, p := range paths {
		args = append(args, "-f", p)
	}

	return Cmd{Command: "create", Args: args}
}

// ==============================
// kubectl delete command(s)
// ==============================

func Delete(args ...string) Cmd {
	return Cmd{Command: "delete", Args: args}
}

func DeleteNamespace(namespace string) Cmd {
	args := []string{"namespace", namespace}
	return Cmd{Command: "delete", Args: args}
}

func DeleteFromFiles(paths ...string) Cmd {
	var args []string
	for _, path := range paths {
		args = append(args, "-f", path)
	}

	return Cmd{Command: "delete", Args: args}
}

func DeleteByTypeAndName(resourceType string, names ...string) Cmd {
	var args []string
	for _, n := range names {
		args = append(args, fmt.Sprintf("%s/%s", resourceType, n))
	}

	return Cmd{Command: "delete", Args: args}
}

// ==============================
// kubectl get command(s)
// ==============================

func Get(args ...string) Cmd {
	return Cmd{Command: "get", Args: args}
}

func GetByTypeAndName(resourceType string, names ...string) Cmd {
	var args []string
	for _, n := range names {
		args = append(args, fmt.Sprintf("%s/%s", resourceType, n))
	}

	return Cmd{Command: "get", Args: args}
}

func GetByFiles(paths ...string) Cmd {
	var args []string
	for _, path := range paths {
		args = append(args, "-f", path)
	}

	return Cmd{Command: "get", Args: args}
}

// =============================================
// ======= Cluster Mgmt Kubectl Commands =======
// =============================================

func ClusterInfoForContext(ctxt string) Cmd {
	args := []string{"--context", ctxt}
	return Cmd{Command: "cluster-info", Args: args}
}

func Taint(node string, key string, value string, effect string) Cmd {
	var args []string
	if value != "" {
		args = []string{"nodes", node, fmt.Sprintf("%s=%s:%s", key, value, effect)}
	} else {
		args = []string{"nodes", node, fmt.Sprintf("%s:%s", key, effect)}
	}

	return Cmd{Command: "taint", Args: args}
}

func Cordon(nodeName string) Cmd {
	return Cmd{Command: "cordon", Args: []string{nodeName}}
}

func Uncordon(nodeName string) Cmd {
	return Cmd{Command: "uncordon", Args: []string{nodeName}}
}

func Drain(nodeName string) Cmd {
	return Cmd{Command: "drain", Args: []string{nodeName}}
}

// =============================================
// ==== Troubleshoot/Debug Kubectl Commands ====
// =============================================

func Logs(args ...string) Cmd {
	return Cmd{Command: "logs", Args: args}
}

func Describe(args ...string) Cmd {
	return Cmd{Command: "describe", Args: args}
}

func Exec(args ...string) Cmd {
	return Cmd{Command: "exec", Args: args}
}

// =============================================
// ========= Advanced Kubectl Commands =========
// =============================================

// ==============================
// kubectl apply command(s)
// ==============================

func Apply(args ...string) Cmd {
	return Cmd{Command: "apply", Args: args}
}

func ApplyFiles(paths ...string) Cmd {
	var args []string
	for _, path := range paths {
		args = append(args, "-f", path)
	}

	return Cmd{Command: "apply", Args: args}
}

// ==============================
// kubectl patch command(s)
// ==============================

func Patch(resource string, data string) Cmd {
	args := []string{resource, "-p", data}
	return Cmd{Command: "patch", Args: args}
}

func PatchMerge(resource string, data string) Cmd {
	args := []string{resource, "--patch", data, "--type", "merge"}
	return Cmd{Command: "patch", Args: args}
}

func PatchJSON(resource string, data string) Cmd {
	args := []string{resource, "--patch", data, "--type", "json"}
	return Cmd{Command: "patch", Args: args}
}

// =============================================
// ========= Settings Kubectl Commands =========
// =============================================

func Label(nodes string, key string, value string) Cmd {
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

	return Cmd{Command: "label", Args: args}
}

func Annotate(resource string, name string, key string, value string) Cmd {
	args := []string{resource, name, fmt.Sprintf("%s=%s", key, value)}
	return Cmd{Command: "annotate", Args: args}
}

// =============================================
// ========== Other Kubectl Commands ===========
// =============================================

func Config(args ...string) Cmd {
	return Cmd{Command: "config", Args: args}
}
