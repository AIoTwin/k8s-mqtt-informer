package informer

import (
	"encoding/json"
	"time"

	"log"

	mqtt "github.com/eclipse/paho.mqtt.golang"
)

type PahoMqttClient struct {
	brokerUrl  string
	clientId   string
	mqttClient mqtt.Client
}

func NewPahoMqttClient(brokerUrl string, clientId string) (*PahoMqttClient, error) {
	pmc := &PahoMqttClient{
		brokerUrl: brokerUrl,
		clientId:  clientId,
	}

	client, err := pmc.createClient()
	if err != nil {
		return nil, err
	}
	pmc.mqttClient = client

	return pmc, nil
}

func (pmc *PahoMqttClient) createClient() (mqtt.Client, error) {
	opts := mqtt.NewClientOptions()
	opts.AddBroker(pmc.brokerUrl)
	opts.SetClientID(pmc.clientId)
	opts.SetAutoReconnect(true)

	//connection lost handler
	opts.OnConnectionLost = func(c mqtt.Client, err error) {
		log.Println("Connection lost")
	}

	//subscribe to topic after connect
	opts.OnConnect = func(c mqtt.Client) {
		log.Println("Client connected")
	}

	client := mqtt.NewClient(opts)
	if token := client.Connect(); token.Wait() && token.Error() != nil {
		err := token.Error()
		log.Printf("Error: %s", err.Error())
		return nil, err
	}

	log.Println("Successfully connected to the broker")

	return client, nil
}

func (pmc *PahoMqttClient) GetClient() mqtt.Client {
	return pmc.mqttClient
}

// SendMessage sends a message to the specified topic
func (pmc *PahoMqttClient) SendMessage(topic string, message interface{}) error {
	messageBytes, err := json.Marshal(message)
	if err != nil {
		log.Printf("Can't marshall JSON: %s", err.Error())
		return err
	}

	messageString := string(messageBytes)
	token := pmc.mqttClient.Publish(topic, 1, false, messageString)

	go func() {
		_ = token.WaitTimeout(1 * time.Second)
		if token.Error() != nil {
			log.Printf("MQTT error: %s", token.Error().Error())
		}
	}()

	log.Printf("Published message to %s: %s\n", topic, message)

	return nil
}
