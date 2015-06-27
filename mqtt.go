package wbgo

import (
	"fmt"
	MQTT "git.eclipse.org/gitroot/paho/org.eclipse.paho.mqtt.golang.git"
	"math/rand"
	"os"
	"sync"
	"time"
)

const (
	DISCONNECT_WAIT_MS = 100
	TOKEN_QUEUE_LEN    = 512
)

type PahoMQTTClient struct {
	sync.Mutex
	innerClient     *MQTT.Client
	waitForRetained bool
	ready           chan struct{}
	stopped         chan struct{}
	tokens          chan MQTT.Token
	started         bool
}

func NewPahoMQTTClient(server, clientID string, waitForRetained bool) (client *PahoMQTTClient) {
	hostname, err := os.Hostname()
	if err != nil {
		hostname = "localhost"
	}
	clientID = fmt.Sprintf("%s-%s-%d", clientID, hostname, os.Getpid())
	opts := MQTT.NewClientOptions().AddBroker(server).SetClientID(clientID)
	client = &PahoMQTTClient{
		innerClient:     MQTT.NewClient(opts),
		waitForRetained: waitForRetained,
		ready:           make(chan struct{}),
		stopped:         make(chan struct{}),
		tokens:          make(chan MQTT.Token, TOKEN_QUEUE_LEN),
		started:         false,
	}
	return
}

func (client *PahoMQTTClient) WaitForReady() <-chan struct{} {
	if !client.waitForRetained {
		close(client.ready)
	} else {
		go func() {
			// There's no guarantee that messages with different QoS don't get
			// mixed up, thus we check QoS 1 and QoS 2 for retained messages here.
			// According to the standard, our message will arrive after the
			// old messages.
			// On the other hand, there's no guarantee that QoS 0 messages
			// will arrive at all, so we don't check for QoS 0 messages here.
			r := rand.New(rand.NewSource(time.Now().UnixNano()))
			waitTopic := fmt.Sprintf("/wbretainhack/%016x%016x", r.Int63(), r.Int63())
			got1, got2 := false, false
			// subscribe synchronously to make sure that
			// messages are published after the subscription is complete
			token := client.subscribe(func(msg MQTTMessage) {
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
			token.Wait()
			if token.Error() != nil {
				Error.Printf("wbretainhack subscription failed: %s", token.Error())
				// don't hang there, anyway
				close(client.ready)
			}
			client.Publish(MQTTMessage{Topic: waitTopic, Payload: "1", QoS: 1})
			client.Publish(MQTTMessage{Topic: waitTopic, Payload: "2", QoS: 2})
		}()
	}

	return client.ready
}

func (client *PahoMQTTClient) Start() {
	client.Lock()
	defer client.Unlock()
	if client.started {
		return
	}
	client.started = true
	if token := client.innerClient.Connect(); token.Wait() && token.Error() != nil {
		panic(token.Error())
	}
	go func() {
		for {
			select {
			case <-client.stopped:
				return
			case token := <-client.tokens:
				token.Wait()
				if token.Error() != nil {
					Error.Printf("MQTT error: %s", token.Error())
				}
			}
		}
	}()
}

func (client *PahoMQTTClient) Stop() {
	// FIXME: restarting the client may not work properly
	client.Lock()
	defer client.Unlock()
	if !client.started {
		return
	}
	client.started = false
	client.innerClient.Disconnect(DISCONNECT_WAIT_MS)
	client.stopped <- struct{}{}
}

func (client *PahoMQTTClient) Publish(message MQTTMessage) {
	Debug.Printf("PUB: %s -> %s", message.Topic, message.Payload)
	client.tokens <- client.innerClient.Publish(
		message.Topic, message.QoS, message.Retained, message.Payload)
}

func (client *PahoMQTTClient) subscribe(callback MQTTMessageHandler, topics ...string) MQTT.Token {
	filters := make(map[string]byte)
	for _, topic := range topics {
		Debug.Printf("SUB: %s", topic)
		filters[topic] = 2
	}

	wrappedCallback := func(client *MQTT.Client, msg MQTT.Message) {
		Debug.Printf("GOT MESSAGE: %s --- %s", msg.Topic(), string(msg.Payload()))
		callback(MQTTMessage{msg.Topic(), string(msg.Payload()),
			byte(msg.Qos()), msg.Retained()})
	}

	return client.innerClient.SubscribeMultiple(filters, wrappedCallback)
}

func (client *PahoMQTTClient) Subscribe(callback MQTTMessageHandler, topics ...string) {
	client.tokens <- client.subscribe(callback, topics...)
}

func (client *PahoMQTTClient) Unsubscribe(topics ...string) {
	client.innerClient.Unsubscribe(topics...)
}
