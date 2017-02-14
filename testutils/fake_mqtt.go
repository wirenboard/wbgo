package testutils

import (
	"fmt"
	"github.com/contactless/wbgo"
	"log"
	"strings"
	"sync"
	"testing"
)

const (
	FAKEMQTT_RECV_LEN = 64
)

func topicPartsMatch(pattern []string, topic []string) bool {
	if len(pattern) == 0 {
		return len(topic) == 0
	}

	if pattern[0] == "#" {
		return true
	}

	return len(topic) > 0 &&
		(pattern[0] == "+" || (pattern[0] == topic[0])) &&
		topicPartsMatch(pattern[1:], topic[1:])
}

func topicMatch(pattern string, topic string) bool {
	return topicPartsMatch(strings.Split(pattern, "/"), strings.Split(topic, "/"))
}

func FormatMQTTMessage(message wbgo.MQTTMessage) string {
	suffix := ""
	if message.Retained {
		suffix = ", retained"
	}
	return fmt.Sprintf("[%s] (QoS %d%s)",
		string(message.Payload), message.QoS, suffix)
}

type SubscriptionList []*FakeMQTTClient
type SubscriptionMap map[string]SubscriptionList

type FakeMQTTBroker struct {
	*Recorder
	sync.Mutex
	subscriptions   SubscriptionMap
	waitForRetained bool
	readyChannels   []chan struct{}
}

func NewFakeMQTTBroker(t *testing.T, rec *Recorder) (broker *FakeMQTTBroker) {
	if rec == nil {
		rec = NewRecorder(t)
	}
	broker = &FakeMQTTBroker{
		Recorder:      rec,
		subscriptions: make(SubscriptionMap),
	}
	return
}

func (broker *FakeMQTTBroker) SetWaitForRetained(waitForRetained bool) {
	broker.waitForRetained = waitForRetained
}

func (broker *FakeMQTTBroker) SetReady() {
	for _, ch := range broker.readyChannels {
		close(ch)
	}
	broker.readyChannels = nil
}

func (broker *FakeMQTTBroker) Publish(origin string, message wbgo.MQTTMessage) {
	broker.Lock()
	defer broker.Unlock()

	waitChs := make([]chan struct{}, 0, len(broker.subscriptions))

	// tell subscribed clients we have a new message
	for pattern, subs := range broker.subscriptions {
		if !topicMatch(pattern, message.Topic) {
			continue
		}
		for _, client := range subs {
			w := make(chan struct{})
			waitChs = append(waitChs, w)
			client.receive(message, w)
		}
	}

	// wait for all clients to process this message and make a record
	wbgo.Debug.Printf("REC: %s -> %s: %s", origin, message.Topic, FormatMQTTMessage(message))
	broker.RecFunc(func() string {
		// wait for clients
		for _, w := range waitChs {
			<-w
		}
		return fmt.Sprintf("%s -> %s: %s", origin, message.Topic, FormatMQTTMessage(message))
	})
}

func (broker *FakeMQTTBroker) Subscribe(client *FakeMQTTClient, topic string) {
	broker.Lock()
	defer broker.Unlock()
	broker.Rec("Subscribe -- %s: %s", client.id, topic)
	subs, found := broker.subscriptions[topic]
	if !found {
		broker.subscriptions[topic] = SubscriptionList{client}
	} else {
		for _, c := range subs {
			if c == client {
				return
			}
		}
		broker.subscriptions[topic] = append(subs, client)
	}
}

func (broker *FakeMQTTBroker) Unsubscribe(client *FakeMQTTClient, topic string) {
	broker.Lock()
	defer broker.Unlock()
	broker.Rec("Unsubscribe -- %s: %s", client.id, topic)
	subs, found := broker.subscriptions[topic]
	if !found {
		return
	} else {
		newSubs := make(SubscriptionList, 0, len(subs))
		for _, c := range subs {
			if c != client {
				newSubs = append(newSubs, c)
			}
		}
		broker.subscriptions[topic] = newSubs
	}
}

func (broker *FakeMQTTBroker) MakeClient(id string) (client *FakeMQTTClient) {
	client = &FakeMQTTClient{
		id:          id,
		started:     false,
		broker:      broker,
		callbackMap: make(map[string][]wbgo.MQTTMessageHandler),
		ready:       make(chan struct{}),
		quit:        make(chan chan struct{}),
		recv:        make(chan fakeMQTTEvent, FAKEMQTT_RECV_LEN),
	}
	if broker.waitForRetained {
		broker.readyChannels = append(broker.readyChannels, client.ready)
	}
	return client
}

type fakeMQTTEvent struct {
	message wbgo.MQTTMessage
	done    chan struct{}
}

type FakeMQTTClient struct {
	sync.Mutex
	id          string
	started     bool
	broker      *FakeMQTTBroker
	callbackMap map[string][]wbgo.MQTTMessageHandler
	ready       chan struct{}
	quit        chan chan struct{}
	recv        chan fakeMQTTEvent
}

func (client *FakeMQTTClient) receive(message wbgo.MQTTMessage, done chan struct{}) {
	client.recv <- fakeMQTTEvent{message, done}
}

func (client *FakeMQTTClient) handle(message wbgo.MQTTMessage) {
	client.Lock()
	// wbgo.Debug.Printf("search for handlers on message %v", message)
	// collect handlers to run
	allHandlers := make([]wbgo.MQTTMessageHandler, 0, 8)
	for topic, handlers := range client.callbackMap {
		if !topicMatch(topic, message.Topic) {
			continue
		}
		for _, handler := range handlers {
			// Debug.Println("append handler
			allHandlers = append(allHandlers, handler)
		}
	}
	client.Unlock()

	for _, handler := range allHandlers {
		handler(message)
	}
}

func (client *FakeMQTTClient) WaitForReady() <-chan struct{} {
	return client.ready
}

func (client *FakeMQTTClient) Start() {
	if client.started {
		return
	}
	client.started = true
	if !client.broker.waitForRetained {
		close(client.ready)
	}

	go func() {
		for {
			select {
			case e := <-client.recv:
				client.handle(e.message)
				close(e.done)
			case quitCh := <-client.quit:
				close(quitCh)
				return
			}
		}
	}()
}

func (client *FakeMQTTClient) Stop() {
	client.ensureStarted()
	client.started = false
	client.broker.Rec("stop: %s", client.id)

	q := make(chan struct{})
	client.quit <- q
	<-q
	close(client.quit)
}

func (client *FakeMQTTClient) ensureStarted() {
	if !client.started {
		log.Panicf("%s: client not started", client.id)
	}
}

func (client *FakeMQTTClient) Publish(message wbgo.MQTTMessage) {
	client.ensureStarted()
	client.broker.Publish(client.id, message)
}

func (client *FakeMQTTClient) Subscribe(callback wbgo.MQTTMessageHandler, topics ...string) {
	client.Lock()
	defer client.Unlock()
	client.ensureStarted()
	for _, topic := range topics {
		client.broker.Subscribe(client, topic)
		handlerList, found := client.callbackMap[topic]
		if found {
			client.callbackMap[topic] = append(handlerList, callback)
		} else {
			client.callbackMap[topic] = []wbgo.MQTTMessageHandler{callback}
		}
	}
}

func (client *FakeMQTTClient) Unsubscribe(topics ...string) {
	client.Lock()
	defer client.Unlock()
	client.ensureStarted()
	for _, topic := range topics {
		client.broker.Unsubscribe(client, topic)
		delete(client.callbackMap, topic)
	}
}

type FakeMQTTFixture struct {
	*Recorder
	Broker *FakeMQTTBroker
}

func NewFakeMQTTFixture(t *testing.T) *FakeMQTTFixture {
	rec := NewRecorder(t)
	return &FakeMQTTFixture{
		Recorder: rec,
		Broker:   NewFakeMQTTBroker(t, rec),
	}
}
