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

//go:build e2e

// DevSkim: ignore DS173237

package observability_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	_ "github.com/marcboeker/go-duckdb"
	portforward "github.com/microcumulus/k8s-portforward-conn"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

var _ = Describe("Observability Stack E2E", Ordered, func() {
	var (
		clientset     *kubernetes.Clientset
		dynamicClient dynamic.Interface
		restConfig    *rest.Config
		namespace     = "observability"
		ctx           context.Context
		cancel        context.CancelFunc
	)

	BeforeAll(func() {
		ctx, cancel = context.WithTimeout(context.Background(), 10*time.Minute)

		// Setup Kubernetes clients
		kubeconfig := filepath.Join(os.Getenv("HOME"), ".kube", "config")
		var err error
		restConfig, err = clientcmd.BuildConfigFromFlags("", kubeconfig)
		Expect(err).NotTo(HaveOccurred(), "failed to load kubeconfig")

		clientset, err = kubernetes.NewForConfig(restConfig)
		Expect(err).NotTo(HaveOccurred(), "failed to create kubernetes clientset")

		dynamicClient, err = dynamic.NewForConfig(restConfig)
		Expect(err).NotTo(HaveOccurred(), "failed to create dynamic client")

		// Verify cluster is accessible
		_, err = clientset.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
		Expect(err).NotTo(HaveOccurred(), "cluster not accessible")
	})

	AfterAll(func() {
		if cancel != nil {
			cancel()
		}
	})

	Describe("Infrastructure Health", func() {
		It("should have VictoriaMetrics ready", func() {
			Eventually(func() bool {
				return isDeploymentReady(ctx, clientset, namespace, "victoriametrics")
			}, "120s", "5s").Should(BeTrue(), "VictoriaMetrics should be ready")
		})

		It("should have VictoriaLogs ready", func() {
			Eventually(func() bool {
				return isDeploymentReady(ctx, clientset, namespace, "victorialogs")
			}, "120s", "5s").Should(BeTrue(), "VictoriaLogs should be ready")
		})

		It("should have VictoriaTraces ready", func() {
			Eventually(func() bool {
				return isDeploymentReady(ctx, clientset, namespace, "victoriatraces")
			}, "120s", "5s").Should(BeTrue(), "VictoriaTraces should be ready")
		})

		It("should have Vector DaemonSet running on all nodes", func() {
			Eventually(func() bool {
				return isDaemonSetReady(ctx, clientset, namespace, "vector")
			}, "120s", "5s").Should(BeTrue(), "Vector should be running on all nodes")
		})

		It("should have Grafana ready", func() {
			Eventually(func() bool {
				return isPodReadyByLabel(ctx, clientset, namespace, "app=grafana")
			}, "180s", "5s").Should(BeTrue(), "Grafana should be ready")
		})
	})

	Describe("Metrics Collection", func() {
		var metricsURL string
		var metricsClient *http.Client

		BeforeEach(func() {
			// Port-forward VictoriaMetrics
			metricsURL, metricsClient = setupPortForward(ctx, restConfig, clientset, namespace, "victoriametrics", 8428)
		})

		It("should accept metrics via import API", func() {
			// Use VictoriaMetrics import API (simpler than OTLP protobuf for testing)
			// Format: metric_name{labels} value timestamp_ms
			timestamp := time.Now().Unix()
			metricData := fmt.Sprintf("test_counter{service=\"e2e-test\"} 42 %d\n", timestamp*1000)

			resp, err := metricsClient.Post(
				fmt.Sprintf("%s/api/v1/import/prometheus", metricsURL),
				"text/plain",
				strings.NewReader(metricData),
			)
			Expect(err).NotTo(HaveOccurred())
			if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
				body, _ := io.ReadAll(resp.Body)
				GinkgoWriter.Printf("Import failed: status=%d, body=%s\n", resp.StatusCode, string(body))
			}
			Expect(resp.StatusCode).To(Or(Equal(http.StatusOK), Equal(http.StatusNoContent)))
			resp.Body.Close()

			// Wait a bit for indexing
			time.Sleep(2 * time.Second)

			// Query metric back
			Eventually(func() bool {
				resp, err := metricsClient.Get(fmt.Sprintf("%s/api/v1/query?query=test_counter", metricsURL))
				if err != nil {
					GinkgoWriter.Printf("Query error: %v\n", err)

					return false
				}
				defer resp.Body.Close()

				body, _ := io.ReadAll(resp.Body)
				var result map[string]any
				if err := json.Unmarshal(body, &result); err != nil {
					GinkgoWriter.Printf("JSON parse error: %v, body: %s\n", err, string(body))

					return false
				}

				data, ok := result["data"].(map[string]any)
				if !ok {
					GinkgoWriter.Printf("No data field in response: %+v\n", result)

					return false
				}
				results, ok := data["result"].([]any)
				if !ok || len(results) == 0 {
					GinkgoWriter.Printf("No results in data: %+v\n", data)

					return false
				}

				return true
			}, "60s", "2s").Should(BeTrue(), "metric should be queryable")
		})

		It("should scrape Prometheus annotations", func() {
			// Deploy test app with prometheus.io/scrape annotation
			deployTestApp(ctx, clientset, namespace, "annotated-app", true, false)
			defer deleteTestApp(ctx, clientset, namespace, "annotated-app")

			// Wait for pod to be ready
			Eventually(func() bool {
				return isPodReadyByLabel(ctx, clientset, namespace, "app=annotated-app")
			}, "60s", "2s").Should(BeTrue(), "annotated app should be ready")

			// Wait for VMAgent to scrape
			time.Sleep(45 * time.Second)

			// First check what targets VMAgent has discovered
			vmagentURL, vmagentClient := setupPortForward(ctx, restConfig, clientset, namespace, "vmagent", 8429)
			targetsResp, err := vmagentClient.Get(fmt.Sprintf("%s/api/v1/targets", vmagentURL))
			if err == nil {
				defer targetsResp.Body.Close()
				targetsBody, _ := io.ReadAll(targetsResp.Body)
				GinkgoWriter.Printf("VMAgent targets: %s\n", string(targetsBody))
			}

			// Verify the test_metric from our app exists
			Eventually(func() bool {
				resp, err := metricsClient.Get(fmt.Sprintf("%s/api/v1/query?query=test_metric", metricsURL))
				if err != nil {
					GinkgoWriter.Printf("Query error: %v\n", err)

					return false
				}
				defer resp.Body.Close()

				body, _ := io.ReadAll(resp.Body)
				var result map[string]any
				json.Unmarshal(body, &result)

				GinkgoWriter.Printf("Query result: %s\n", string(body))

				data, _ := result["data"].(map[string]any)
				results, _ := data["result"].([]any)

				return len(results) > 0
			}, "60s", "5s").Should(BeTrue(), "annotated metrics should be scraped")
		})
	})

	Describe("Logs Collection", func() {
		var logsURL string
		var logsClient *http.Client

		BeforeEach(func() {
			// Port-forward VictoriaLogs
			logsURL, logsClient = setupPortForward(ctx, restConfig, clientset, namespace, "victorialogs", 9428)
		})

		It("should collect container logs via Vector", func() {
			// Deploy test app that produces logs
			deployTestApp(ctx, clientset, namespace, "logging-app", false, false)
			defer deleteTestApp(ctx, clientset, namespace, "logging-app")

			// Wait for logs to be collected
			time.Sleep(20 * time.Second)

			// Query logs
			Eventually(func() bool {
				resp, err := logsClient.Get(fmt.Sprintf("%s/select/logsql/query?query=_stream:{pod=~\"logging-app.*\"}", logsURL))
				if err != nil {
					return false
				}
				defer resp.Body.Close()

				body, _ := io.ReadAll(resp.Body)

				return strings.Contains(string(body), "logging-app")
			}, "60s", "5s").Should(BeTrue(), "container logs should be collected")
		})

		It("should accept logs via JSON API", func() {
			// Use VictoriaLogs JSON API (simpler than OTLP protobuf for testing)
			logTime := time.Now().Format(time.RFC3339Nano)
			logData := fmt.Sprintf(`{"_time":"%s","_msg":"test log message","service":"e2e-test","level":"info"}`, logTime)

			resp, err := logsClient.Post(
				fmt.Sprintf("%s/insert/jsonline", logsURL),
				"application/x-ndjson",
				strings.NewReader(logData+"\n"),
			)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode).To(Or(Equal(http.StatusOK), Equal(http.StatusNoContent)))
			resp.Body.Close()
		})
	})

	Describe("Traces Collection", func() {
		var tracesURL string
		var tracesClient *http.Client
		var queryURL string
		var queryClient *http.Client

		BeforeEach(func() {
			// Port-forward VictoriaTraces HTTP endpoint (supports both OTLP and Jaeger Query API)
			tracesURL, tracesClient = setupPortForward(ctx, restConfig, clientset, namespace, "victoriatraces", 10428)
			queryURL = tracesURL
			queryClient = tracesClient
		})

		It("should accept traces via OTLP HTTP", func() {
			now := time.Now()
			traceID := "0123456789abcdef0123456789abcdef" // DevSkim: ignore DS173237 - synthetic test trace ID
			spanID := "0123456789abcdef"                  // DevSkim: ignore DS173237 - synthetic test span ID

			traceData := fmt.Sprintf(`{
				"resourceSpans": [{
					"resource": {
						"attributes": [{
							"key": "service.name",
							"value": {"stringValue": "e2e-test"}
						}]
					},
					"scopeSpans": [{
						"spans": [{
							"traceId": %q,
							"spanId": %q,
							"name": "test-span",
							"startTimeUnixNano": "%d",
							"endTimeUnixNano": "%d",
							"kind": 1
						}]
					}]
				}]
			}`, traceID, spanID, now.UnixNano(), now.Add(time.Millisecond).UnixNano())

			resp, err := tracesClient.Post(
				fmt.Sprintf("%s/opentelemetry/v1/traces", tracesURL),
				"application/json",
				strings.NewReader(traceData),
			)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode).To(Or(Equal(http.StatusOK), Equal(http.StatusAccepted), Equal(http.StatusNoContent)))
			resp.Body.Close()
		})

		It("should query traces via Jaeger API", func() {
			// Query traces via Jaeger Query API on port 10428
			Eventually(func() error {
				resp, err := queryClient.Get(fmt.Sprintf("%s/select/jaeger/api/traces?service=e2e-test&limit=10", queryURL))
				if err != nil {
					return err
				}
				defer resp.Body.Close()

				if resp.StatusCode != http.StatusOK {
					return fmt.Errorf("unexpected status: %d", resp.StatusCode)
				}

				return nil
			}, "30s", "5s").Should(Succeed(), "traces should be queryable")
		})
	})

	Describe("OTel Env Var Injection", func() {
		It("should inject OTLP environment variables into annotated pods", func() {
			// Deploy app with OTel injection annotation
			deployTestApp(ctx, clientset, namespace, "otel-injected-app", false, true)
			defer deleteTestApp(ctx, clientset, namespace, "otel-injected-app")

			// Wait for pod to be ready
			Eventually(func() bool {
				return isPodReadyByLabel(ctx, clientset, namespace, "app=otel-injected-app")
			}, "120s", "5s").Should(BeTrue())

			// Get pod and check env vars
			pods, err := clientset.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
				LabelSelector: "app=otel-injected-app",
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(pods.Items).NotTo(BeEmpty())

			pod := pods.Items[0]
			envVars := make(map[string]string)
			for _, container := range pod.Spec.Containers {
				for _, env := range container.Env {
					envVars[env.Name] = env.Value
				}
			}

			// Verify OTLP env vars are present
			Expect(envVars).To(HaveKey("OTEL_EXPORTER_OTLP_METRICS_ENDPOINT"))
			Expect(envVars).To(HaveKey("OTEL_EXPORTER_OTLP_LOGS_ENDPOINT"))
			Expect(envVars).To(HaveKey("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT"))
		})
	})

	Describe("Grafana Dashboards", func() {
		var grafanaURL string
		var grafanaClient *http.Client

		BeforeEach(func() {
			// Port-forward Grafana
			grafanaURL, grafanaClient = setupPortForward(ctx, restConfig, clientset, namespace, "grafana-service", 3000)
		})

		It("should have Grafana accessible", func() {
			Eventually(func() error {
				resp, err := grafanaClient.Get(fmt.Sprintf("%s/api/health", grafanaURL))
				if err != nil {
					return err
				}
				defer resp.Body.Close()

				if resp.StatusCode != http.StatusOK {
					return fmt.Errorf("unexpected status: %d", resp.StatusCode)
				}

				return nil
			}, "60s", "5s").Should(Succeed(), "Grafana should be accessible")
		})

		It("should have datasources configured", func() {
			Eventually(func() int {
				req, _ := http.NewRequest(http.MethodGet, fmt.Sprintf("%s/api/datasources", grafanaURL), nil)
				req.SetBasicAuth("admin", "admin")

				resp, err := grafanaClient.Do(req)
				if err != nil {
					return 0
				}
				defer resp.Body.Close()

				var datasources []map[string]any
				body, _ := io.ReadAll(resp.Body)
				json.Unmarshal(body, &datasources)

				return len(datasources)
			}, "120s", "5s").Should(BeNumerically(">=", 3), "should have at least 3 datasources")
		})
	})

	Describe("Profiling with Parca", func() {
		var parcaURL string
		var parcaClient *http.Client

		BeforeEach(func() {
			// Port-forward Parca
			parcaURL, parcaClient = setupPortForward(ctx, restConfig, clientset, namespace, "parca-server", 7070)
		})

		It("should have Parca server accessible", func() {
			Eventually(func() error {
				resp, err := parcaClient.Get(fmt.Sprintf("%s/api/v1alpha1/labels", parcaURL))
				if err != nil {
					return err
				}
				defer resp.Body.Close()

				if resp.StatusCode != http.StatusOK {
					return fmt.Errorf("unexpected status: %d", resp.StatusCode)
				}

				return nil
			}, "60s", "5s").Should(Succeed(), "Parca should be accessible")
		})
	})

	Describe("VMAgent Prometheus CRDs", func() {
		It("should support ServiceMonitor CRD", func() {
			// Create ServiceMonitor
			serviceMonitor := &unstructured.Unstructured{
				Object: map[string]any{
					"apiVersion": "monitoring.coreos.com/v1",
					"kind":       "ServiceMonitor",
					"metadata": map[string]any{
						"name":      "test-servicemonitor",
						"namespace": namespace,
					},
					"spec": map[string]any{
						"selector": map[string]any{
							"matchLabels": map[string]any{
								"app": "test",
							},
						},
						"endpoints": []any{
							map[string]any{
								"port": "metrics",
							},
						},
					},
				},
			}

			gvr := schema.GroupVersionResource{
				Group:    "monitoring.coreos.com",
				Version:  "v1",
				Resource: "servicemonitors",
			}

			_, err := dynamicClient.Resource(gvr).Namespace(namespace).Create(
				ctx, serviceMonitor, metav1.CreateOptions{},
			)
			Expect(err).NotTo(HaveOccurred(), "should create ServiceMonitor CRD")

			// Cleanup
			defer dynamicClient.Resource(gvr).Namespace(namespace).Delete(
				ctx, "test-servicemonitor", metav1.DeleteOptions{},
			)
		})
	})

	Describe("Data Export", func() {
		var (
			metricsURL    string
			metricsClient *http.Client
			logsURL       string
			logsClient    *http.Client
			tracesURL     string
			tracesClient  *http.Client
			exportDir     string
		)

		BeforeEach(func() {
			// Setup port-forwards for all backends
			metricsURL, metricsClient = setupPortForward(ctx, restConfig, clientset, namespace, "victoriametrics", 8428)
			logsURL, logsClient = setupPortForward(ctx, restConfig, clientset, namespace, "victorialogs", 9428)
			tracesURL, tracesClient = setupPortForward(ctx, restConfig, clientset, namespace, "victoriatraces", 10428)

			// Create temporary export directory
			var err error
			exportDir, err = os.MkdirTemp("", "observability-export-*")
			Expect(err).NotTo(HaveOccurred())
		})

		AfterEach(func() {
			// Cleanup export directory
			if exportDir != "" {
				os.RemoveAll(exportDir)
			}
		})

		It("should export metrics, logs, and traces to DuckDB", func() {
			// Send test data to each backend using simpler APIs
			// Metrics via Prometheus format
			timestamp := time.Now().Unix()
			metricData := fmt.Sprintf("export_test_counter{service=\"export-test\"} 123 %d", timestamp*1000)
			resp, err := metricsClient.Post(
				fmt.Sprintf("%s/api/v1/import/prometheus", metricsURL),
				"text/plain",
				strings.NewReader(metricData),
			)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode).To(Or(Equal(http.StatusOK), Equal(http.StatusNoContent)))
			resp.Body.Close()

			// Logs via JSON API
			logTime := time.Now().Format(time.RFC3339Nano)
			logData := fmt.Sprintf(`{"_time":"%s","_msg":"export test log message","service":"export-test","level":"info"}`, logTime)
			resp, err = logsClient.Post(
				fmt.Sprintf("%s/insert/jsonline", logsURL),
				"application/x-ndjson",
				strings.NewReader(logData+"\n"),
			)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode).To(Or(Equal(http.StatusOK), Equal(http.StatusNoContent)))
			resp.Body.Close()

			// Traces via OTLP HTTP JSON
			now := time.Now()
			exportTraceID := "abcdef0123456789abcdef0123456789" // DevSkim: ignore DS173237 - synthetic test trace ID
			exportSpanID := "abcdef0123456789"                  // DevSkim: ignore DS173237 - synthetic test span ID
			traceData := fmt.Sprintf(`{
				"resourceSpans": [{
					"resource": {
						"attributes": [{
							"key": "service.name",
							"value": {"stringValue": "export-test"}
						}]
					},
					"scopeSpans": [{
						"spans": [{
							"traceId": %q,
							"spanId": %q,
							"name": "export-test-span",
							"startTimeUnixNano": "%d",
							"endTimeUnixNano": "%d",
							"kind": 1
						}]
					}]
				}]
			}`, exportTraceID, exportSpanID, now.UnixNano(), now.Add(time.Millisecond).UnixNano())
			resp, err = tracesClient.Post(
				fmt.Sprintf("%s/opentelemetry/v1/traces", tracesURL),
				"application/json",
				strings.NewReader(traceData),
			)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode).To(Or(Equal(http.StatusOK), Equal(http.StatusAccepted), Equal(http.StatusNoContent)))
			resp.Body.Close()

			// Wait for data to be indexed
			time.Sleep(5 * time.Second)

			// Run export CLI
			exportBinary := filepath.Join(projectRoot(), "bin", "observability-export")
			cmd := exec.CommandContext(ctx, exportBinary,
				"--output-dir", exportDir,
				"--metrics-url", metricsURL,
				"--logs-url", logsURL,
				"--traces-url", tracesURL,
				"--skip-profiles",
			)
			output, err := cmd.CombinedOutput()
			if err != nil {
				GinkgoWriter.Printf("Export command failed: %v\nOutput: %s\n", err, string(output))
			}
			Expect(err).NotTo(HaveOccurred(), "export command should succeed")

			// Verify DuckDB file exists
			files, err := filepath.Glob(filepath.Join(exportDir, "telemetry-*.duckdb"))
			Expect(err).NotTo(HaveOccurred())
			Expect(files).To(HaveLen(1), "should have one DuckDB export file")

			dbPath := files[0]

			// Open DuckDB and verify data
			db, err := sql.Open("duckdb", dbPath)
			Expect(err).NotTo(HaveOccurred())
			defer db.Close()

			// Verify metrics table has data
			var metricCount int
			err = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM metrics WHERE metric_name = 'export_test_counter'").Scan(&metricCount)
			Expect(err).NotTo(HaveOccurred())
			Expect(metricCount).To(BeNumerically(">", 0), "metrics table should have exported data")

			// Verify logs table has data
			var logCount int
			err = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM logs WHERE message LIKE '%export test log%'").Scan(&logCount)
			Expect(err).NotTo(HaveOccurred())
			Expect(logCount).To(BeNumerically(">", 0), "logs table should have exported data")

			// Verify spans table has data
			var spanCount int
			err = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM spans WHERE operation = 'export-test-span'").Scan(&spanCount)
			Expect(err).NotTo(HaveOccurred())
			Expect(spanCount).To(BeNumerically(">", 0), "spans table should have exported data")
		})
	})
})

// Helper functions

func isDeploymentReady(ctx context.Context, client *kubernetes.Clientset, namespace, name string) bool {
	deployment, err := client.AppsV1().Deployments(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return false
	}

	return deployment.Status.ReadyReplicas > 0
}

func isDaemonSetReady(ctx context.Context, client *kubernetes.Clientset, namespace, name string) bool {
	ds, err := client.AppsV1().DaemonSets(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return false
	}

	return ds.Status.NumberReady > 0 && ds.Status.NumberReady == ds.Status.DesiredNumberScheduled
}

func isPodReadyByLabel(ctx context.Context, client *kubernetes.Clientset, namespace, labelSelector string) bool {
	pods, err := client.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: labelSelector,
	})
	if err != nil || len(pods.Items) == 0 {
		return false
	}

	for _, pod := range pods.Items {
		for _, condition := range pod.Status.Conditions {
			if condition.Type == "Ready" && condition.Status == "True" {
				return true
			}
		}
	}

	return false
}

// setupPortForward creates an HTTP client configured to connect to a Kubernetes
// service via port-forward, without allocating local listening ports.
// Returns a fake URL and an HTTP client that routes requests through port-forward.
func setupPortForward(ctx context.Context, config *rest.Config, clientset *kubernetes.Clientset, namespace, serviceName string, port int) (string, *http.Client) {
	// Get the service to find the target pod
	svc, err := clientset.CoreV1().Services(namespace).Get(ctx, serviceName, metav1.GetOptions{})
	if err != nil {
		// Fallback to direct service URL with default client
		return fmt.Sprintf("http://%s.%s.svc.cluster.local:%d", serviceName, namespace, port), http.DefaultClient // DevSkim: ignore DS137138 - just a test
	}

	// Find a pod matching the service selector
	labelSelector := metav1.FormatLabelSelector(&metav1.LabelSelector{
		MatchLabels: svc.Spec.Selector,
	})
	pods, err := clientset.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: labelSelector,
		Limit:         1,
	})
	if err != nil || len(pods.Items) == 0 {
		// Fallback to direct service URL with default client
		return fmt.Sprintf("http://%s.%s.svc.cluster.local:%d", serviceName, namespace, port), http.DefaultClient // DevSkim: ignore DS137138 - just a test
	}

	pod := pods.Items[0]

	// Create a custom dialer that uses k8s-portforward-conn
	// This creates direct connections through port-forward without local listening ports
	dialer := func(ctx context.Context, _, _ string) (conn io.ReadWriteCloser, err error) {
		// Forward creates a connection to the pod via port-forward
		fwdConn, err := portforward.Forward(ctx, config, pod, fmt.Sprintf("%d", port))
		if err != nil {
			return nil, fmt.Errorf("port-forward failed: %w", err)
		}

		return fwdConn, nil
	}

	// Create an HTTP client with the port-forward dialer
	client := &http.Client{
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, network, addr string) (conn net.Conn, err error) {
				// Use our port-forward dialer
				rwc, err := dialer(ctx, network, addr)
				if err != nil {
					return nil, err
				}
				// FwdConn implements net.Conn, so we can return it directly
				return rwc.(net.Conn), nil
			},
		},
		Timeout: 30 * time.Second,
	}

	// Return a fake URL (the dialer handles routing) and the configured client
	// The URL is just for test readability - actual connection goes through dialer
	return fmt.Sprintf("http://%s", net.JoinHostPort(serviceName, fmt.Sprintf("%d", port))), client // DevSkim: ignore DS137138 - just a test
}

func deployTestApp(ctx context.Context, client *kubernetes.Clientset, namespace, name string, withAnnotations, withOTel bool) {
	// Create a simple deployment for testing
	replicas := int32(1)

	podAnnotations := map[string]string{}
	serviceAnnotations := map[string]string{}

	if withAnnotations {
		serviceAnnotations["prometheus.io/scrape"] = "true"
		serviceAnnotations["prometheus.io/port"] = "8080"
	}
	if withOTel {
		podAnnotations["instrumentation.opentelemetry.io/inject-sdk"] = "true"
	}

	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": name},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels:      map[string]string{"app": name},
					Annotations: podAnnotations,
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "app",
							Image: "busybox:latest",
							Command: []string{
								"/bin/sh", "-c",
								// Simple HTTP server that exposes /metrics endpoint
								`echo "Starting HTTP server on port 8080...";
								while true; do
									echo 'Test log from ` + name + `';
									{ echo -e 'HTTP/1.1 200 OK\r\nContent-Type: text/plain\r\n\r\n# HELP test_metric A test metric\n# TYPE test_metric counter\ntest_metric{app="` + name + `"} 1\n'; } | nc -l -p 8080 || true;
									sleep 1;
								done`,
							},
							Ports: []corev1.ContainerPort{
								{ContainerPort: 8080, Name: "metrics"},
							},
						},
					},
				},
			},
		},
	}

	_, err := client.AppsV1().Deployments(namespace).Create(ctx, deployment, metav1.CreateOptions{})
	if err != nil {
		fmt.Printf("Warning: failed to create test app %s: %v\n", name, err)
	}

	// Create Service if annotations are requested (for VMAgent service discovery)
	if withAnnotations {
		service := &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:        name,
				Namespace:   namespace,
				Annotations: serviceAnnotations,
			},
			Spec: corev1.ServiceSpec{
				Selector: map[string]string{"app": name},
				Ports: []corev1.ServicePort{
					{
						Name:       "metrics",
						Port:       8080,
						TargetPort: intstr.FromInt(8080),
						Protocol:   corev1.ProtocolTCP,
					},
				},
			},
		}

		_, err := client.CoreV1().Services(namespace).Create(ctx, service, metav1.CreateOptions{})
		if err != nil {
			fmt.Printf("Warning: failed to create service for %s: %v\n", name, err)
		}
	}

	// Wait for pod to be scheduled
	time.Sleep(5 * time.Second)
}

func deleteTestApp(ctx context.Context, client *kubernetes.Clientset, namespace, name string) {
	err := client.AppsV1().Deployments(namespace).Delete(ctx, name, metav1.DeleteOptions{})
	if err != nil {
		fmt.Printf("Warning: failed to delete test app %s: %v\n", name, err)
	}

	// Also delete service if it exists
	err = client.CoreV1().Services(namespace).Delete(ctx, name, metav1.DeleteOptions{})
	if err != nil && !strings.Contains(err.Error(), "not found") {
		fmt.Printf("Warning: failed to delete service for %s: %v\n", name, err)
	}
}

func projectRoot() string {
	// Get current working directory
	wd, err := os.Getwd()
	if err != nil {
		return "."
	}

	// Walk up until we find go.mod
	dir := wd
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			// Reached root without finding go.mod
			return "."
		}
		dir = parent
	}
}
