package wbgo

import (
	"github.com/stretchr/testify/assert"
	"testing"
)

type driverFixture struct {
	t      *testing.T
	broker *FakeMQTTBroker
	model  *FakeModel
	client *FakeMQTTClient
	driver *Driver
}

func newDriverFixture(t *testing.T) *driverFixture {
	fixture := &driverFixture{
		broker: NewFakeMQTTBroker(t),
		model:  NewFakeModel(t),
	}

	fixture.client = fixture.broker.MakeClient("tst")
	fixture.client.Start()

	fixture.driver = NewDriver(fixture.model, fixture.broker.MakeClient("driver"))
	fixture.driver.SetAutoPoll(false)

	return fixture
}

func TestDriver(t *testing.T) {
	fixture := newDriverFixture(t)

	dev := fixture.model.MakeLocalDevice("somedev", "SomeDev", map[string]string{
		"paramOne": "switch",
		"paramTwo": "switch",
	})
	fixture.driver.Start()

	fixture.broker.Verify(
		"driver -> /devices/somedev/meta/name: [SomeDev] (QoS 1, retained)",
		"driver -> /devices/somedev/controls/paramOne/meta/type: [switch] (QoS 1, retained)",
		"driver -> /devices/somedev/controls/paramOne/meta/order: [1] (QoS 1, retained)",
		"driver -> /devices/somedev/controls/paramOne: [0] (QoS 1, retained)",
		"Subscribe -- driver: /devices/somedev/controls/paramOne/on",
		"driver -> /devices/somedev/controls/paramTwo/meta/type: [switch] (QoS 1, retained)",
		"driver -> /devices/somedev/controls/paramTwo/meta/order: [2] (QoS 1, retained)",
		"driver -> /devices/somedev/controls/paramTwo: [0] (QoS 1, retained)",
		"Subscribe -- driver: /devices/somedev/controls/paramTwo/on",
	)

	for i := 0; i < 3; i++ {
		fixture.driver.Poll()
		fixture.model.Verify("poll")
	}

	fixture.client.Publish(MQTTMessage{"/devices/somedev/controls/paramOne/on", "1", 1, false})
	fixture.broker.Verify(
		"tst -> /devices/somedev/controls/paramOne/on: [1] (QoS 1)",
		"driver -> /devices/somedev/controls/paramOne: [1] (QoS 1, retained)",
	)
	fixture.model.Verify(
		"AcceptOnValue: somedev.paramOne = 1",
	)

	fixture.driver.CallSync(func() {
		dev.ReceiveValue("paramTwo", "1")
	})
	fixture.broker.Verify(
		"driver -> /devices/somedev/controls/paramTwo: [1] (QoS 1, retained)",
	)

	fixture.driver.Stop()
	fixture.broker.Verify(
		"stop: driver",
	)
	fixture.model.Verify()
}

func TestExternalDevices(t *testing.T) {
	fixture := newDriverFixture(t)
	fixture.driver.SetAcceptsExternalDevices(true)
	fixture.driver.Start()
	defer fixture.driver.Stop()

	fixture.client.Publish(MQTTMessage{"/devices/somedev/meta/name", "SomeDev", 1, true})
	fixture.broker.Verify(
		"Subscribe -- driver: /devices/+/meta/name",
		"Subscribe -- driver: /devices/+/controls/+",
		"Subscribe -- driver: /devices/+/controls/+/meta/type",
		"Subscribe -- driver: /devices/+/controls/+/meta/max",
		"tst -> /devices/somedev/meta/name: [SomeDev] (QoS 1, retained)",
	)
	WaitFor(t, func() bool {
		c := make(chan bool)
		fixture.driver.CallSync(func() {
			c <- fixture.model.HasDevice("somedev")
		})
		return <-c
	})
	dev := fixture.model.GetDevice("somedev")
	assert.NotEqual(t, nil, dev)

	fixture.client.Publish(MQTTMessage{"/devices/somedev/controls/paramOne", "42", 1, true})
	fixture.broker.Verify(
		"tst -> /devices/somedev/controls/paramOne: [42] (QoS 1, retained)",
	)
	fixture.model.Verify(
		"AcceptValue: somedev.paramOne = 42",
	)
	assert.Equal(t, "42", dev.GetValue("paramOne"))
	assert.Equal(t, "text", dev.GetType("paramOne"))

	fixture.client.Publish(MQTTMessage{"/devices/somedev/controls/paramOne/meta/type", "temperature", 1, true})
	fixture.broker.Verify(
		"tst -> /devices/somedev/controls/paramOne/meta/type: [temperature] (QoS 1, retained)",
	)
	fixture.model.Verify(
		"the type of somedev.paramOne is: temperature",
	)
	assert.Equal(t, "42", dev.GetValue("paramOne"))
	assert.Equal(t, "temperature", dev.GetType("paramOne"))

	fixture.client.Publish(MQTTMessage{"/devices/somedev/controls/paramTwo/meta/type", "pressure", 1, true})
	fixture.broker.Verify(
		"tst -> /devices/somedev/controls/paramTwo/meta/type: [pressure] (QoS 1, retained)",
	)
	fixture.model.Verify(
		"the type of somedev.paramTwo is: pressure",
	)
	assert.Equal(t, "", dev.GetValue("paramTwo"))
	assert.Equal(t, "pressure", dev.GetType("paramTwo"))

	// FIXME: should use separate 'range' cell
	fixture.client.Publish(MQTTMessage{"/devices/somedev/controls/paramTwo/meta/max", "1000", 1, true})
	fixture.broker.Verify(
		"tst -> /devices/somedev/controls/paramTwo/meta/max: [1000] (QoS 1, retained)",
	)
	fixture.model.Verify(
		"max value for somedev.paramTwo is: 1000",
	)

	fixture.client.Publish(MQTTMessage{"/devices/somedev/controls/paramTwo", "755", 1, true})
	fixture.broker.Verify(
		"tst -> /devices/somedev/controls/paramTwo: [755] (QoS 1, retained)",
	)
	fixture.model.Verify(
		"AcceptValue: somedev.paramTwo = 755",
	)
	assert.Equal(t, "755", dev.GetValue("paramTwo"))
	assert.Equal(t, "pressure", dev.GetType("paramTwo"))
}
