package wbgo

import (
	"fmt"
	"testing"
)

type DriverSuiteBase struct {
	Suite
	*FakeMQTTFixture
	model  *FakeModel
	client *FakeMQTTClient
	driver *Driver
}

func (s *DriverSuiteBase) SetupTest() {
	s.Suite.SetupTest()
	s.FakeMQTTFixture = NewFakeMQTTFixture(s.T())
	s.model = NewFakeModel(s.T())

	s.client = s.Broker.MakeClient("tst")
	s.client.Start()

	s.driver = NewDriver(s.model, s.Broker.MakeClient("driver"))
	s.driver.SetAutoPoll(false)
}

func (s *DriverSuiteBase) TearDownTest() {
	s.driver.Stop()
	s.Suite.TearDown()
}

func (s *DriverSuiteBase) createLocalDevice() *FakeLocalDevice {
	return s.model.MakeLocalVirtualDevice("somedev", "SomeDev", map[string]string{
		"paramOne": "switch",
		"paramTwo": "switch",
	})
}

func (s *DriverSuiteBase) createLocalDeviceSync() *FakeLocalDevice {
	// after the driver is started, new devices should be created
	// only within the driver's coroutine
	ch := make(chan *FakeLocalDevice)
	s.driver.CallSync(func() {
		ch <- s.createLocalDevice()
	})
	return <-ch
}

func (s *DriverSuiteBase) verifyLocalDevice(dev *FakeLocalDevice, invert bool) {
	v1, v2 := "0", "1"
	if invert {
		v1, v2 = "1", "0"
	}
	s.Verify(
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
		s.driver.Poll()
		s.model.Verify("poll")
	}

	s.client.Publish(MQTTMessage{"/devices/somedev/controls/paramOne/on", v2, 1, false})
	s.Verify(
		fmt.Sprintf("tst -> /devices/somedev/controls/paramOne/on: [%s] (QoS 1)", v2),
		fmt.Sprintf("driver -> /devices/somedev/controls/paramOne: [%s] (QoS 1, retained)", v2),
	)
	s.model.Verify(
		"AcceptOnValue: somedev.paramOne = " + v2,
	)

	s.driver.CallSync(func() {
		dev.ReceiveValue("paramTwo", v2)
	})
	s.Verify(
		fmt.Sprintf("driver -> /devices/somedev/controls/paramTwo: [%s] (QoS 1, retained)", v2),
	)
}

type LocalDriverSuite struct {
	DriverSuiteBase
}

func (s *LocalDriverSuite) TestDriver() {
	dev := s.createLocalDevice()
	s.driver.Start()
	s.verifyLocalDevice(dev, false)

	s.driver.Stop()
	s.Verify(
		"stop: driver",
	)
	s.model.Verify()
}

type ExtDriverSuite struct {
	DriverSuiteBase
}

func (s *ExtDriverSuite) SetupTest() {
	s.DriverSuiteBase.SetupTest()
	s.driver.SetAcceptsExternalDevices(true)
	s.driver.Start()

	s.Verify(
		"Subscribe -- driver: /devices/+/meta/name",
		"Subscribe -- driver: /devices/+/controls/+",
		"Subscribe -- driver: /devices/+/controls/+/meta/type",
		"Subscribe -- driver: /devices/+/controls/+/meta/max",
	)
	s.client.Publish(MQTTMessage{"/devices/somedev/meta/name", "SomeDev", 1, true})
	s.Verify(
		"tst -> /devices/somedev/meta/name: [SomeDev] (QoS 1, retained)",
	)
	WaitFor(s.T(), func() bool {
		c := make(chan bool)
		s.driver.CallSync(func() {
			c <- s.model.HasDevice("somedev")
		})
		return <-c
	})
}

func (s *ExtDriverSuite) TestExternalDevices() {
	dev := s.model.GetDevice("somedev")
	s.NotEqual(nil, dev)

	s.client.Publish(MQTTMessage{"/devices/somedev/controls/paramOne", "42", 1, true})
	s.Verify(
		"tst -> /devices/somedev/controls/paramOne: [42] (QoS 1, retained)",
	)
	s.model.Verify(
		"AcceptValue: somedev.paramOne = 42",
	)
	s.Equal("42", dev.GetValue("paramOne"))
	s.Equal("text", dev.GetType("paramOne"))

	s.client.Publish(MQTTMessage{"/devices/somedev/controls/paramOne/meta/type", "temperature", 1, true})
	s.Verify(
		"tst -> /devices/somedev/controls/paramOne/meta/type: [temperature] (QoS 1, retained)",
	)
	s.model.Verify(
		"the type of somedev.paramOne is: temperature",
	)
	s.Equal("42", dev.GetValue("paramOne"))
	s.Equal("temperature", dev.GetType("paramOne"))

	s.client.Publish(MQTTMessage{"/devices/somedev/controls/paramTwo/meta/type", "pressure", 1, true})
	s.Verify(
		"tst -> /devices/somedev/controls/paramTwo/meta/type: [pressure] (QoS 1, retained)",
	)
	s.model.Verify(
		"the type of somedev.paramTwo is: pressure",
	)
	s.Equal("", dev.GetValue("paramTwo"))
	s.Equal("pressure", dev.GetType("paramTwo"))

	// FIXME: should use separate 'range' cell
	s.client.Publish(MQTTMessage{"/devices/somedev/controls/paramTwo/meta/max", "1000", 1, true})
	s.Verify(
		"tst -> /devices/somedev/controls/paramTwo/meta/max: [1000] (QoS 1, retained)",
	)
	s.model.Verify(
		"max value for somedev.paramTwo is: 1000",
	)

	s.client.Publish(MQTTMessage{"/devices/somedev/controls/paramTwo", "755", 1, true})
	s.Verify(
		"tst -> /devices/somedev/controls/paramTwo: [755] (QoS 1, retained)",
	)
	s.model.Verify(
		"AcceptValue: somedev.paramTwo = 755",
	)
	s.Equal("755", dev.GetValue("paramTwo"))
	s.Equal("pressure", dev.GetType("paramTwo"))
}

func (s *ExtDriverSuite) TestConvertRemoteToLocal() {
	dev := s.createLocalDeviceSync()
	s.verifyLocalDevice(dev, false)
}

func (s *ExtDriverSuite) TestLocalDeviceRedefinition() {
	dev1 := s.createLocalDeviceSync()
	s.verifyLocalDevice(dev1, false)

	Debug.Printf("----------------------------------------")

	dev2 := s.createLocalDeviceSync()

	// old device removal causes unsubscription
	s.Verify(
		"Unsubscribe -- driver: /devices/somedev/controls/paramOne/on",
		"Unsubscribe -- driver: /devices/somedev/controls/paramTwo/on",
	)

	s.verifyLocalDevice(dev2, true)
}

func TestDriverSuite(t *testing.T) {
	RunSuite(t, new(LocalDriverSuite))
	RunSuite(t, new(ExtDriverSuite))
}

// TBD: test non-virtual devices (local devices which don't pick up retained values)
