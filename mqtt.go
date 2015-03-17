package wbgo

import (
	"os"
	"fmt"
	"time"
	"math/rand"
	"strings"
	MQTT "git.eclipse.org/gitroot/paho/org.eclipse.paho.mqtt.golang.git"
)

const DISCONNECT_WAIT_MS = 100

type PahoMQTTClient struct {
	innerClient *MQTT.MqttClient
	waitForRetained bool
	ready chan struct{}
}

func NewPahoMQTTClient(server, clientID string, waitForRetained bool) (client *PahoMQTTClient) {
	hostname, err := os.Hostname()
	if err != nil {
		hostname = "localhost"
	}
	clientID = fmt.Sprintf("%s-%s-%d", clientID, hostname, os.Getpid())
	opts := MQTT.NewClientOptions().AddBroker(server).SetClientId(clientID)
	client = &PahoMQTTClient{
		MQTT.NewClient(opts),
		waitForRetained,
		make(chan struct{}),
	}
	return
}

func (client *PahoMQTTClient) ReadyChannel() <-chan struct{} {
	return client.ready
}

func (client *PahoMQTTClient) Start() {
	_, err := client.innerClient.Start()
	if err != nil {
		panic(err)
	}
	if !client.waitForRetained {
		close(client.ready)
	} else {
		// There's no guarantee that messages with different QoS don't get
		// mixed up, thus we check QoS 1 and QoS 2 for retained messages here.
		// According to the standard, our message will arrive after the
		// old messages.
		// On the other hand, there's no guarantee that QoS 0 messages
		// will arrive at all, so we don't check for QoS 0 messages here.
		r := rand.New(rand.NewSource(time.Now().UnixNano()))
		waitTopic := fmt.Sprintf("/wbretainhack/%16x%16x", r.Int63(), r.Int63())
		got1, got2 := false, false
		client.Subscribe(func (msg MQTTMessage) {
			switch {
			case got1 && got2:
				// avoid closing the channel twice upon QoS1
				// message duplication
				return
			case msg.Payload == "1":
				got1 = true
			case msg.Payload == "2":
				got2 = true
			}
			if got1 && got2 {
				client.Unsubscribe(waitTopic)
				close(client.ready)
			}
		}, waitTopic)
		client.Publish(MQTTMessage{Topic: waitTopic, Payload: "1", QoS: 1})
		client.Publish(MQTTMessage{Topic: waitTopic, Payload: "2", QoS: 2})
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
			Error.Printf("PublishMessage() failed for topic: ", message.Topic) // FIXME
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
		Debug.Printf("GOT MESSAGE: %s --- %s", msg.Topic(), string(msg.Payload()))
		callback(MQTTMessage{msg.Topic(), string(msg.Payload()),
			byte(msg.QoS()), msg.RetainedFlag()})
	}

	Debug.Printf("SUB: %s", strings.Join(topics, "; "))
	go func () {
		if receipt, err := client.innerClient.StartSubscription(wrappedCallback, filters...); err != nil {
			panic(err)
		} else {
			<-receipt
			Debug.Printf("SUB DONE: %s", strings.Join(topics, "; "))
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
