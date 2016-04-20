package wbgo_test

import (
	. "github.com/contactless/wbgo"
	"github.com/contactless/wbgo/testutils"
	"testing"
)

func TestFakeMQTT(t *testing.T) {
	broker := testutils.NewFakeMQTTBroker(t, nil)

	c1 := broker.MakeClient("c1")
	c1.Start()
	c1.Subscribe(func(message MQTTMessage) {
		broker.Rec("message for c1: %s", testutils.FormatMQTTMessage(message))
	}, "/some/topic")

	c2 := broker.MakeClient("c2")
	c2.Start()
	c2.Subscribe(func(message MQTTMessage) {
		broker.Rec("(some) message for c2: %s", testutils.FormatMQTTMessage(message))
	}, "/some/topic")
	c2.Subscribe(func(message MQTTMessage) {
		broker.Rec("(another) message for c2: %s", testutils.FormatMQTTMessage(message))
	}, "/another/topic")

	c1.Publish(MQTTMessage{"/some/topic", "somemsg", 1, true})

	c1.Publish(MQTTMessage{"/another/topic", "anothermsg", 0, false})

	c2.Unsubscribe("/another/topic")
	c1.Publish(MQTTMessage{"/another/topic", "anothermsg", 0, false})
	c1.Publish(MQTTMessage{"/some/topic", "anothermsg", 0, false})

	c1.Unsubscribe("/some/topic")
	c1.Publish(MQTTMessage{"/some/topic", "anothermsg", 0, false})

	c1.Stop()
	c2.Stop()

	broker.Verify(
		"Subscribe -- c1: /some/topic",
		"Subscribe -- c2: /some/topic",
		"Subscribe -- c2: /another/topic",
		"c1 -> /some/topic: [somemsg] (QoS 1, retained)",
		"message for c1: [somemsg] (QoS 1, retained)",
		"(some) message for c2: [somemsg] (QoS 1, retained)",

		"c1 -> /another/topic: [anothermsg] (QoS 0)",
		"(another) message for c2: [anothermsg] (QoS 0)",

		"Unsubscribe -- c2: /another/topic",
		"c1 -> /another/topic: [anothermsg] (QoS 0)",
		"c1 -> /some/topic: [anothermsg] (QoS 0)",
		"message for c1: [anothermsg] (QoS 0)",
		"(some) message for c2: [anothermsg] (QoS 0)",

		"Unsubscribe -- c1: /some/topic",
		"c1 -> /some/topic: [anothermsg] (QoS 0)",
		"(some) message for c2: [anothermsg] (QoS 0)",

		"stop: c1",
		"stop: c2",
	)
}

// TBD: support wildcard topics
