// FIXME: !!! TBD: !!! On* stuff should only be called in the context of primary goroutine.
package wbgo

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

const (
	EVENT_QUEUE_LEN          = 100
	DEFAULT_POLL_INTERVAL_MS = 5000
)

type MQTTMessage struct {
	Topic    string
	Payload  string
	QoS      byte
	Retained bool
}

type MQTTMessageHandler func(message MQTTMessage)

type MQTTClient interface {
	WaitForReady() <-chan struct{}
	Start()
	Stop()
	Publish(message MQTTMessage)
	Subscribe(callback MQTTMessageHandler, topics ...string)
	Unsubscribe(topics ...string)
}

type Model interface {
	Start() error
	Stop()
	Observe(observer ModelObserver)
	Poll()
}

// ExtendedModel is a Model that supports external devices
type ExtendedModel interface {
	Model
	AddExternalDevice(name string) (ExternalDeviceModel, error)
}

type DeviceModel interface {
	Name() string
	Title() string
	Observe(observer DeviceObserver)
	// AcceptOnValue accepts the specified control value sent via an MQTT value topic
	// (not .../on). For local devices, that can be a retained value, for external
	// devices, the current control value
	AcceptValue(name, value string)
}

type LocalDeviceModel interface {
	DeviceModel
	// AcceptOnValue accepts the specified control value sent via an MQTT .../on topic
	// for the target device and returns true if the value should be automatically
	// echoed back
	AcceptOnValue(name, value string) bool
}

type ExternalDeviceModel interface {
	DeviceModel
	SetTitle(title string)
	AcceptControlType(name, controlType string)
	AcceptControlRange(name string, max float64)
}

// TBD: rename ModelObserver(?) (it's not just an observer)

type ModelObserver interface {
	CallSync(thunk func())
	WhenReady(thunk func())
	OnNewDevice(dev DeviceModel)
}

type DeviceObserver interface {
	OnNewControl(dev DeviceModel, name, paramType, value string, readOnly bool, max float64)
	OnValue(dev DeviceModel, name, value string)
}

type ModelBase struct {
	Observer ModelObserver
}

func (model *ModelBase) Observe(observer ModelObserver) {
	model.Observer = observer
}

func (model *ModelBase) Poll() {}

func (model *ModelBase) Stop() {}

type DeviceBase struct {
	DevName  string
	DevTitle string
	Observer DeviceObserver
}

func (dev *DeviceBase) Name() string {
	return dev.DevName
}

func (dev *DeviceBase) Title() string {
	return dev.DevTitle
}

func (dev *DeviceBase) SetTitle(title string) {
	dev.DevTitle = title
}

func (dev *DeviceBase) Observe(observer DeviceObserver) {
	dev.Observer = observer
}

// Driver transfers data between Model with MQTTClient
type Driver struct {
	model                  Model
	client                 MQTTClient
	eventCh                chan func()
	quit                   chan struct{}
	poll                   chan time.Time
	deviceMap              map[string]DeviceModel
	nextOrder              map[string]int
	autoPoll               bool
	pollIntervalMs         int
	acceptsExternalDevices bool
	active                 bool
	ready                  bool
	whenReady              *DeferredList
}

func NewDriver(model Model, client MQTTClient) (drv *Driver) {
	drv = &Driver{
		model:  model,
		client: client,
		// Actually EVENT_QUEUE_LEN > 0 is only needed
		// to avoid deadlocks in tests in a case when
		// model change causes MQTT message to be generated
		// that is passed back to the model
		eventCh:        make(chan func(), EVENT_QUEUE_LEN),
		quit:           make(chan struct{}),
		poll:           make(chan time.Time),
		nextOrder:      make(map[string]int),
		deviceMap:      make(map[string]DeviceModel),
		autoPoll:       true,
		pollIntervalMs: DEFAULT_POLL_INTERVAL_MS,
	}
	drv.whenReady = NewDeferredList(drv.CallSync)
	drv.model.Observe(drv)
	return
}

func (drv *Driver) SetAutoPoll(autoPoll bool) {
	drv.autoPoll = autoPoll
}

func (drv *Driver) AutoPoll() bool {
	return drv.autoPoll
}

func (drv *Driver) SetPollInterval(pollIntervalMs int) {
	drv.pollIntervalMs = pollIntervalMs
}

func (drv *Driver) PollInterval() int {
	return drv.pollIntervalMs
}

func (drv *Driver) Poll() {
	drv.poll <- time.Now()
}

func (drv *Driver) topic(dev DeviceModel, sub ...string) string {
	parts := append(append([]string(nil), "/devices", dev.Name()), sub...)
	return strings.Join(parts, "/")
}

func (drv *Driver) controlTopic(dev DeviceModel, controlName string, sub ...string) string {
	parts := append(append([]string(nil), "controls", controlName), sub...)
	return drv.topic(dev, parts...)
}

func (drv *Driver) publish(topic, payload string, qos byte) {
	drv.client.Publish(MQTTMessage{topic, payload, qos, true})
}

func (drv *Driver) publishMeta(topic string, payload string) {
	drv.publish(topic, payload, 1)
}

func (drv *Driver) publishValue(dev DeviceModel, controlName, value string) {
	drv.publish(drv.controlTopic(dev, controlName), value, 1)
}

func (drv *Driver) publishOnValue(dev DeviceModel, controlName, value string) {
	drv.client.Publish(MQTTMessage{
		drv.controlTopic(dev, controlName) + "/on",
		value,
		1,
		false,
	})
}

func (drv *Driver) OnNewDevice(dev DeviceModel) {
	// this overrides a possibly created external device with same name
	drv.deviceMap[dev.Name()] = dev
	if _, ext := dev.(ExternalDeviceModel); !ext {
		drv.publishMeta(drv.topic(dev, "meta", "name"), dev.Title())
	}
	dev.Observe(drv)
}

// wrapMessageHandler wraps the message function so that it's run in
// the driver's primary goroutine
func (drv *Driver) wrapMessageHandler(handler MQTTMessageHandler) MQTTMessageHandler {
	return func(msg MQTTMessage) {
		drv.CallSync(func() {
			handler(msg)
		})
	}
}

func (drv *Driver) subscribe(handler MQTTMessageHandler, topics ...string) {
	drv.client.Subscribe(drv.wrapMessageHandler(handler), topics...)
}

func (drv *Driver) OnNewControl(dev DeviceModel, controlName, paramType, value string, readOnly bool, max float64) {
	devName := dev.Name()
	nextOrder, found := drv.nextOrder[devName]
	if !found {
		nextOrder = 1
	}
	drv.publishMeta(drv.controlTopic(dev, controlName, "meta", "type"), paramType)
	drv.publishMeta(drv.controlTopic(dev, controlName, "meta", "order"),
		strconv.Itoa(nextOrder))
	if max >= 0 {
		drv.publishMeta(drv.controlTopic(dev, controlName, "meta", "max"),
			fmt.Sprintf("%v", max))
	}
	drv.nextOrder[devName] = nextOrder + 1
	drv.publishValue(dev, controlName, value)
	if !readOnly {
		Debug.Println("subscribe to: %s", drv.controlTopic(dev, controlName, "on"))
		drv.subscribe(
			drv.handleIncomingControlOnValue,
			drv.controlTopic(dev, controlName, "on"))
	}
}

func (drv *Driver) OnValue(dev DeviceModel, controlName, value string) {
	if _, ext := dev.(ExternalDeviceModel); ext {
		drv.publishOnValue(dev, controlName, value)
	} else {
		drv.publishValue(dev, controlName, value)
	}
}

func (drv *Driver) ensureDevice(deviceName string) (DeviceModel, error) {
	dev, found := drv.deviceMap[deviceName]
	if found {
		return dev, nil
	}

	if !drv.acceptsExternalDevices {
		return nil, fmt.Errorf("unknown device: %s", deviceName)
	}

	extModel := drv.model.(ExtendedModel)
	if dev, err := extModel.AddExternalDevice(deviceName); err != nil {
		return nil, err
	} else {
		drv.deviceMap[deviceName] = dev
		return dev, nil
	}
}

func (drv *Driver) ensureExtDevice(deviceName string) (ExternalDeviceModel, error) {
	dev, err := drv.ensureDevice(deviceName)
	if err != nil {
		return nil, err
	}

	extDev, ok := dev.(ExternalDeviceModel)
	if ok {
		return extDev, nil
	} else {
		return nil, nil
	}
}

func (drv *Driver) handleIncomingControlOnValue(msg MQTTMessage) {
	// /devices/<name>/controls/<control>/on
	Debug.Printf("handleIncomingMQTTValue() topic: %s", msg.Topic)
	Debug.Printf("MSG: %s\n", msg.Payload)
	parts := strings.Split(msg.Topic, "/")
	deviceName := parts[2]
	controlName := parts[4]
	dev, found := drv.deviceMap[deviceName]
	if !found {
		Error.Printf("UNKNOWN DEVICE: %s", deviceName)
		return
	}
	if dev.(LocalDeviceModel).AcceptOnValue(controlName, msg.Payload) {
		drv.publishValue(dev, controlName, msg.Payload)
	}
}

func (drv *Driver) handleDeviceTitle(msg MQTTMessage) {
	deviceName := strings.Split(msg.Topic, "/")[2]
	dev, err := drv.ensureExtDevice(deviceName)
	if err != nil {
		Warn.Printf("Not registering external device %s: %s", deviceName, err)
	}
	if dev != nil { // nil would mean a local device
		dev.SetTitle(msg.Payload)
	}
}

func (drv *Driver) handleIncomingControlValue(msg MQTTMessage) {
	// /devices/<name>/controls/<control>
	parts := strings.Split(msg.Topic, "/")
	deviceName := parts[2]
	controlName := parts[4]
	var dev DeviceModel
	var err error
	if drv.ready {
		dev, err = drv.ensureExtDevice(deviceName)
	} else {
		// not ready yet - may accept retained values for local devices
		dev, err = drv.ensureDevice(deviceName)
	}
	if err != nil {
		Error.Printf("Cannot register external device %s: %s", deviceName, err)
	}
	if dev != nil { // nil would mean a local device
		dev.AcceptValue(controlName, msg.Payload)
	}
}

func (drv *Driver) handleExternalControlType(msg MQTTMessage) {
	// /devices/<name>/controls/<control>
	parts := strings.Split(msg.Topic, "/")
	deviceName := parts[2]
	controlName := parts[4]
	dev, err := drv.ensureExtDevice(deviceName)
	if err != nil {
		Error.Printf("Cannot register external device %s: %s", deviceName, err)
	}
	if dev != nil { // nil would mean a local device
		dev.AcceptControlType(controlName, msg.Payload)
	}
}

func (drv *Driver) handleExternalControlMax(msg MQTTMessage) {
	// /devices/<name>/controls/<control>/meta/max
	parts := strings.Split(msg.Topic, "/")
	deviceName := parts[2]
	controlName := parts[4]
	dev, err := drv.ensureExtDevice(deviceName)
	if err != nil {
		Error.Printf("Cannot register external device %s: %s", deviceName, err)
		return
	}
	if dev == nil { // nil would mean a local device
		return
	}
	max, err := strconv.ParseFloat(msg.Payload, 64)
	if err != nil {
		Error.Printf("Cannot parse max value for device %s control %s", deviceName, controlName)
		return
	}
	dev.AcceptControlRange(controlName, max)
}

func (drv *Driver) AcceptsExternalDevices() bool {
	return drv.acceptsExternalDevices
}

func (drv *Driver) SetAcceptsExternalDevices(accepts bool) {
	if drv.active {
		panic("trying to do SetAcceptsExternalDevices() on an active driver")
	}
	drv.acceptsExternalDevices = accepts
}

func (drv *Driver) CallSync(thunk func()) {
	drv.eventCh <- thunk
}

func (drv *Driver) WhenReady(thunk func()) {
	drv.whenReady.MaybeDefer(thunk)
}

func (drv *Driver) Start() error {
	if drv.active {
		return nil
	}
	drv.active = true
	drv.client.Start()
	if err := drv.model.Start(); err != nil {
		return err
	}
	var ticker *time.Ticker
	var pollChannel <-chan time.Time = drv.poll
	if drv.autoPoll {
		ticker = time.NewTicker(time.Duration(drv.pollIntervalMs) * time.Millisecond)
		pollChannel = ticker.C
	}

	if drv.acceptsExternalDevices {
		drv.subscribe(drv.handleDeviceTitle, "/devices/+/meta/name")
		drv.subscribe(drv.handleIncomingControlValue, "/devices/+/controls/+")
		drv.subscribe(drv.handleExternalControlType, "/devices/+/controls/+/meta/type")
		drv.subscribe(drv.handleExternalControlMax, "/devices/+/controls/+/meta/max")
	}

	go func() {
		readyCh := drv.client.WaitForReady()
		for {
			select {
			case <-readyCh:
				drv.whenReady.Ready()
				readyCh = nil
				drv.ready = true
			case <-drv.quit:
				Debug.Printf("Driver: stopping")
				if ticker != nil {
					ticker.Stop()
				}
				drv.model.Stop()
				drv.client.Stop()
				return
			case <-pollChannel:
				drv.model.Poll()
			case f := <-drv.eventCh:
				f()
			}
		}
	}()
	return nil
}

func (drv *Driver) Stop() {
	if !drv.active {
		return
	}
	Debug.Printf("Driver.Stop()")
	drv.quit <- struct{}{}
}
