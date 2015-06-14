package wbgo

import (
	"fmt"
	"github.com/stretchr/testify/suite"
	"testing"
)

type DriverSuiteBase struct {
	suite.Suite
	broker *FakeMQTTBroker
	model  *FakeModel
	client *FakeMQTTClient
	driver *Driver
}

func (suite *DriverSuiteBase) SetupTest() {
	SetupTestLogging(suite.T())

	suite.broker = NewFakeMQTTBroker(suite.T())
	suite.model = NewFakeModel(suite.T())

	suite.client = suite.broker.MakeClient("tst")
	suite.client.Start()

	suite.driver = NewDriver(suite.model, suite.broker.MakeClient("driver"))
	suite.driver.SetAutoPoll(false)
}

func (suite *DriverSuiteBase) TearDownTest() {
	suite.driver.Stop()
}

func (suite *DriverSuiteBase) createLocalDevice() *FakeLocalDevice {
	return suite.model.MakeLocalVirtualDevice("somedev", "SomeDev", map[string]string{
		"paramOne": "switch",
		"paramTwo": "switch",
	})
}

func (suite *DriverSuiteBase) createLocalDeviceSync() *FakeLocalDevice {
	// after the driver is started, new devices should be created
	// only within the driver's coroutine
	ch := make(chan *FakeLocalDevice)
	suite.driver.CallSync(func() {
		ch <- suite.createLocalDevice()
	})
	return <-ch
}

func (suite *DriverSuiteBase) verifyLocalDevice(dev *FakeLocalDevice, invert bool) {
	v1, v2 := "0", "1"
	if invert {
		v1, v2 = "1", "0"
	}
	suite.broker.Verify(
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
		suite.driver.Poll()
		suite.model.Verify("poll")
	}

	suite.client.Publish(MQTTMessage{"/devices/somedev/controls/paramOne/on", v2, 1, false})
	suite.broker.Verify(
		fmt.Sprintf("tst -> /devices/somedev/controls/paramOne/on: [%s] (QoS 1)", v2),
		fmt.Sprintf("driver -> /devices/somedev/controls/paramOne: [%s] (QoS 1, retained)", v2),
	)
	suite.model.Verify(
		"AcceptOnValue: somedev.paramOne = " + v2,
	)

	suite.driver.CallSync(func() {
		dev.ReceiveValue("paramTwo", v2)
	})
	suite.broker.Verify(
		fmt.Sprintf("driver -> /devices/somedev/controls/paramTwo: [%s] (QoS 1, retained)", v2),
	)
}

type LocalDriverSuite struct {
	DriverSuiteBase
}

func (suite *LocalDriverSuite) TestDriver() {
	dev := suite.createLocalDevice()
	suite.driver.Start()
	suite.verifyLocalDevice(dev, false)

	suite.driver.Stop()
	suite.broker.Verify(
		"stop: driver",
	)
	suite.model.Verify()
}

type ExtDriverSuite struct {
	DriverSuiteBase
}

func (suite *ExtDriverSuite) SetupTest() {
	suite.DriverSuiteBase.SetupTest()
	suite.driver.SetAcceptsExternalDevices(true)
	suite.driver.Start()

	suite.broker.Verify(
		"Subscribe -- driver: /devices/+/meta/name",
		"Subscribe -- driver: /devices/+/controls/+",
		"Subscribe -- driver: /devices/+/controls/+/meta/type",
		"Subscribe -- driver: /devices/+/controls/+/meta/max",
	)
	suite.client.Publish(MQTTMessage{"/devices/somedev/meta/name", "SomeDev", 1, true})
	suite.broker.Verify(
		"tst -> /devices/somedev/meta/name: [SomeDev] (QoS 1, retained)",
	)
	WaitFor(suite.T(), func() bool {
		c := make(chan bool)
		suite.driver.CallSync(func() {
			c <- suite.model.HasDevice("somedev")
		})
		return <-c
	})
}

func (suite *ExtDriverSuite) TestExternalDevices() {
	dev := suite.model.GetDevice("somedev")
	suite.NotEqual(nil, dev)

	suite.client.Publish(MQTTMessage{"/devices/somedev/controls/paramOne", "42", 1, true})
	suite.broker.Verify(
		"tst -> /devices/somedev/controls/paramOne: [42] (QoS 1, retained)",
	)
	suite.model.Verify(
		"AcceptValue: somedev.paramOne = 42",
	)
	suite.Equal("42", dev.GetValue("paramOne"))
	suite.Equal("text", dev.GetType("paramOne"))

	suite.client.Publish(MQTTMessage{"/devices/somedev/controls/paramOne/meta/type", "temperature", 1, true})
	suite.broker.Verify(
		"tst -> /devices/somedev/controls/paramOne/meta/type: [temperature] (QoS 1, retained)",
	)
	suite.model.Verify(
		"the type of somedev.paramOne is: temperature",
	)
	suite.Equal("42", dev.GetValue("paramOne"))
	suite.Equal("temperature", dev.GetType("paramOne"))

	suite.client.Publish(MQTTMessage{"/devices/somedev/controls/paramTwo/meta/type", "pressure", 1, true})
	suite.broker.Verify(
		"tst -> /devices/somedev/controls/paramTwo/meta/type: [pressure] (QoS 1, retained)",
	)
	suite.model.Verify(
		"the type of somedev.paramTwo is: pressure",
	)
	suite.Equal("", dev.GetValue("paramTwo"))
	suite.Equal("pressure", dev.GetType("paramTwo"))

	// FIXME: should use separate 'range' cell
	suite.client.Publish(MQTTMessage{"/devices/somedev/controls/paramTwo/meta/max", "1000", 1, true})
	suite.broker.Verify(
		"tst -> /devices/somedev/controls/paramTwo/meta/max: [1000] (QoS 1, retained)",
	)
	suite.model.Verify(
		"max value for somedev.paramTwo is: 1000",
	)

	suite.client.Publish(MQTTMessage{"/devices/somedev/controls/paramTwo", "755", 1, true})
	suite.broker.Verify(
		"tst -> /devices/somedev/controls/paramTwo: [755] (QoS 1, retained)",
	)
	suite.model.Verify(
		"AcceptValue: somedev.paramTwo = 755",
	)
	suite.Equal("755", dev.GetValue("paramTwo"))
	suite.Equal("pressure", dev.GetType("paramTwo"))
}

func (suite *ExtDriverSuite) TestConvertRemoteToLocal() {
	dev := suite.createLocalDeviceSync()
	suite.verifyLocalDevice(dev, false)
}

func (suite *ExtDriverSuite) TestLocalDeviceRedefinition() {
	dev1 := suite.createLocalDeviceSync()
	suite.verifyLocalDevice(dev1, false)

	Debug.Printf("----------------------------------------")

	dev2 := suite.createLocalDeviceSync()

	// old device removal causes unsubscription
	suite.broker.Verify(
		"Unsubscribe -- driver: /devices/somedev/controls/paramOne/on",
		"zUnsubscribe -- driver: /devices/somedev/controls/paramTwo/on",
	)

	suite.verifyLocalDevice(dev2, true)
}

func TestDriverSuite(t *testing.T) {
	suite.Run(t, new(LocalDriverSuite))
	suite.Run(t, new(ExtDriverSuite))
}

// TBD: wbgo.Suite
// TBD: test non-virtual devices (local devices which don't pick up retained values)
