package openshiftinstall

import (
	cmdutils "github.com/couchbase/couchbase-operator/test/cao_test_runner/util/cmd_utils"
)

const (
	openShiftInstallRootCmd = "openshift-install"
)

type OpenShiftInstallCmd struct {
	cmdutils.Cmd
}

// WithFlag adds a flag to the openshift-install command. Flags are appended just after "openshift-install" like: "openshift-install --flag command".
func (o *OpenShiftInstallCmd) WithFlag(name string, value string) *OpenShiftInstallCmd {
	if o.Flags == nil {
		o.Flags = make(map[string]string)
	}

	o.Flags[name] = value

	return o
}

func (o *OpenShiftInstallCmd) WithDirectory(directoryName string) *OpenShiftInstallCmd {
	return o.WithFlag("dir", directoryName)
}

func (o *OpenShiftInstallCmd) WithLogLevel(logLevel string) *OpenShiftInstallCmd {
	return o.WithFlag("log-level", logLevel)
}

// =================================================
// ========== Openshift Install Commands ===========
// =================================================

func Analyze(fileName string) *OpenShiftInstallCmd {
	args := []string{"--file", fileName}
	return &OpenShiftInstallCmd{cmdutils.Cmd{RootCommand: openShiftInstallRootCmd, Command: "analyze", Args: args}}
}

func Completion() *OpenShiftInstallCmd {
	args := []string{"bash"}
	return &OpenShiftInstallCmd{cmdutils.Cmd{RootCommand: openShiftInstallRootCmd, Command: "completion", Args: args}}
}

func CoreOS(printStreamJSON bool) *OpenShiftInstallCmd {
	args := []string{}

	if printStreamJSON {
		args = append(args, "print-stream-json")
	}

	return &OpenShiftInstallCmd{cmdutils.Cmd{RootCommand: openShiftInstallRootCmd, Command: "coreos", Args: args}}
}

func CreateCluster() *OpenShiftInstallCmd {
	args := []string{"cluster"}
	return &OpenShiftInstallCmd{cmdutils.Cmd{RootCommand: openShiftInstallRootCmd, Command: "create", Args: args}}
}

func CreateIgnitionConfigs() *OpenShiftInstallCmd {
	args := []string{"ignition-configs"}
	return &OpenShiftInstallCmd{cmdutils.Cmd{RootCommand: openShiftInstallRootCmd, Command: "create", Args: args}}
}

func CreateInstallConfig() *OpenShiftInstallCmd {
	args := []string{"install-config"}
	return &OpenShiftInstallCmd{cmdutils.Cmd{RootCommand: openShiftInstallRootCmd, Command: "create", Args: args}}
}

func CreateManifests() *OpenShiftInstallCmd {
	args := []string{"manifests"}
	return &OpenShiftInstallCmd{cmdutils.Cmd{RootCommand: openShiftInstallRootCmd, Command: "create", Args: args}}
}

func CreateSingleNodeIgnitionConfig() *OpenShiftInstallCmd {
	args := []string{"single-node-ignition-config"}
	return &OpenShiftInstallCmd{cmdutils.Cmd{RootCommand: openShiftInstallRootCmd, Command: "create", Args: args}}
}

func DestroyCluster() *OpenShiftInstallCmd {
	args := []string{"cluster"}
	return &OpenShiftInstallCmd{cmdutils.Cmd{RootCommand: openShiftInstallRootCmd, Command: "destroy", Args: args}}
}

func DestroyBootstrap() *OpenShiftInstallCmd {
	args := []string{"bootstrap"}
	return &OpenShiftInstallCmd{cmdutils.Cmd{RootCommand: openShiftInstallRootCmd, Command: "destroy", Args: args}}
}

func Explain(resource string) *OpenShiftInstallCmd {
	args := []string{resource}
	return &OpenShiftInstallCmd{cmdutils.Cmd{RootCommand: openShiftInstallRootCmd, Command: "explain", Args: args}}
}

func Gather() *OpenShiftInstallCmd {
	args := []string{}
	return &OpenShiftInstallCmd{cmdutils.Cmd{RootCommand: openShiftInstallRootCmd, Command: "gather", Args: args}}
}

func GatherBootstrap(bootstrapHost, sshKeyPath, masterHost string, skipAnalysis bool) *OpenShiftInstallCmd {
	args := []string{
		"bootstrap",
		"--bootstrap", bootstrapHost,
		"--key", sshKeyPath,
		"--master", masterHost,
	}

	if skipAnalysis {
		args = append(args, "--skipAnalysis")
	}

	return &OpenShiftInstallCmd{cmdutils.Cmd{RootCommand: openShiftInstallRootCmd, Command: "gather", Args: args}}
}

func Graph(outputFile string) *OpenShiftInstallCmd {
	args := []string{"--output-file", outputFile}
	return &OpenShiftInstallCmd{cmdutils.Cmd{RootCommand: openShiftInstallRootCmd, Command: "graph", Args: args}}
}

func Migrate() *OpenShiftInstallCmd {
	args := []string{}
	return &OpenShiftInstallCmd{cmdutils.Cmd{RootCommand: openShiftInstallRootCmd, Command: "migrate", Args: args}}
}

func MigrateAzurePrivateDNS(cloudName, resourceGroupName,
	virtualNetworkName, virtualNetworkResourceGroupName, zone string, link bool) *OpenShiftInstallCmd {
	args := []string{
		"azure-privatedns",
		"--cloud-name", cloudName,
		"--resource-group", resourceGroupName,
		"--virtual-network", virtualNetworkName,
		"--virtual-network-resource-group", virtualNetworkResourceGroupName,
		"--zone", zone,
	}

	if link {
		args = append(args, "--link")
	}

	return &OpenShiftInstallCmd{cmdutils.Cmd{RootCommand: openShiftInstallRootCmd, Command: "migrate", Args: args}}
}

func MigrateAzurePrivateDNSEligible(cloudName string) *OpenShiftInstallCmd {
	args := []string{
		"azure-privatedns-eligible",
		"--cloud-name", cloudName,
	}

	return &OpenShiftInstallCmd{cmdutils.Cmd{RootCommand: openShiftInstallRootCmd, Command: "migrate", Args: args}}
}

func GetVersion() *OpenShiftInstallCmd {
	return &OpenShiftInstallCmd{cmdutils.Cmd{RootCommand: openShiftInstallRootCmd, Command: "version"}}
}

func WaitFor() *OpenShiftInstallCmd {
	return &OpenShiftInstallCmd{cmdutils.Cmd{RootCommand: openShiftInstallRootCmd, Command: "wait-for"}}
}

func WaitForBootstrapComplete() *OpenShiftInstallCmd {
	args := []string{"bootstrap-complete"}
	return &OpenShiftInstallCmd{cmdutils.Cmd{RootCommand: openShiftInstallRootCmd, Command: "wait-for", Args: args}}
}

func WaitForInstallComplete() *OpenShiftInstallCmd {
	args := []string{"install-complete"}
	return &OpenShiftInstallCmd{cmdutils.Cmd{RootCommand: openShiftInstallRootCmd, Command: "wait-for", Args: args}}
}
