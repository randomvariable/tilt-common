// Copyright 2026 Naadir Jeewa
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
//
// SPDX-License-Identifier: Apache-2.0

// Package k3dgpu provides mage targets for creating and managing k3d clusters
// with NVIDIA GPU support using the functional options pattern.
//
// Usage:
//
//	import k3dgpu "github.com/randomvariable/tilt-common/extensions/k3d-gpu/magefiles"
//
//	// Basic setup
//	func SetupGPUCluster() error {
//	    return k3dgpu.CreateCluster(
//	        k3dgpu.WithName("my-cluster"),
//	    )
//	}
//
//	// With model caching
//	func SetupWithModelCache() error {
//	    return k3dgpu.CreateCluster(
//	        k3dgpu.WithName("my-cluster"),
//	        k3dgpu.WithModelCache("", ""),  // Uses defaults
//	        k3dgpu.WithVerbose(true),
//	    )
//	}
package k3dgpu

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/distribution/reference"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

const (
	// DefaultClusterName is the default cluster name if not specified.
	DefaultClusterName = "gpu-dev"
	// DefaultK3sVersion is the default k3s version.
	DefaultK3sVersion = "v1.31.6+k3s1"
	// DefaultAPIPort is the default Kubernetes API port.
	DefaultAPIPort = "6550"
	// DefaultRegistryPort is the default local registry port.
	DefaultRegistryPort = "5005"
	// NodeReadyPollInterval is the interval for polling node readiness.
	NodeReadyPollInterval = 2 * time.Second
	// NodeReadyTimeout is the default timeout for waiting for nodes.
	NodeReadyTimeout = 120 * time.Second
	// DefaultDirPermissions is the default permission for created directories.
	DefaultDirPermissions = 0o750
	// DefaultArgsCapacity is the default capacity for k3d args slice.
	DefaultArgsCapacity = 15
	// ClusterDeleteTimeout is the timeout for waiting for cluster deletion.
	ClusterDeleteTimeout = 30 * time.Second
)

var (
	// ErrMissingTools is returned when required tools are not found.
	ErrMissingTools = errors.New("missing required tools")
	// ErrImageNotFound is returned when an image is not found and SkipBuild is true.
	ErrImageNotFound = errors.New("image not found and SkipBuild=true")
	// ErrClusterExists is returned when attempting to create an existing cluster.
	ErrClusterExists = errors.New("cluster already exists")
	// ErrClusterStillExists is returned when a cluster still exists after deletion.
	ErrClusterStillExists = errors.New("cluster still exists after deletion")
	// ErrCallerFailed is returned when runtime.Caller fails.
	ErrCallerFailed = errors.New("failed to get current file path")
	// ErrInvalidImageTag is returned when an image tag contains invalid characters.
	ErrInvalidImageTag = errors.New("invalid image tag format")
)

// clusterConfig defines configuration for a k3d GPU cluster.
type clusterConfig struct {
	name         string
	k3sVersion   string
	apiPort      string
	registryPort string
	imageTag     string
	skipBuild    bool
	volumes      []string
	buildArgs    map[string]string
	verbose      bool
}

// setDefaults applies default values to unset fields.
func (c *clusterConfig) setDefaults() {
	if c.name == "" {
		c.name = DefaultClusterName
	}

	if c.k3sVersion == "" {
		c.k3sVersion = DefaultK3sVersion
	}

	if c.apiPort == "" {
		c.apiPort = DefaultAPIPort
	}

	if c.registryPort == "" {
		c.registryPort = DefaultRegistryPort
	}

	if c.imageTag == "" {
		// Sanitize version for Docker tag (replace + with -)
		sanitizedVersion := strings.ReplaceAll(c.k3sVersion, "+", "-")
		c.imageTag = fmt.Sprintf("k3s-gpu:%s-local", sanitizedVersion)
	}
}

// Option configures a cluster.
type Option func(*clusterConfig)

// WithName sets the cluster name (default: "gpu-dev").
func WithName(name string) Option {
	return func(c *clusterConfig) {
		c.name = name
	}
}

// WithK3sVersion sets the k3s version (default: "v1.31.6+k3s1").
func WithK3sVersion(version string) Option {
	return func(c *clusterConfig) {
		c.k3sVersion = version
	}
}

// WithAPIPort sets the Kubernetes API port (default: "6550").
func WithAPIPort(port string) Option {
	return func(c *clusterConfig) {
		c.apiPort = port
	}
}

// WithRegistryPort sets the local registry port (default: "5005").
func WithRegistryPort(port string) Option {
	return func(c *clusterConfig) {
		c.registryPort = port
	}
}

// WithImageTag sets a custom Docker image tag.
// If not specified, uses "k3s-gpu:{version}-local".
func WithImageTag(tag string) Option {
	return func(c *clusterConfig) {
		c.imageTag = tag
	}
}

// WithSkipBuild skips building the custom image (uses existing).
func WithSkipBuild(skip bool) Option {
	return func(c *clusterConfig) {
		c.skipBuild = skip
	}
}

// WithVolumes adds volume mounts to the cluster.
// Format: "host_path:container_path@node_filter"
// Example: WithVolumes("/home/user/data:/data@server:0").
func WithVolumes(volumes ...string) Option {
	return func(c *clusterConfig) {
		c.volumes = append(c.volumes, volumes...)
	}
}

// WithBuildArgs adds custom Docker build arguments.
func WithBuildArgs(args map[string]string) Option {
	return func(c *clusterConfig) {
		if c.buildArgs == nil {
			c.buildArgs = make(map[string]string)
		}

		maps.Copy(c.buildArgs, args)
	}
}

// WithVerbose enables verbose output.
func WithVerbose(verbose bool) Option {
	return func(c *clusterConfig) {
		c.verbose = verbose
	}
}

// WithModelCache adds a model cache volume mount for persisting ML models.
// If hostDir is empty, uses ~/.cache/models.
// If containerPath is empty, uses /var/lib/models.
//
// Example:
//
//	WithModelCache("", "")  // Uses defaults
//	WithModelCache("~/.cache/huggingface", "/var/lib/huggingface")
func WithModelCache(hostDir, containerPath string) Option {
	return func(c *clusterConfig) {
		// Default paths
		if hostDir == "" {
			home, err := os.UserHomeDir()
			if err != nil {
				logWarnf("Failed to get home directory for model cache: %v", err)

				return
			}

			hostDir = filepath.Join(home, ".cache", "models")
		}

		if containerPath == "" {
			containerPath = "/var/lib/models"
		}

		// Expand ~ in hostDir
		if hostDir != "" && hostDir[0] == '~' {
			home, err := os.UserHomeDir()
			if err != nil {
				logWarnf("Failed to expand ~ in model cache path: %v", err)

				return
			}

			hostDir = filepath.Join(home, hostDir[1:])
		}

		// Create directory if it doesn't exist
		err := os.MkdirAll(hostDir, DefaultDirPermissions)
		if err != nil {
			logWarnf("Failed to create model cache directory %s: %v", hostDir, err)

			return
		}

		// Add volume mount
		volumeMount := fmt.Sprintf("%s:%s@server:0", hostDir, containerPath)
		c.volumes = append(c.volumes, volumeMount)

		if c.verbose {
			logInfof("Model cache: %s → %s", hostDir, containerPath)
		}
	}
}

// WithDataVolume adds a data volume mount for persisting application data.
// Creates the host directory if it doesn't exist.
//
// Example:
//
//	WithDataVolume("~/data", "/var/lib/data")
func WithDataVolume(hostDir, containerPath string) Option {
	return func(c *clusterConfig) {
		// Expand ~ in hostDir
		if hostDir != "" && hostDir[0] == '~' {
			home, err := os.UserHomeDir()
			if err != nil {
				logWarnf("Failed to expand ~ in data volume path: %v", err)

				return
			}

			hostDir = filepath.Join(home, hostDir[1:])
		}

		// Create directory if it doesn't exist
		err := os.MkdirAll(hostDir, DefaultDirPermissions)
		if err != nil {
			logWarnf("Failed to create data directory %s: %v", hostDir, err)

			return
		}

		// Add volume mount
		volumeMount := fmt.Sprintf("%s:%s@server:0", hostDir, containerPath)
		c.volumes = append(c.volumes, volumeMount)
	}
}

// getAssetsDir returns the path to the cluster assets directory.
func getAssetsDir() (string, error) {
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		return "", ErrCallerFailed
	}

	assetsDir := filepath.Join(filepath.Dir(filename), "..", "assets", "cluster")

	absPath, err := filepath.Abs(assetsDir)
	if err != nil {
		return "", fmt.Errorf("resolving assets path: %w", err)
	}

	_, err = os.Stat(absPath)
	if err != nil {
		return "", fmt.Errorf("assets directory not found: %s: %w", absPath, err)
	}

	return absPath, nil
}

// logInfof prints an info message.
func logInfof(format string, args ...any) {
	_, _ = fmt.Printf("\033[0;34m[INFO]\033[0m "+format+"\n", args...)
}

// logSuccessf prints a success message.
func logSuccessf(format string, args ...any) {
	_, _ = fmt.Printf("\033[0;32m[SUCCESS]\033[0m "+format+"\n", args...)
}

// logWarnf prints a warning message.
func logWarnf(format string, args ...any) {
	_, _ = fmt.Printf("\033[1;33m[WARN]\033[0m "+format+"\n", args...)
}

// checkPrerequisites validates that required tools are available.
func checkPrerequisites() error {
	required := []string{"docker", "k3d", "kubectl"}

	missing := make([]string, 0, len(required))

	for _, cmd := range required {
		_, err := exec.LookPath(cmd)
		if err != nil {
			missing = append(missing, cmd)
		}
	}

	if len(missing) > 0 {
		return fmt.Errorf("%w: %s", ErrMissingTools, strings.Join(missing, ", "))
	}

	_, err := exec.LookPath("nvidia-ctk")
	if err != nil {
		logWarnf("nvidia-ctk not found. GPU support may not work correctly.")
		logWarnf("Install nvidia-container-toolkit: https://docs.nvidia.com/datacenter/cloud-native/container-toolkit/install-guide.html")
	}

	return nil
}

// runCommand executes a command with streaming stdout/stderr.
func runCommand(ctx context.Context, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	err := cmd.Run()
	if err != nil {
		return fmt.Errorf("executing %s: %w", name, err)
	}

	return nil
}

// runCommandWithEnv executes a command with custom environment and streaming output.
func runCommandWithEnv(ctx context.Context, env []string, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)

	cmd.Env = append(os.Environ(), env...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	err := cmd.Run()
	if err != nil {
		return fmt.Errorf("executing %s: %w", name, err)
	}

	return nil
}

// getKubernetesClient creates a Kubernetes client for the current context.
func getKubernetesClient() (*kubernetes.Clientset, error) {
	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()

	config, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		loadingRules,
		&clientcmd.ConfigOverrides{},
	).ClientConfig()
	if err != nil {
		return nil, fmt.Errorf("loading kubeconfig: %w", err)
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("creating kubernetes client: %w", err)
	}

	return clientset, nil
}

// waitForNodesReady waits for all nodes in the cluster to become ready.
func waitForNodesReady(ctx context.Context, timeout time.Duration) error {
	clientset, err := getKubernetesClient()
	if err != nil {
		return err
	}

	logInfof("Waiting for nodes to be ready (timeout: %v)...", timeout)

	err = wait.PollUntilContextTimeout(ctx, NodeReadyPollInterval, timeout, true, func(ctx context.Context) (bool, error) {
		nodes, listErr := clientset.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
		if listErr != nil {
			// Cluster might not be ready yet, keep waiting.
			return false, nil //nolint:nilerr // intentional: transient errors during node startup are expected
		}

		if len(nodes.Items) == 0 {
			// No nodes registered yet, keep waiting
			return false, nil
		}

		// Check if all nodes are ready
		for i := range nodes.Items {
			node := &nodes.Items[i]
			ready := false

			for _, condition := range node.Status.Conditions {
				if condition.Type == corev1.NodeReady && condition.Status == corev1.ConditionTrue {
					ready = true

					break
				}
			}

			if !ready {
				// At least one node is not ready
				return false, nil
			}
		}

		// All nodes are ready
		logSuccessf("All %d node(s) ready", len(nodes.Items))

		return true, nil
	})
	if err != nil {
		return fmt.Errorf("waiting for nodes to be ready: %w", err)
	}

	return nil
}

// clusterExists checks if a k3d cluster with the given name exists.
func clusterExists(ctx context.Context, name string) (bool, error) {
	cmd := exec.CommandContext(ctx, "k3d", "cluster", "list", "-o", "json")

	output, err := cmd.Output()
	if err != nil {
		return false, fmt.Errorf("listing k3d clusters: %w", err)
	}

	var clusters []map[string]any

	err = json.Unmarshal(output, &clusters)
	if err != nil {
		return false, fmt.Errorf("parsing cluster list: %w", err)
	}

	for _, cluster := range clusters {
		if clusterName, ok := cluster["name"].(string); ok && clusterName == name {
			return true, nil
		}
	}

	return false, nil
}

// dockerImageInspectCmd creates a command to inspect a Docker image.
// The imageTag must already be validated before calling this function.
func dockerImageInspectCmd(ctx context.Context, imageTag string) *exec.Cmd {
	return exec.CommandContext(ctx, "docker", "image", "inspect", imageTag)
}

// buildImage builds the custom k3s GPU Docker image.
func buildImage(ctx context.Context, config *clusterConfig, assetsDir string) error {
	// Validate image reference format using the distribution/reference parser
	_, err := reference.ParseNormalizedNamed(config.imageTag)
	if err != nil {
		return fmt.Errorf("%w: %s: %w", ErrInvalidImageTag, config.imageTag, err)
	}

	if config.skipBuild {
		logInfof("Skipping image build (SkipBuild=true)")

		// Check image exists - imageTag validated above
		cmd := dockerImageInspectCmd(ctx, config.imageTag)
		cmd.Stdout = io.Discard
		cmd.Stderr = io.Discard

		err := cmd.Run()
		if err != nil {
			return fmt.Errorf("%w: %s", ErrImageNotFound, config.imageTag)
		}

		logInfof("Using existing image: %s", config.imageTag)

		return nil
	}

	logInfof("Building custom k3s GPU image: %s", config.imageTag)
	logInfof("k3s version: %s", config.k3sVersion)
	logInfof("Assets directory: %s", assetsDir)

	args := []string{
		"build",
		"--build-arg", "K3S_VERSION=" + config.k3sVersion,
		"-t", config.imageTag,
		"-f", filepath.Join(assetsDir, "Dockerfile"),
	}

	for key, value := range config.buildArgs {
		args = append(args, "--build-arg", fmt.Sprintf("%s=%s", key, value))
	}

	args = append(args, assetsDir)

	// Always stream output for Docker build
	err = runCommandWithEnv(ctx, []string{"DOCKER_BUILDKIT=1"}, "docker", args...)
	if err != nil {
		return fmt.Errorf("building image: %w", err)
	}

	logSuccessf("Image built successfully: %s", config.imageTag)

	return nil
}

// createCluster creates a k3d cluster with the custom GPU image.
func createK3dCluster(ctx context.Context, config *clusterConfig) error {
	registryName := config.name + "-registry"

	logInfof("Creating k3d cluster: %s", config.name)
	logInfof("API port: %s", config.apiPort)
	logInfof("Registry: %s:%s", registryName, config.registryPort)

	args := make([]string, 0, DefaultArgsCapacity+len(config.volumes)*2)
	args = append(args,
		"cluster", "create", config.name,
		"--api-port", config.apiPort,
		"--registry-create", fmt.Sprintf("%s:0.0.0.0:%s", registryName, config.registryPort),
		"--gpus=all",
		"--image="+config.imageTag,
		"--k3s-arg=--snapshotter=fuse-overlayfs@server:*",
		"--wait",
	)

	for _, volume := range config.volumes {
		args = append(args, "--volume", volume)
	}

	// Always stream output for k3d cluster creation
	err := runCommand(ctx, "k3d", args...)
	if err != nil {
		return fmt.Errorf("creating k3d cluster: %w", err)
	}

	logSuccessf("Cluster created successfully")

	return nil
}

// waitForCluster waits for the cluster to be ready.
func waitForCluster(ctx context.Context, name string) error {
	kubectlContext := "k3d-" + name

	logInfof("Switching kubectl context to: %s", kubectlContext)

	err := runCommand(ctx, "kubectl", "config", "use-context", kubectlContext)
	if err != nil {
		return fmt.Errorf("switching kubectl context: %w", err)
	}

	// Wait for nodes to be ready using Kubernetes client
	err = waitForNodesReady(ctx, NodeReadyTimeout)
	if err != nil {
		return fmt.Errorf("waiting for nodes: %w", err)
	}

	logInfof("Waiting for device plugin to be ready...")

	err = runCommand(ctx, "kubectl", "rollout", "status",
		"daemonset/nvidia-device-plugin-daemonset",
		"-n", "kube-system",
		"--timeout=120s")
	if err != nil {
		logWarnf("Device plugin not ready yet. This may be normal if GPUs are not available.")
	}

	return nil
}

// printSummary prints a summary of the created cluster.
func printSummary(writer io.Writer, config *clusterConfig) {
	kubectlContext := "k3d-" + config.name

	_, _ = fmt.Fprintln(writer)
	_, _ = fmt.Fprintln(writer, "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	_, _ = fmt.Fprintln(writer, "  k3d GPU Cluster Ready")
	_, _ = fmt.Fprintln(writer, "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	_, _ = fmt.Fprintln(writer)
	_, _ = fmt.Fprintf(writer, "  Cluster:   %s\n", config.name)
	_, _ = fmt.Fprintf(writer, "  Context:   %s\n", kubectlContext)
	_, _ = fmt.Fprintf(writer, "  API Port:  %s\n", config.apiPort)
	_, _ = fmt.Fprintf(writer, "  Registry:  localhost:%s\n", config.registryPort)
	_, _ = fmt.Fprintf(writer, "  Image:     %s\n", config.imageTag)
	_, _ = fmt.Fprintln(writer)
	_, _ = fmt.Fprintln(writer, "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	_, _ = fmt.Fprintln(writer)
	_, _ = fmt.Fprintln(writer, "Next steps:")
	_, _ = fmt.Fprintln(writer)
	_, _ = fmt.Fprintln(writer, "  1. Verify GPU resources:")
	_, _ = fmt.Fprintln(writer, "     kubectl get nodes -o json | jq '.items[].status.allocatable'")
	_, _ = fmt.Fprintln(writer)
	_, _ = fmt.Fprintln(writer, "  2. Create a Tiltfile with GPU support:")
	_, _ = fmt.Fprintln(writer, "     load('ext://tilt-common/k3d-gpu', 'enable_gpu_support')")
	_, _ = fmt.Fprintln(writer, "     enable_gpu_support()")
	_, _ = fmt.Fprintln(writer)
	_, _ = fmt.Fprintln(writer, "  3. Start Tilt:")
	_, _ = fmt.Fprintln(writer, "     tilt up")
	_, _ = fmt.Fprintln(writer)
}

// CreateCluster creates a k3d cluster with NVIDIA GPU support.
//
// Example:
//
//	err := k3dgpu.CreateCluster(
//	    k3dgpu.WithName("my-cluster"),
//	    k3dgpu.WithModelCache("", ""),  // Use defaults
//	    k3dgpu.WithVerbose(true),
//	)
func CreateCluster(opts ...Option) error {
	config := &clusterConfig{}

	// Apply options
	for _, opt := range opts {
		opt(config)
	}

	// Apply defaults
	config.setDefaults()

	ctx := context.Background()

	logInfof("k3d GPU Cluster Setup")

	_, _ = fmt.Println()

	err := checkPrerequisites()
	if err != nil {
		return err
	}

	exists, err := clusterExists(ctx, config.name)
	if err != nil {
		return err
	}

	if exists {
		logWarnf("Cluster '%s' already exists", config.name)
		logInfof("Delete it first with: k3d cluster delete %s", config.name)

		return ErrClusterExists
	}

	assetsDir, err := getAssetsDir()
	if err != nil {
		return err
	}

	err = buildImage(ctx, config, assetsDir)
	if err != nil {
		return err
	}

	err = createK3dCluster(ctx, config)
	if err != nil {
		return err
	}

	err = waitForCluster(ctx, config.name)
	if err != nil {
		return err
	}

	printSummary(os.Stdout, config)

	logSuccessf("Setup complete!")

	return nil
}

// DeleteCluster deletes a k3d GPU cluster.
func DeleteCluster(name string) error {
	if name == "" {
		name = DefaultClusterName
	}

	ctx := context.Background()

	exists, err := clusterExists(ctx, name)
	if err != nil {
		return err
	}

	if !exists {
		logWarnf("Cluster '%s' does not exist", name)

		return nil
	}

	logInfof("Deleting k3d cluster: %s", name)

	err = runCommand(ctx, "k3d", "cluster", "delete", name)
	if err != nil {
		return fmt.Errorf("deleting cluster: %w", err)
	}

	err = wait.PollUntilContextTimeout(
		ctx,
		1*time.Second,
		ClusterDeleteTimeout,
		true,
		func(ctx context.Context) (bool, error) {
			exists, err := clusterExists(ctx, name)
			if err != nil {
				return false, err
			}

			return !exists, nil
		},
	)
	if err != nil {
		return fmt.Errorf("%w: %s: %w", ErrClusterStillExists, name, err)
	}

	logSuccessf("Cluster deleted successfully")

	return nil
}

// ClusterStatus returns information about a k3d GPU cluster.
type ClusterStatus struct {
	Name      string
	Exists    bool
	Running   bool
	NodeCount int
	Context   string
}

// GetClusterStatus returns the status of a k3d GPU cluster.
func GetClusterStatus(name string) (*ClusterStatus, error) {
	if name == "" {
		name = DefaultClusterName
	}

	status := &ClusterStatus{
		Name:    name,
		Context: "k3d-" + name,
	}

	ctx := context.Background()

	exists, err := clusterExists(ctx, name)
	if err != nil {
		return nil, err
	}

	status.Exists = exists

	if !exists {
		return status, nil
	}

	cmd := exec.CommandContext(ctx, "k3d", "cluster", "list", "-o", "json")

	output, err := cmd.Output()
	if err != nil {
		return status, fmt.Errorf("getting cluster info: %w", err)
	}

	var clusters []map[string]any

	err = json.Unmarshal(output, &clusters)
	if err != nil {
		return status, fmt.Errorf("parsing cluster info: %w", err)
	}

	for _, cluster := range clusters {
		if clusterName, ok := cluster["name"].(string); ok && clusterName == name {
			if serversRunning, ok := cluster["serversRunning"].(float64); ok {
				status.Running = serversRunning > 0
			}

			if serversCount, ok := cluster["serversCount"].(float64); ok {
				status.NodeCount = int(serversCount)
			}

			if agentsCount, ok := cluster["agentsCount"].(float64); ok {
				status.NodeCount += int(agentsCount)
			}

			break
		}
	}

	return status, nil
}
