package main

import (
	"context"
	"encoding/csv"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"
	"k8s.io/klog"
	"k8s.io/metrics/pkg/client/clientset/versioned"
)

func main() {
	// Initialize Kubernetes client using kubeconfig
	kubeconfigPath := filepath.Join(homedir.HomeDir(), ".kube", "config")
	config, err := clientcmd.BuildConfigFromFlags("", kubeconfigPath)
	if err != nil {
		klog.Fatalf("Error building kubeconfig: %v", err)
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		klog.Fatalf("Error creating clientset: %v", err)
	}

	// Initialize Metrics client
	metricsClient, err := versioned.NewForConfig(config)
	if err != nil {
		klog.Fatalf("Error creating metrics clientset: %v", err)
	}

	// Read pod and namespace names from CSV file
	podsFile, err := os.Open("pods.csv")
	if err != nil {
		klog.Fatalf("Error opening pods file: %v", err)
	}
	defer podsFile.Close()

	podsCSV := csv.NewReader(podsFile)
	podsCSV.FieldsPerRecord = -1 // Allow variable number of fields
	podsData, err := podsCSV.ReadAll()
	if err != nil {
		klog.Fatalf("Error reading pods CSV: %v", err)
	}

	// Create a CSV file to export metrics
	metricsFile, err := os.Create("metrics.csv")
	if err != nil {
		klog.Fatalf("Error creating metrics CSV file: %v", err)
	}
	defer metricsFile.Close()

	metricsWriter := csv.NewWriter(metricsFile)
	defer metricsWriter.Flush()

	// Iterate over pods and stress test each
	for _, podData := range podsData {
		podName := strings.TrimSpace(podData[0])
		namespace := strings.TrimSpace(podData[1])

		klog.Infof("Stressing pod: %s in namespace: %s", podName, namespace)

		var cpuTotalMilli, memoryTotal int64
		var numContainers int

		// Get the pod from Kubernetes
		pod, err := clientset.CoreV1().Pods(namespace).Get(context.TODO(), podName, metav1.GetOptions{})
		if err != nil {
			klog.Errorf("Error getting pod: %v", err)
			continue
		}

		// Extract the deployment name from the pod's metadata name
		deploymentName := pod.ObjectMeta.Name
		if deploymentName == "" {
			klog.Warningf("No deployment found for pod: %s in namespace: %s", podName, namespace)
			continue
		}

		// Stress the pod (adjust the number of iterations as needed)
		for i := 0; i < 5; i++ {
			// Get resource usage metrics
			pod, err := clientset.CoreV1().Pods(namespace).Get(context.TODO(), podName, metav1.GetOptions{})
			if err != nil {
				klog.Errorf("Error getting pod: %v", err)
				continue
			}

			// Fetch and calculate container metrics
			for _, containerMetric := range pod.Status.ContainerStatuses {
				containerMetrics, err := metricsClient.MetricsV1beta1().PodMetricses(namespace).Get(context.TODO(), podName, metav1.GetOptions{})
				if err != nil {
					klog.Errorf("Error getting pod metrics: %v", err)
					continue
				}

				// Filter metrics for the specific container
				var containerUsage v1.ResourceList
				for _, container := range containerMetrics.Containers {
					if container.Name == containerMetric.Name {
						containerUsage = container.Usage
						break
					}
				}

				if containerUsage != nil {
					cpuUsage := containerUsage[v1.ResourceCPU]
					memoryUsage := containerUsage[v1.ResourceMemory]

					cpuTotalMilli += cpuUsage.MilliValue()
					memoryTotal += memoryUsage.Value()
					numContainers++
				}
			}

			// Wait for some time to stress the pod
			time.Sleep(1 * time.Second) // Adjust the duration as needed
		}

		// Calculate average metrics
		var avgCPUMilli int64
		var avgMemoryBytes int64

		if numContainers > 0 {
			avgCPUMilli = cpuTotalMilli / int64(numContainers)
			avgMemoryBytes = memoryTotal / int64(numContainers)
		}

		// Write average metrics to CSV
		row := []string{
			deploymentName, // Changed to deploymentName
			fmt.Sprintf("%d"+"m", avgCPUMilli),
			fmt.Sprintf("%.0f"+"Mi", float64(avgMemoryBytes)/(1024*1024)),
		}
		err = metricsWriter.Write(row)
		if err != nil {
			klog.Errorf("Error writing metrics CSV row: %v", err)
		}

		// Flush the writer after each pod
		metricsWriter.Flush()

		klog.Infof("Finished stressing pod: %s in namespace: %s", podName, namespace)
	}

	klog.Info("All pods stressed. Average metrics exported to metrics.csv")
}
