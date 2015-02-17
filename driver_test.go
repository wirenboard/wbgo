package wbgo

import (
	"testing"
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

		dev.ReceiveValue("paramTwo", "1")
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
