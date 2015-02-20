package wbgo

import (
	"testing"
	"github.com/stretchr/testify/assert"
)

func doTestDriver(t *testing.T,
	thunk func (driver *Driver, broker *FakeMQTTBroker, client MQTTClient, model *FakeModel)) {
	broker := NewFakeMQTTBroker(t)
	model := NewFakeModel(t)

	client := broker.MakeClient("tst")
	client.Start()

	driver := NewDriver(model, broker.MakeClient("driver"))
	driver.SetAutoPoll(false)
	thunk(driver, broker, client, model)
}

func TestDriver(t *testing.T) {
	doTestDriver(t, func (driver *Driver, broker *FakeMQTTBroker, client MQTTClient, model *FakeModel) {
		dev := model.MakeDevice("somedev", "SomeDev", map[string]string {
			"paramOne": "switch",
			"paramTwo": "switch",
		})
		driver.Start()

		broker.Verify(
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
			driver.Poll()
			model.Verify("poll")
		}

		client.Publish(MQTTMessage{"/devices/somedev/controls/paramOne/on", "1", 1, false})
		broker.Verify(
			"tst -> /devices/somedev/controls/paramOne/on: [1] (QoS 1)",
			"driver -> /devices/somedev/controls/paramOne: [1] (QoS 1, retained)",
		)
		model.Verify(
			"send: somedev.paramOne = 1",
		)

		driver.CallSync(func () {
			dev.ReceiveValue("paramTwo", "1")
		})
		broker.Verify(
			"driver -> /devices/somedev/controls/paramTwo: [1] (QoS 1, retained)",
		)

		driver.Stop()
		broker.Verify(
			"stop: driver",
		)
		model.Verify()
	})
}

func TestExternalDevices(t *testing.T) {
	doTestDriver(t, func (driver *Driver, broker *FakeMQTTBroker, client MQTTClient, model *FakeModel) {
		driver.SetAcceptsExternalDevices(true)
		driver.Start()
		defer driver.Stop()
		client.Publish(MQTTMessage{"/devices/somedev/meta/name", "SomeDev", 1, true})
		broker.Verify(
			"Subscribe -- driver: /devices/+/meta/name",
			"Subscribe -- driver: /devices/+/controls/+",
			"Subscribe -- driver: /devices/+/controls/+/meta/type",
			"tst -> /devices/somedev/meta/name: [SomeDev] (QoS 1, retained)",
		)
		WaitFor(t, func () bool {
			return model.HasDevice("somedev")
		})
		dev := model.GetDevice("somedev")
		assert.NotEqual(t, nil, dev)

		client.Publish(MQTTMessage{"/devices/somedev/controls/paramOne", "42", 1, true})
		broker.Verify(
			"tst -> /devices/somedev/controls/paramOne: [42] (QoS 1, retained)",
		)
		model.Verify(
			"send: somedev.paramOne = 42",
		)
		assert.Equal(t, "42", dev.GetValue("paramOne"))
		assert.Equal(t, "text", dev.GetType("paramOne"))

		client.Publish(MQTTMessage{"/devices/somedev/controls/paramOne/meta/type", "temperature", 1, true})
		broker.Verify(
			"tst -> /devices/somedev/controls/paramOne/meta/type: [temperature] (QoS 1, retained)",
		)
		model.Verify(
			"the type of somedev.paramOne is: temperature",
		)
		assert.Equal(t, "42", dev.GetValue("paramOne"))
		assert.Equal(t, "temperature", dev.GetType("paramOne"))

		client.Publish(MQTTMessage{"/devices/somedev/controls/paramTwo/meta/type", "pressure", 1, true})
		broker.Verify(
			"tst -> /devices/somedev/controls/paramTwo/meta/type: [pressure] (QoS 1, retained)",
		)
		model.Verify(
			"the type of somedev.paramTwo is: pressure",
		)
		assert.Equal(t, "", dev.GetValue("paramTwo"))
		assert.Equal(t, "pressure", dev.GetType("paramTwo"))

		client.Publish(MQTTMessage{"/devices/somedev/controls/paramTwo", "755", 1, true})
		broker.Verify(
			"tst -> /devices/somedev/controls/paramTwo: [755] (QoS 1, retained)",
		)
		model.Verify(
			"send: somedev.paramTwo = 755",
		)
		assert.Equal(t, "755", dev.GetValue("paramTwo"))
		assert.Equal(t, "pressure", dev.GetType("paramTwo"))
	})
}
