package informer

import (
	"context"
	"fmt"
	"log"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/metrics/pkg/client/clientset/versioned"
)

type Informer struct {
	mqttClient    *PahoMqttClient
	clientset     *kubernetes.Clientset
	metricsClient *versioned.Clientset
	namespace     string
	pods          map[string]bool
}

func NewInformer(mqttClient *PahoMqttClient, kubeConfigPath string, namespace string) *Informer {
	config, err := clientcmd.BuildConfigFromFlags("", kubeConfigPath)
	if err != nil {
		log.Fatalf("Error loading kubeconfig: %v", err)
	}
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		log.Fatalf("Error creating Kubernetes client: %v", err)
	} else {
		log.Println("K8s client connected")
	}

	metricsClient, err := versioned.NewForConfig(config)
	if err != nil {
		log.Fatalf("Error creating Metrics client: %v", err)
	}

	return &Informer{
		mqttClient:    mqttClient,
		clientset:     clientset,
		metricsClient: metricsClient,
		namespace:     namespace,
		pods:          make(map[string]bool),
	}
}

func (informer *Informer) Start() {
	go informer.fetchPodCpuUsages()
	informer.watchPods()
}

func (informer *Informer) watchPods() {
	// stop signal for the informer
	stopper := make(chan struct{})
	defer close(stopper)

	// create shared informers for resources in all known API group versions with a reSync period and namespace
	factory := informers.NewSharedInformerFactoryWithOptions(informer.clientset, 10*time.Second, informers.WithNamespace(informer.namespace))
	podInformer := factory.Core().V1().Pods().Informer()

	defer runtime.HandleCrash()

	// start informer ->
	go factory.Start(stopper)

	// start to sync and call list
	if !cache.WaitForCacheSync(stopper, podInformer.HasSynced) {
		runtime.HandleError(fmt.Errorf("timed out waiting for caches to sync"))
		return
	}

	podInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			pod := obj.(*corev1.Pod)

			if len(pod.Spec.Containers) > 0 && len(pod.Spec.Containers[0].Ports) > 0 {
				node := pod.Labels["node"]
				service := pod.Labels["service"]
				instance := pod.Name

				instanceMessage := &InstanceMessage{
					IpAddress: pod.Status.PodIP,
					Port:      pod.Spec.Containers[0].Ports[0].ContainerPort,
				}
				message := &Message{
					Type:        InstanceAdded_MessageType,
					MessageData: instanceMessage,
				}

				topic := fmt.Sprintf("changes/node/%s/service/%s/instance/%s", node, service, instance)
				informer.mqttClient.SendMessage(topic, message)

				informer.pods[pod.Name] = false
				log.Println(informer.pods)
			}
		},
		UpdateFunc: func(oldObj, newObj interface{}) {
			oldPod := oldObj.(*corev1.Pod)
			newPod := newObj.(*corev1.Pod)

			node := newPod.Labels["node"]
			service := newPod.Labels["service"]
			instance := newPod.Name

			// Check if the status has changed to 'Running' or 'Stopped'
			if newPod.Status.Phase == corev1.PodRunning && oldPod.Status.Phase != corev1.PodRunning {
				if len(newPod.Spec.Containers) > 0 && len(newPod.Spec.Containers[0].Ports) > 0 {
					instanceMessage := &InstanceMessage{
						IpAddress: newPod.Status.PodIP,
						Port:      newPod.Spec.Containers[0].Ports[0].ContainerPort,
					}
					message := &Message{
						Type:        InstanceRunning_MessageType,
						MessageData: instanceMessage,
					}

					topic := fmt.Sprintf("changes/node/%s/service/%s/instance/%s", node, service, instance)
					informer.mqttClient.SendMessage(topic, message)
				}
			} else if newPod.Status.Phase == corev1.PodSucceeded || newPod.Status.Phase == corev1.PodFailed ||
				newPod.DeletionTimestamp != nil {
				message := &Message{
					Type:        InstanceStopped_MessageType,
					MessageData: nil,
				}

				topic := fmt.Sprintf("changes/node/%s/service/%s/instance/%s", node, service, instance)
				informer.mqttClient.SendMessage(topic, message)
			}
		},
		DeleteFunc: func(obj interface{}) {
			pod := obj.(*corev1.Pod)
			node := pod.Labels["node"]
			service := pod.Labels["service"]
			instance := pod.Name

			message := &Message{
				Type:        InstanceRemoved_MessageType,
				MessageData: nil,
			}

			topic := fmt.Sprintf("changes/node/%s/service/%s/instance/%s", node, service, instance)
			informer.mqttClient.SendMessage(topic, message)

			delete(informer.pods, pod.Name)
		},
	})

	// block the main go routine from exiting
	<-stopper
}

func (informer *Informer) fetchPodCpuUsages() {
	for {
		for podName, _ := range informer.pods {
			cpuUsage, err := informer.getPodCpuUsageMetrics(podName)
			if err != nil {
				log.Printf("Error getting CPU usage for pod %s: %v", podName, err)
			}

			if podName == "i1" {
				log.Printf("CPU usage of %s: %.3f", podName, cpuUsage)
			}
		}

		// Sleep for some time before checking again
		time.Sleep(1 * time.Second)
	}
}

func (informer *Informer) getPodCpuUsage(podName string) (float64, error) {
	// Get metrics for the pod
	podMetrics, err := informer.metricsClient.MetricsV1beta1().PodMetricses(informer.namespace).Get(context.TODO(), podName, metav1.GetOptions{})
	if err != nil {
		return 0, fmt.Errorf("failed to get pod metrics: %v", err)
	}

	var cpuUsage float64
	for _, container := range podMetrics.Containers {
		// Aggregate CPU usage for all containers
		if cpu, ok := container.Usage[corev1.ResourceCPU]; ok {
			cpuUsage += float64(cpu.Value()) / 1e9 // Convert from nanocores to cores
		}
	}
	return cpuUsage, nil
}

func (informer *Informer) getPodCpuUsageMetrics(podName string) (float64, error) {
	// Get the current metrics for the pod
	podMetrics, err := informer.metricsClient.MetricsV1beta1().PodMetricses(informer.namespace).Get(context.TODO(), podName, metav1.GetOptions{})
	if err != nil {
		return 0, fmt.Errorf("failed to get pod metrics: %v", err)
	}

	var totalCPUUsageMilli float64

	// Sum up the CPU usage of all containers in the pod
	for _, container := range podMetrics.Containers {
		if cpuUsage, ok := container.Usage[corev1.ResourceCPU]; ok {
			// Convert from nanocores to millicores (1 millicore = 1000 nanocores)
			mcpu := float64(cpuUsage.MilliValue())
			totalCPUUsageMilli += mcpu
		}
	}

	totalCpuUsageCores := totalCPUUsageMilli / float64(1000)

	return totalCpuUsageCores, nil
}
