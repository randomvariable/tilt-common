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

//go:build mage

// Example magefile showing how to use the k3dgpu mage targets
// with functional options pattern.
//
// Usage:
//
//	mage setupGPUCluster              # Basic setup
//	mage setupGPUClusterCustom        # With all options
//	mage setupGPUClusterWithModelCache # With model caching
//	mage teardownGPUCluster           # Delete cluster
//	mage clusterStatus                # Check cluster status
package main

import (
	"fmt"

	k3dgpu "github.com/randomvariable/tilt-common/extensions/k3d-gpu"
)

const (
	clusterName = "example-gpu"
)

// SetupGPUCluster creates a k3d cluster with GPU support.
// Uses minimal configuration with just a custom cluster name.
func SetupGPUCluster() error {
	return k3dgpu.CreateCluster(
		k3dgpu.WithName(clusterName),
	)
}

// SetupGPUClusterCustom creates a k3d cluster with custom configuration.
// This example shows all available configuration options.
func SetupGPUClusterCustom() error {
	return k3dgpu.CreateCluster(
		k3dgpu.WithName(clusterName),
		k3dgpu.WithK3sVersion("v1.31.6+k3s1"),
		k3dgpu.WithAPIPort("6550"),
		k3dgpu.WithRegistryPort("5005"),
		k3dgpu.WithModelCache("", ""), // Uses defaults: ~/.cache/models → /var/lib/models
		k3dgpu.WithBuildArgs(map[string]string{
			// Custom build arguments (optional)
			// "CUDA_VERSION": "12.6.0",
			// "UBUNTU_VERSION": "22.04",
		}),
		k3dgpu.WithVerbose(true), // Show detailed output
	)
}

// SetupGPUClusterWithModelCache demonstrates the HuggingFace model cache pattern.
// This is the recommended setup for ML/AI workloads.
func SetupGPUClusterWithModelCache() error {
	return k3dgpu.CreateCluster(
		k3dgpu.WithName(clusterName),
		k3dgpu.WithModelCache("~/.cache/huggingface", "/var/lib/huggingface"),
		k3dgpu.WithVerbose(true),
	)
}

// SetupGPUClusterWithMultipleVolumes shows how to mount multiple volumes.
func SetupGPUClusterWithMultipleVolumes() error {
	return k3dgpu.CreateCluster(
		k3dgpu.WithName(clusterName),
		k3dgpu.WithModelCache("", ""),                           // Model cache
		k3dgpu.WithDataVolume("~/data", "/var/lib/data"),        // Data volume
		k3dgpu.WithVolumes("./config:/etc/app/config@server:0"), // Config files
	)
}

// TeardownGPUCluster deletes the GPU-enabled k3d cluster.
func TeardownGPUCluster() error {
	return k3dgpu.DeleteCluster(clusterName)
}

// ClusterStatus checks the status of the GPU cluster.
func ClusterStatus() error {
	status, err := k3dgpu.GetClusterStatus(clusterName)
	if err != nil {
		return err
	}

	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println("  Cluster Status")
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Printf("  Name:    %s\n", status.Name)
	fmt.Printf("  Exists:  %v\n", status.Exists)
	fmt.Printf("  Running: %v\n", status.Running)
	fmt.Printf("  Nodes:   %d\n", status.NodeCount)
	fmt.Printf("  Context: %s\n", status.Context)
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")

	if !status.Exists {
		fmt.Println("\nCluster does not exist. Create it with: mage setupGPUCluster")
	} else if !status.Running {
		fmt.Println("\nCluster exists but is not running.")
	} else {
		fmt.Println("\nCluster is ready!")
	}

	return nil
}
