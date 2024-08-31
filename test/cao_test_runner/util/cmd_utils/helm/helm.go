package helm

import (
	"strings"

	cmdutils "github.com/couchbase/couchbase-operator/test/cao_test_runner/util/cmd_utils"
)

const (
	helmRootCmd = "helm"
)

type HelmCmd struct {
	cmdutils.Cmd
}

// WithFlag adds a flag to the helm command. Flags are appended just after "helm" like: "helm --flag command".
func (h *HelmCmd) WithFlag(name string, value string) *HelmCmd {
	if h.Flags == nil {
		h.Flags = make(map[string]string)
	}

	h.Flags[name] = value

	return h
}

func (h *HelmCmd) InNamespace(namespace string) *HelmCmd {
	return h.WithFlag("namespace", namespace)
}

func (h *HelmCmd) InContext(ctxt string) *HelmCmd {
	return h.WithFlag("kube-context", ctxt)
}

func (h *HelmCmd) Set(args ...string) *HelmCmd {
	value := strings.Join(args, " ")
	h.WithFlag("set", value)

	return h
}

func (h *HelmCmd) Edit(args ...string) *HelmCmd {
	value := strings.Join(args, " ")
	h.WithFlag("edit", value)

	return h
}

// =============================================
// =============== Helm Commands ===============
// =============================================

func List(args ...string) *HelmCmd {
	return &HelmCmd{cmdutils.Cmd{RootCommand: helmRootCmd, Command: "list", Args: args}}
}

func Repo(args ...string) *HelmCmd {
	return &HelmCmd{cmdutils.Cmd{RootCommand: helmRootCmd, Command: "repo", Args: args}}
}

func Pull(args ...string) *HelmCmd {
	return &HelmCmd{cmdutils.Cmd{RootCommand: helmRootCmd, Command: "pull", Args: args}}
}

func Install(args ...string) *HelmCmd {
	return &HelmCmd{cmdutils.Cmd{RootCommand: helmRootCmd, Command: "install", Args: args}}
}

func Upgrade(args ...string) *HelmCmd {
	return &HelmCmd{cmdutils.Cmd{RootCommand: helmRootCmd, Command: "upgrade", Args: args}}
}

func Uninstall(args ...string) *HelmCmd {
	return &HelmCmd{cmdutils.Cmd{RootCommand: helmRootCmd, Command: "uninstall", Args: args}}
}
