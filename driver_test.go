package wbgo

import (
	"fmt"
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
	SetupTestLogging(t)

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

func (fixture *driverFixture) createLocalDevice() *FakeLocalDevice {
	return fixture.model.MakeLocalVirtualDevice("somedev", "SomeDev", map[string]string{
		"paramOne": "switch",
		"paramTwo": "switch",
	})
}

func (fixture *driverFixture) createLocalDeviceSync() *FakeLocalDevice {
	// after the driver is started, new devices should be created
	// only within the driver's coroutine
	ch := make(chan *FakeLocalDevice)
	fixture.driver.CallSync(func() {
		ch <- fixture.createLocalDevice()
	})
	return <-ch
}

func (fixture *driverFixture) verifyLocalDevice(dev *FakeLocalDevice, invert bool) {
	v1, v2 := "0", "1"
	if invert {
		v1, v2 = "1", "0"
	}
	fixture.broker.Verify(
		"driver -> /devices/somedev/meta/name: [SomeDev] (QoS 1, retained)",
		"driver -> /devices/somedev/controls/paramOne/meta/type: [switch] (QoS 1, retained)",
		"driver -> /devices/somedev/controls/paramOne/meta/order: [1] (QoS 1, retained)",
		fmt.Sprintf("driver -> /devices/somedev/controls/paramOne: [%s] (QoS 1, retained)", v1),
		"Subscribe -- driver: /devices/somedev/controls/paramOne/on",
		"driver -> /devices/somedev/controls/paramTwo/meta/type: [switch] (QoS 1, retained)",
		"driver -> /devices/somedev/controls/paramTwo/meta/order: [2] (QoS 1, retained)",
		fmt.Sprintf("driver -> /devices/somedev/controls/paramTwo: [%s] (QoS 1, retained)", v1),
		"Subscribe -- driver: /devices/somedev/controls/paramTwo/on",
	)

	for i := 0; i < 3; i++ {
		fixture.driver.Poll()
		fixture.model.Verify("poll")
	}

	fixture.client.Publish(MQTTMessage{"/devices/somedev/controls/paramOne/on", v2, 1, false})
	fixture.broker.Verify(
		fmt.Sprintf("tst -> /devices/somedev/controls/paramOne/on: [%s] (QoS 1)", v2),
		fmt.Sprintf("driver -> /devices/somedev/controls/paramOne: [%s] (QoS 1, retained)", v2),
	)
	fixture.model.Verify(
		"AcceptOnValue: somedev.paramOne = " + v2,
	)

	fixture.driver.CallSync(func() {
		dev.ReceiveValue("paramTwo", v2)
	})
	fixture.broker.Verify(
		fmt.Sprintf("driver -> /devices/somedev/controls/paramTwo: [%s] (QoS 1, retained)", v2),
	)
}

func TestDriver(t *testing.T) {
	fixture := newDriverFixture(t)
	dev := fixture.createLocalDevice()
	fixture.driver.Start()
	fixture.verifyLocalDevice(dev, false)

	fixture.driver.Stop()
	fixture.broker.Verify(
		"stop: driver",
	)
	fixture.model.Verify()
}

func (fixture *driverFixture) extDevSetup() {
	fixture.driver.SetAcceptsExternalDevices(true)
	fixture.driver.Start()

	fixture.broker.Verify(
		"Subscribe -- driver: /devices/+/meta/name",
		"Subscribe -- driver: /devices/+/controls/+",
		"Subscribe -- driver: /devices/+/controls/+/meta/type",
		"Subscribe -- driver: /devices/+/controls/+/meta/max",
	)
	fixture.client.Publish(MQTTMessage{"/devices/somedev/meta/name", "SomeDev", 1, true})
	fixture.broker.Verify(
		"tst -> /devices/somedev/meta/name: [SomeDev] (QoS 1, retained)",
	)
	WaitFor(fixture.t, func() bool {
		c := make(chan bool)
		fixture.driver.CallSync(func() {
			c <- fixture.model.HasDevice("somedev")
		})
		return <-c
	})
}

func TestExternalDevices(t *testing.T) {
	fixture := newDriverFixture(t)
	fixture.extDevSetup()
	defer fixture.driver.Stop()

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

func TestConvertRemoteToLocal(t *testing.T) {
	fixture := newDriverFixture(t)
	fixture.extDevSetup()
	defer fixture.driver.Stop()

	dev := fixture.createLocalDeviceSync()
	fixture.verifyLocalDevice(dev, false)
}

func TestLocalDeviceRedefinition(t *testing.T) {
	fixture := newDriverFixture(t)
	fixture.extDevSetup()
	defer fixture.driver.Stop()

	dev1 := fixture.createLocalDeviceSync()
	fixture.verifyLocalDevice(dev1, false)

	Debug.Printf("----------------------------------------")

	dev2 := fixture.createLocalDeviceSync()

	// old device removal causes unsubscription
	fixture.broker.Verify(
		"Unsubscribe -- driver: /devices/somedev/controls/paramOne/on",
		"Unsubscribe -- driver: /devices/somedev/controls/paramTwo/on",
	)

	fixture.verifyLocalDevice(dev2, true)
}

// TBD: test non-virtual devices (local devices which don't pick up retained values)
