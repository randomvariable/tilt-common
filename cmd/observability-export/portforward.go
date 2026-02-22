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

package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"

	portforward "github.com/microcumulus/k8s-portforward-conn"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// portForwardRoute maps a service name to the port the export CLI should
// connect to on that service.
type portForwardRoute struct {
	serviceName string
	port        int
}

// portForwardDialer routes HTTP connections to cluster pods via the Kubernetes
// port-forward API. Each call to DialContext opens a fresh port-forward
// connection to the first ready pod backing the requested service.
type portForwardDialer struct {
	config    *rest.Config
	clientset *kubernetes.Clientset
	namespace string
	// routes maps the URL hostname used by the CLI to a service+port pair.
	routes map[string]portForwardRoute
}

// DialContext implements the DialContext signature expected by http.Transport.
// The addr parameter is host:port from the URL; only the host is used for
// routing — the port comes from the registered route so it matches the
// container port regardless of what URL port was specified.
func (d *portForwardDialer) DialContext(ctx context.Context, _, addr string) (net.Conn, error) {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, fmt.Errorf("parsing dial address %q: %w", addr, err)
	}

	route, ok := d.routes[host]
	if !ok {
		return nil, fmt.Errorf("no port-forward route registered for host %q", host)
	}

	svc, err := d.clientset.CoreV1().Services(d.namespace).Get(ctx, route.serviceName, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("getting service %q: %w", route.serviceName, err)
	}

	if len(svc.Spec.Selector) == 0 {
		return nil, fmt.Errorf("service %q has no pod selector", route.serviceName)
	}

	selector := metav1.FormatLabelSelector(&metav1.LabelSelector{
		MatchLabels: svc.Spec.Selector,
	})

	pods, err := d.clientset.CoreV1().Pods(d.namespace).List(ctx, metav1.ListOptions{
		LabelSelector: selector,
		Limit:         1,
	})
	if err != nil {
		return nil, fmt.Errorf("listing pods for service %q: %w", route.serviceName, err)
	}

	if len(pods.Items) == 0 {
		return nil, fmt.Errorf("no pods found for service %q (selector: %s)", route.serviceName, selector)
	}

	conn, err := portforward.Forward(ctx, d.config, pods.Items[0], strconv.Itoa(route.port))
	if err != nil {
		return nil, fmt.Errorf("port-forward to %s: %w", route.serviceName, err)
	}

	return conn, nil
}

// SetupPortForward configures a port-forward HTTP client in opts and updates
// the URL fields to use service names as hostnames. It loads the kubeconfig
// from kubeconfigPath (empty string auto-detects ~/.kube/config or in-cluster
// config). Returns a no-op cleanup function and an error if setup fails.
func SetupPortForward(ctx context.Context, kubeconfigPath, namespace string, opts *ExportOptions) (func(), error) {
	cfg, err := buildKubeConfig(kubeconfigPath)
	if err != nil {
		return nil, fmt.Errorf("loading kubeconfig: %w", err)
	}

	clientset, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("creating kubernetes client: %w", err)
	}

	// Verify cluster connectivity before configuring routes.
	if _, err := clientset.CoreV1().Namespaces().Get(ctx, namespace, metav1.GetOptions{}); err != nil {
		return nil, fmt.Errorf("connecting to cluster (namespace %q): %w", namespace, err)
	}

	dialer := &portForwardDialer{
		config:    cfg,
		clientset: clientset,
		namespace: namespace,
		routes:    make(map[string]portForwardRoute),
	}

	if !opts.SkipMetrics {
		dialer.routes["victoriametrics"] = portForwardRoute{"victoriametrics", 8428}
		opts.MetricsURL = "http://victoriametrics:8428"
	}

	if !opts.SkipTraces {
		dialer.routes["victoriatraces"] = portForwardRoute{"victoriatraces", 10428}
		opts.TracesURL = "http://victoriatraces:10428"
	}

	if !opts.SkipLogs {
		dialer.routes["victorialogs"] = portForwardRoute{"victorialogs", 9428}
		opts.LogsURL = "http://victorialogs:9428"
	}

	if !opts.SkipProfiles {
		dialer.routes["parca-server"] = portForwardRoute{"parca-server", 7070}
		opts.ParcaURL = "http://parca-server:7070"
	}

	opts.HTTPClient = &http.Client{
		Transport: &http.Transport{
			DialContext:       dialer.DialContext,
			DisableKeepAlives: true, // Each request needs a fresh port-forward connection.
		},
	}

	return func() {}, nil
}

// buildKubeConfig loads the REST config from the given kubeconfig path.
// If path is empty it falls back to KUBECONFIG env var, then ~/.kube/config,
// and finally in-cluster configuration.
func buildKubeConfig(kubeconfigPath string) (*rest.Config, error) {
	if kubeconfigPath == "" {
		if env := os.Getenv("KUBECONFIG"); env != "" {
			kubeconfigPath = env
		} else {
			kubeconfigPath = filepath.Join(os.Getenv("HOME"), ".kube", "config")
		}
	}

	cfg, err := clientcmd.BuildConfigFromFlags("", kubeconfigPath)
	if err != nil {
		// Fall back to in-cluster config (running inside a pod).
		inCluster, inErr := rest.InClusterConfig()
		if inErr != nil {
			return nil, fmt.Errorf("kubeconfig %q failed (%v); in-cluster config also failed: %w", kubeconfigPath, err, inErr)
		}

		return inCluster, nil
	}

	return cfg, nil
}
