package wbgo

import (
	"fmt"
	// MQTT "github.com/contactless/org.eclipse.paho.mqtt.golang"
	MQTT "github.com/eclipse/paho.mqtt.golang"
	"log"
	"log/syslog"
	"math/rand"
	"os"
	"sync"
	"time"
)

const (
	DISCONNECT_WAIT_MS = 100
	TOKEN_QUEUE_LEN    = 512
)

type MQTTSubscriptionMap map[string][]MQTTMessageHandler

type PahoMQTTClient struct {
	startMtx        sync.Mutex
	connMtx         sync.Mutex
	innerClient     MQTT.Client
	waitForRetained bool
	ready           chan struct{}
	stopped         chan struct{}
	tokens          chan MQTT.Token
	subs            MQTTSubscriptionMap
	started         bool
	connected       bool
}

func NewPahoMQTTClient(server, clientID string, waitForRetained bool) (client *PahoMQTTClient) {
	hostname, err := os.Hostname()
	if err != nil {
		hostname = "localhost"
	}
	clientID = fmt.Sprintf("%s-%s-%d", clientID, hostname, os.Getpid())
	client = &PahoMQTTClient{
		waitForRetained: waitForRetained,
		ready:           make(chan struct{}),
		stopped:         make(chan struct{}),
		tokens:          make(chan MQTT.Token, TOKEN_QUEUE_LEN),
		subs:            make(MQTTSubscriptionMap),
		started:         false,
		connected:       false,
	}
	opts := MQTT.NewClientOptions().
		AddBroker(server).
		SetClientID(clientID).
		SetOnConnectHandler(client.onConnect).
		SetConnectionLostHandler(client.onConnectionLost)
	client.innerClient = MQTT.NewClient(opts)
	return
}

func (client *PahoMQTTClient) onConnect(innerClient MQTT.Client) {
	Info.Printf("MQTT connection established")
	client.connMtx.Lock()
	client.connected = true
	for topic, callbacks := range client.subs {
		for _, callback := range callbacks {
			if DebuggingEnabled() {
				Debug.Printf("RESUB: %s", topic)
			}
			client.tokens <- client.subscribe(callback, topic)
		}
	}
	client.connMtx.Unlock()
}

func (client *PahoMQTTClient) onConnectionLost(inner MQTT.Client, err error) {
	Warn.Printf("MQTT connection lost")
	client.connMtx.Lock()
	client.connected = false
	client.connMtx.Unlock()
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
	client.startMtx.Lock()
	defer client.startMtx.Unlock()
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
	client.startMtx.Lock()
	defer client.startMtx.Unlock()
	if !client.started {
		return
	}
	client.started = false
	client.innerClient.Disconnect(DISCONNECT_WAIT_MS)
	client.stopped <- struct{}{}
}

func (client *PahoMQTTClient) Publish(message MQTTMessage) {
	if DebuggingEnabled() {
		Debug.Printf("PUB: %s -> %s", message.Topic, message.Payload)
	}
	client.tokens <- client.innerClient.Publish(
		message.Topic, message.QoS, message.Retained, message.Payload)
}

func (client *PahoMQTTClient) subscribe(callback MQTTMessageHandler, topics ...string) MQTT.Token {
	filters := make(map[string]byte)
	for _, topic := range topics {
		if DebuggingEnabled() {
			Debug.Printf("SUB: %s", topic)
		}
		filters[topic] = 2
	}

	wrappedCallback := func(client MQTT.Client, msg MQTT.Message) {
		if DebuggingEnabled() {
			Debug.Printf("GOT MESSAGE: %s --- %s", msg.Topic(), string(msg.Payload()))
		}
		callback(MQTTMessage{msg.Topic(), string(msg.Payload()),
			byte(msg.Qos()), msg.Retained()})
	}

	return client.innerClient.SubscribeMultiple(filters, wrappedCallback)
}

func (client *PahoMQTTClient) Subscribe(callback MQTTMessageHandler, topics ...string) {
	client.connMtx.Lock()
	for _, topic := range topics {
		subList, found := client.subs[topic]
		if !found {
			client.subs[topic] = []MQTTMessageHandler{callback}
		} else {
			client.subs[topic] = append(subList, callback)
		}
	}
	if !client.connected {
		client.connMtx.Unlock()
		return
	}
	client.connMtx.Unlock()
	client.tokens <- client.subscribe(callback, topics...)
}

func (client *PahoMQTTClient) Unsubscribe(topics ...string) {
	client.connMtx.Lock()
	for _, topic := range topics {
		delete(client.subs, topic)
	}
	if !client.connected {
		client.connMtx.Unlock()
		return
	}
	client.connMtx.Unlock()
	client.innerClient.Unsubscribe(topics...)
}

func EnableMQTTDebugLog(use_syslog bool) {
	if use_syslog {
		MQTT.ERROR = makeSyslogger(syslog.LOG_DAEMON|syslog.LOG_ERR, "[MQTT] ERROR: ")
		MQTT.CRITICAL = makeSyslogger(syslog.LOG_DAEMON|syslog.LOG_CRIT, "[MQTT] CRITICAL: ")
		MQTT.WARN = makeSyslogger(syslog.LOG_DAEMON|syslog.LOG_WARNING, "[MQTT] WARNING: ")
		MQTT.DEBUG = makeSyslogger(syslog.LOG_DAEMON|syslog.LOG_DEBUG, "[MQTT] DEBUG: ")
	} else {
		MQTT.ERROR = log.New(os.Stdout, "", 0)
		MQTT.CRITICAL = log.New(os.Stdout, "", 0)
		MQTT.WARN = log.New(os.Stdout, "", 0)
		MQTT.DEBUG = log.New(os.Stdout, "", 0)
	}
}
