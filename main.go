package main

import (
	"k8s-mqtt-informer/informer"
	"os"
	"time"
)

func main() {
	brokerUrl := os.Getenv("INFORMER_MQTT_BROKER_URL")
	mqttClient := createMqttClient(brokerUrl, "informer")

	kubeConfigPath := os.Getenv("INFORMER_KUBECONFIG_PATH")

	namespace := os.Getenv("INFORMER_NAMESPACE")

	informer := informer.NewInformer(mqttClient, kubeConfigPath, namespace)
	informer.Start()
}

func createMqttClient(brokerUrl string, clientId string) *informer.PahoMqttClient {
	for try := 1; try <= 6; try++ {
		pmc, err := informer.NewPahoMqttClient(brokerUrl, clientId)
		if err != nil {
			time.Sleep(10 * time.Second)
		} else {
			return pmc
		}
	}

	return nil
}
