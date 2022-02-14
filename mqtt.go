package main

import (
	"log"
	"os"
	"sync"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/gofrs/uuid"
)

var logger = log.New(os.Stdout, "[Pi-App-Updater-Server] ", log.LstdFlags)

const (
	pushTopic = "repo/push"
)

type fn func(string)

var connectHandler mqtt.OnConnectHandler = func(client mqtt.Client) {
	logger.Println("Connected to MQTT server")
}

var connectLostHandler mqtt.ConnectionLostHandler = func(client mqtt.Client, err error) {
	logger.Fatalf("Connection to MQTT server lost: %v", err)
}

type mqttClient struct {
	client mqtt.Client
}

func newMQTTClient(addr string) mqttClient {
	opts := mqtt.NewClientOptions()
	opts.AddBroker(addr)
	var clientID string
	u, _ := uuid.NewV4()
	clientID = u.String()
	logger.Println("Starting MQTT client with id", clientID)
	opts.SetClientID(clientID)
	opts.OnConnect = connectHandler
	opts.OnConnectionLost = connectLostHandler
	client := mqtt.NewClient(opts)
	if token := client.Connect(); token.Wait() && token.Error() != nil {
		panic(token.Error())
	}
	return mqttClient{
		client,
	}
}

func (c mqttClient) Cleanup() {
	c.client.Disconnect(250)
}

func (c mqttClient) SubscribePushTopic(subscribeHandler fn) {
	subscribeTopic(c, pushTopic, subscribeHandler)
}

func subscribeTopic(c mqttClient, topic string, subscribeHandler fn) {
	var wg sync.WaitGroup
	wg.Add(1)

	if token := c.client.Subscribe(pushTopic, 0, func(client mqtt.Client, msg mqtt.Message) {
		subscribeHandler(string(msg.Payload()))
	}); token.Wait() && token.Error() != nil {
		logger.Fatal(token.Error())
	}
}

func (c mqttClient) PublishPushTopic(message string) {
	publish(c, pushTopic, message)
}

func publish(c mqttClient, topic, message string) {
	token := c.client.Publish(topic, 0, false, message)
	token.Wait()
}
