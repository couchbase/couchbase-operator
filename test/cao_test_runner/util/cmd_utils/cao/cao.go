package cao

import (
	"encoding/json"
	"fmt"
	"strings"

	cmdutils "github.com/couchbase/couchbase-operator/test/cao_test_runner/util/cmd_utils"
)

var (
	caoRootCmd = "cao"
)

type CaoCmd struct {
	cmdutils.Cmd
}

// If the path of the cao binary is not in env, then we can use this function to explicitly set the path
func WithBinaryPath(path string) {
	caoRootCmd = path
}

// WithFlag adds a flag to the cao command. Flags are appended just after "cao" like: "cao --flag command".
func (c *CaoCmd) WithFlag(name string, value string) *CaoCmd {
	if c.Flags == nil {
		c.Flags = make(map[string]string)
	}

	c.Flags[name] = value

	return c
}

func (c *CaoCmd) As(asUser string) *CaoCmd {
	return c.WithFlag("as", asUser)
}

func (c *CaoCmd) AsGroup(asGroup []string) *CaoCmd {
	jsonBytes, err := json.Marshal(asGroup)
	if err != nil {
		return nil
	}

	jsonString := string(jsonBytes)
	escapedString := strings.ReplaceAll(jsonString, `"`, `\"`)

	return c.WithFlag("as-group", escapedString)
}

func (c *CaoCmd) AsUID(asUID string) *CaoCmd {
	return c.WithFlag("as-uid", asUID)
}

func (c *CaoCmd) WithCacheDir(cacheDir string) *CaoCmd {
	return c.WithFlag("cache-dir", cacheDir)
}

func (c *CaoCmd) WithCertificateAuthority(certificateAuthority string) *CaoCmd {
	return c.WithFlag("certificate-authority", certificateAuthority)
}

func (c *CaoCmd) WithClientCertificate(clientCertificate string) *CaoCmd {
	return c.WithFlag("client-certifcate", clientCertificate)
}

func (c *CaoCmd) WithClientKey(clientKey string) *CaoCmd {
	return c.WithFlag("client-key", clientKey)
}

func (c *CaoCmd) InCluster(cluster string) *CaoCmd {
	return c.WithFlag("cluster", cluster)
}

func (c *CaoCmd) InContext(context string) *CaoCmd {
	return c.WithFlag("context", context)
}

func (c *CaoCmd) DisableCompression() *CaoCmd {
	return c.WithFlag("disable-compression", "true")
}

func (c *CaoCmd) InsecureSkipTLSVerify() *CaoCmd {
	return c.WithFlag("insecure-skip-tls-verify", "true")
}

func (c *CaoCmd) WithKubeconfig(kubeconfig string) *CaoCmd {
	return c.WithFlag("kubeconfig", kubeconfig)
}

func (c *CaoCmd) InNamespace(namespace string) *CaoCmd {
	return c.WithFlag("namespace", namespace)
}

func (c *CaoCmd) WithRequestTimeout(requestTimeout string) *CaoCmd {
	return c.WithFlag("request-timeout", requestTimeout)
}

func (c *CaoCmd) WithServer(kubernetesServerAddress string) *CaoCmd {
	return c.WithFlag("server", kubernetesServerAddress)
}

func (c *CaoCmd) WithTLSServerName(tlsServerName string) *CaoCmd {
	return c.WithFlag("tls-server-name", tlsServerName)
}

func (c *CaoCmd) WithToken(bearerToken string) *CaoCmd {
	return c.WithFlag("token", bearerToken)
}

func (c *CaoCmd) WithUser(kubeconfigUser string) *CaoCmd {
	return c.WithFlag("user", kubeconfigUser)
}

// =============================================
// =============== CAO Commands ===============
// =============================================

func Certify(archiveName, image, imagePullPolicy, registry, storageClass, timeout string,
	clean, ipv6, lpv, useFsGroup bool, collectedLogLevel, fsGroup, parallel int) *CaoCmd {
	args := []string{
		"--archive-name", archiveName,
		"--collected-log-level", fmt.Sprintf("%d", collectedLogLevel),
		"--fsgroup", fmt.Sprintf("%d", fsGroup),
		"--image", image,
		"--image-pull-policy", imagePullPolicy,
		"--parallel", fmt.Sprintf("%d", parallel),
		"--registry", registry,
		"--storage-class", storageClass,
		"--timeout", timeout,
	}
	if clean {
		args = append(args, "--clean")
	}

	if ipv6 {
		args = append(args, "--ipv6")
	}

	if lpv {
		args = append(args, "--lpv")
	}

	if useFsGroup {
		args = append(args, "--use-fsgroup")
	}

	return &CaoCmd{cmdutils.Cmd{RootCommand: caoRootCmd, Command: "certify", Args: args}}
}

func Collectlogs(all, collectInfo, collectInfoList, collectInfoRedact, system, uploadLogs bool, parallel, logLevel int,
	collectInfoCollect, couchbaseCluster, customer, directory, eventCollectorPort,
	operatorImage, operatorMetricsPort, operatorRestPort, serverImage, ticket, uploadHost, uploadProxy string) *CaoCmd {
	args := []string{
		"--collectinfo-collect", collectInfoCollect,
		"--couchbase-cluster", couchbaseCluster,
		"--customer", customer,
		"--directory", directory,
		"--event-collector-port", eventCollectorPort,
		"--log-level", fmt.Sprintf("%d", logLevel),
		"--operator-image", operatorImage,
		"--operator-metrics-port", operatorMetricsPort,
		"--operator-rest-port", operatorRestPort,
		"--parallel", fmt.Sprintf("%d", parallel),
		"--server-image", serverImage,
		"--ticket", ticket,
		"--upload-host", uploadHost,
		"--upload-proxy", uploadProxy,
	}
	if all {
		args = append(args, "--all")
	}

	if collectInfo {
		args = append(args, "--collectinfo")
	}

	if collectInfoList {
		args = append(args, "--collectinfo-list")
	}

	if collectInfoRedact {
		args = append(args, "--collectinfo-redact")
	}

	if system {
		args = append(args, "--system")
	}

	if uploadLogs {
		args = append(args, "--upload-logs")
	}

	return &CaoCmd{cmdutils.Cmd{RootCommand: caoRootCmd, Command: "collect-logs", Args: args}}
}

func Completion(shellCompletion string) *CaoCmd {
	args := []string{shellCompletion}
	return &CaoCmd{cmdutils.Cmd{RootCommand: caoRootCmd, Command: "completion", Args: args}}
}

func CreateAdmissionController(cpuLimit, cpuRequest, memoryLimit, memoryRequest, replicas int,
	image, imagePullPolicy, imagePullSecret, logLevel, scope string,
	validateSecrets, validateStorageClasses bool) *CaoCmd {
	args := []string{
		"admission",
		"--cpu-limit", fmt.Sprintf("%d", cpuLimit),
		"--cpu-request", fmt.Sprintf("%d", cpuRequest),
		"--image", image,
		"--image-pull-policy", imagePullPolicy,
		"--image-pull-secret", imagePullSecret,
		"--logLevel", logLevel,
		"--memory-limit", fmt.Sprintf("%d", memoryLimit),
		"--memory-request", fmt.Sprintf("%d", memoryRequest),
		"--replicas", fmt.Sprintf("%d", replicas),
		"--scope", scope,
	}
	if validateSecrets {
		args = append(args, "--validate-secrets")
	}

	if validateStorageClasses {
		args = append(args, "--validate-storage-classes")
	}

	return &CaoCmd{cmdutils.Cmd{RootCommand: caoRootCmd, Command: "create", Args: args}}
}

func CreateOperator(cpuLimit, cpuRequest, memoryLimit, memoryRequest int,
	image, imagePullPolicy, imagePullSecret, logLevel, scope, podCreationTimeout string,
	podDeleteDelay, podReadinessDelay, podReadinessPeriod string) *CaoCmd {
	args := []string{
		"operator",
		"--cpu-limit", fmt.Sprintf("%d", cpuLimit),
		"--cpu-request", fmt.Sprintf("%d", cpuRequest),
		"--image", image,
		"--image-pull-policy", imagePullPolicy,
		"--image-pull-secret", imagePullSecret,
		"--log-level", logLevel,
		"--memory-limit", fmt.Sprintf("%d", memoryLimit),
		"--memory-request", fmt.Sprintf("%d", memoryRequest),
		"--pod-creation-timeout", podCreationTimeout,
		"--pod-delete-delay", podDeleteDelay,
		"--pod-readiness-delay", podReadinessDelay,
		"--pod-readiness-period", podReadinessPeriod,
		"--scope", scope,
	}

	return &CaoCmd{cmdutils.Cmd{RootCommand: caoRootCmd, Command: "create", Args: args}}
}

func CreateBackup(iamRoleArn string) *CaoCmd {
	args := []string{
		"backup",
		"--iam-role-arn", iamRoleArn,
	}

	return &CaoCmd{cmdutils.Cmd{RootCommand: caoRootCmd, Command: "create", Args: args}}
}

func DeleteAdmissionController(scope string) *CaoCmd {
	args := []string{
		"admission",
		"--scope", scope,
	}

	return &CaoCmd{cmdutils.Cmd{RootCommand: caoRootCmd, Command: "delete", Args: args}}
}

func DeleteOperator(scope string) *CaoCmd {
	args := []string{
		"operator",
		"--scope", scope,
	}

	return &CaoCmd{cmdutils.Cmd{RootCommand: caoRootCmd, Command: "delete", Args: args}}
}

func DeleteBackup() *CaoCmd {
	args := []string{"backup"}
	return &CaoCmd{cmdutils.Cmd{RootCommand: caoRootCmd, Command: "delete", Args: args}}
}

func GenerateOperator(cpuLimit, cpuRequest, memoryLimit, memoryRequest int,
	image, imagePullPolicy, imagePullSecret, logLevel, scope, podCreationTimeout string,
	podDeleteDelay, podReadinessDelay, podReadinessPeriod string) *CaoCmd {
	args := []string{
		"operator",
		"--cpu-limit", fmt.Sprintf("%d", cpuLimit),
		"--cpu-request", fmt.Sprintf("%d", cpuRequest),
		"--image", image,
		"--image-pull-policy", imagePullPolicy,
		"--image-pull-secret", imagePullSecret,
		"--log-level", logLevel,
		"--memory-limit", fmt.Sprintf("%d", memoryLimit),
		"--memory-request", fmt.Sprintf("%d", memoryRequest),
		"--pod-creation-timeout", podCreationTimeout,
		"--pod-delete-delay", podDeleteDelay,
		"--pod-readiness-delay", podReadinessDelay,
		"--pod-readiness-period", podReadinessPeriod,
		"--scope", scope,
	}

	return &CaoCmd{cmdutils.Cmd{RootCommand: caoRootCmd, Command: "generate", Args: args}}
}

func GenerateAdmissionController(cpuLimit, cpuRequest, memoryLimit, memoryRequest, replicas int,
	image, imagePullPolicy, imagePullSecret, logLevel, scope string,
	validateSecrets, validateStorageClasses bool) *CaoCmd {
	args := []string{
		"admission",
		"--cpu-limit", fmt.Sprintf("%d", cpuLimit),
		"--cpu-request", fmt.Sprintf("%d", cpuRequest),
		"--image", image,
		"--image-pull-policy", imagePullPolicy,
		"--image-pull-secret", imagePullSecret,
		"--logLevel", logLevel,
		"--memory-limit", fmt.Sprintf("%d", memoryLimit),
		"--memory-request", fmt.Sprintf("%d", memoryRequest),
		"--replicas", fmt.Sprintf("%d", replicas),
		"--scope", scope,
	}
	if validateSecrets {
		args = append(args, "--validate-secrets")
	}

	if validateStorageClasses {
		args = append(args, "--validate-storage-classes")
	}

	return &CaoCmd{cmdutils.Cmd{RootCommand: caoRootCmd, Command: "generate", Args: args}}
}

func GenerateBackup(iamRoleArn string) *CaoCmd {
	args := []string{
		"backup",
		"--iam-role-arn", iamRoleArn,
	}

	return &CaoCmd{cmdutils.Cmd{RootCommand: caoRootCmd, Command: "generate", Args: args}}
}

func Restore(couchbaseCluster, fileName, path, strategy string) *CaoCmd {
	args := []string{
		"--couchbase-cluster", couchbaseCluster,
		"--filename", fileName,
		"--path", path,
		"--strategy", strategy,
	}

	return &CaoCmd{cmdutils.Cmd{RootCommand: caoRootCmd, Command: "restore", Args: args}}
}

func Save(couchbaseCluster, fileName, path string) *CaoCmd {
	args := []string{
		"--couchbase-cluster", couchbaseCluster,
		"--filename", fileName,
		"--path", path,
	}

	return &CaoCmd{cmdutils.Cmd{RootCommand: caoRootCmd, Command: "save", Args: args}}
}

func Update(scope string) *CaoCmd {
	args := []string{
		"webhook",
		"--scope", scope,
	}

	return &CaoCmd{cmdutils.Cmd{RootCommand: caoRootCmd, Command: "update", Args: args}}
}

func GetVersion() *CaoCmd {
	return &CaoCmd{cmdutils.Cmd{RootCommand: caoRootCmd, Command: "version"}}
}
