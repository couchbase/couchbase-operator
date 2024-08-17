package kind

import (
	cmdutils "github.com/couchbase/couchbase-operator/test/cao_test_runner/util/cmd_utils"
)

type KindCmd struct {
	cmdutils.Cmd
}

// WithFlag adds a flag to the kind command. Flags are appended just after "kind" like: "kind --flag command".
func (h *KindCmd) WithFlag(name string, value string) *KindCmd {
	if h.Flags == nil {
		h.Flags = make(map[string]string)
	}

	h.Flags[name] = value

	return h
}

// ===================================================
// =============== Kind Commands ===============
// ===================================================

func Build(kubernetesSource, architecture, baseImage, image, buildType string) *KindCmd {
	args := []string{
		"node-image", kubernetesSource,
		"--arch", architecture,
		"--base-image", baseImage,
		"--image", image,
		"--type", buildType,
	}

	return &KindCmd{cmdutils.Cmd{RootCommand: "kind", Command: "build", Args: args}}
}

func Completion(shellCompletion string) *KindCmd {
	args := []string{shellCompletion}
	return &KindCmd{cmdutils.Cmd{RootCommand: "kind", Command: "completion", Args: args}}
}

func Create(config, image, kubeconfig, name, wait string, retain bool) *KindCmd {
	args := []string{
		"cluster",
		"--config", config,
		"--image", image,
		"--kubeconfig", kubeconfig,
		"--name", name,
		"--wait", wait,
	}
	if retain {
		args = append(args, "--retain")
	}

	return &KindCmd{cmdutils.Cmd{RootCommand: "kind", Command: "create", Args: args}}
}

func DeleteCluster(kubeconfig, name string) *KindCmd {
	args := []string{
		"cluster",
		"--kubeconfig", kubeconfig,
		"--name", name,
	}

	return &KindCmd{cmdutils.Cmd{RootCommand: "kind", Command: "delete", Args: args}}
}

func DeleteClusters(kubeconfig string, all bool, clusters []string) *KindCmd {
	args := []string{
		"clusters",
		"--kubeconfig", kubeconfig,
	}
	if all {
		args = append(args, "--all")
	}

	args = append(args, clusters...)

	return &KindCmd{cmdutils.Cmd{RootCommand: "kind", Command: "delete", Args: args}}
}

func ExportKubeConfig(name, kubeconfig string, internal bool) *KindCmd {
	args := []string{
		"kubeconfig",
		"--kubeconfig", kubeconfig,
		"--name", name,
	}
	if internal {
		args = append(args, "--internal")
	}

	return &KindCmd{cmdutils.Cmd{RootCommand: "kind", Command: "export", Args: args}}
}

func ExportLogs(name, outputDir string) *KindCmd {
	args := []string{
		"logs", outputDir,
		"--name", name,
	}

	return &KindCmd{cmdutils.Cmd{RootCommand: "kind", Command: "export", Args: args}}
}

func GetClusters() *KindCmd {
	args := []string{"clusters"}
	return &KindCmd{cmdutils.Cmd{RootCommand: "kind", Command: "get", Args: args}}
}

func GetKubeConfig(name string, internal bool) *KindCmd {
	args := []string{
		"kubeconfig",
		"--name", name,
	}
	if internal {
		args = append(args, "--internal")
	}

	return &KindCmd{cmdutils.Cmd{RootCommand: "kind", Command: "get", Args: args}}
}

func GetNodes(name string, allClusters bool) *KindCmd {
	args := []string{
		"nodes",
		"--name", name,
	}
	if allClusters {
		args = append(args, "--all-clusters")
	}

	return &KindCmd{cmdutils.Cmd{RootCommand: "kind", Command: "get", Args: args}}
}

func GetVersion() *KindCmd {
	return &KindCmd{cmdutils.Cmd{RootCommand: "kind", Command: "version"}}
}

func LoadImageArchive(imagePath, name string, nodes []string) *KindCmd {
	args := []string{
		"image-archive", imagePath,
		"--name", name,
	}
	args = append(args, nodes...)

	return &KindCmd{cmdutils.Cmd{RootCommand: "kind", Command: "load", Args: args}}
}

func LoadDockerImage(image, name string, nodes []string) *KindCmd {
	args := []string{
		"docker-image", image,
		"--name", name,
	}
	args = append(args, nodes...)

	return &KindCmd{cmdutils.Cmd{RootCommand: "kind", Command: "load", Args: args}}
}
