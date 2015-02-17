package wbgo

import (
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
	if ch := client.innerClient.PublishMessage(message.Topic, m); ch != nil {
		<- ch
	} else {
		panic("PublishMessage() failed")
	}
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
		callback(MQTTMessage{msg.Topic(), string(msg.Payload()),
			byte(msg.QoS()), msg.RetainedFlag()})
	}

	if receipt, err := client.innerClient.StartSubscription(wrappedCallback, filters...); err != nil {
		panic(err)
	} else {
		<-receipt
	}
}

func (client *PahoMQTTClient) Unsubscribe(topics... string) {
	if receipt, err := client.innerClient.EndSubscription(topics...); err != nil {
		panic(err)
	} else {
		<-receipt
	}
}
