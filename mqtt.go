package wbgo

import (
	"log"
	"strings"
	MQTT "git.eclipse.org/gitroot/paho/org.eclipse.paho.mqtt.golang.git"
)

const DISCONNECT_WAIT_MS = 100

type PahoMQTTClient struct {
	innerClient *MQTT.MqttClient
}

func NewPahoMQTTClient(server, clientID string) (client *PahoMQTTClient) {
	opts := MQTT.NewClientOptions().AddBroker(server).SetClientId(clientID)
	client = &PahoMQTTClient{MQTT.NewClient(opts)}
	return
}

func (client *PahoMQTTClient) Start() {
	_, err := client.innerClient.Start()
	if err != nil {
		panic(err)
	}
}

func (client *PahoMQTTClient) Stop() {
	client.innerClient.Disconnect(DISCONNECT_WAIT_MS)
}

func (client *PahoMQTTClient) Publish(message MQTTMessage) {
	m := MQTT.NewMessage([]byte(message.Payload))
	m.SetQoS(MQTT.QoS(message.QoS))
	m.SetRetainedFlag(message.Retained)
	go func () {
		if ch := client.innerClient.PublishMessage(message.Topic, m); ch != nil {
			<- ch
		} else {
			log.Printf("WARNING: PublishMessage() failed for topic: ", message.Topic) // FIXME
		}
	}()
}

func (client *PahoMQTTClient) Subscribe(callback MQTTMessageHandler, topics... string) {
	filters := make([]*MQTT.TopicFilter, len(topics))
	for i, topic := range topics {
		if filter, err := MQTT.NewTopicFilter(topic, 2); err != nil {
			panic("bad subscription")
		} else {
			filters[i] = filter
		}
	}

	wrappedCallback := func(client *MQTT.MqttClient, msg MQTT.Message) {
		log.Printf("GOT MESSAGE: %s --- %s", msg.Topic(), string(msg.Payload()))
		callback(MQTTMessage{msg.Topic(), string(msg.Payload()),
			byte(msg.QoS()), msg.RetainedFlag()})
	}

	log.Printf("SUB: %s", strings.Join(topics, "; "))
	go func () {
		if receipt, err := client.innerClient.StartSubscription(wrappedCallback, filters...); err != nil {
			panic(err)
		} else {
			<-receipt
			log.Printf("SUB DONE: %s", strings.Join(topics, "; "))
		}
	}()
}

func (client *PahoMQTTClient) Unsubscribe(topics... string) {
	go func () {
		if receipt, err := client.innerClient.EndSubscription(topics...); err != nil {
			panic(err)
		} else {
			<-receipt
		}
	}()
}
