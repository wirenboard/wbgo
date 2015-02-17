package wbgo

import (
	"log"
	"sort"
	"testing"
)

type FakeModel struct {
	Recorder
	ModelBase
	devices map[string]*FakeDevice
}

type FakeDevice struct {
	DeviceBase
	model *FakeModel
	paramTypes map[string]string
	paramValues map[string]string
}

func NewFakeModel(t *testing.T) (model *FakeModel) {
	model = &FakeModel{devices: make(map[string]*FakeDevice)}
	model.InitRecorder(t)
	return
}

func (model *FakeModel) Poll () {
	model.Rec("poll")
}

func (model *FakeModel) Start() error {
	names := make([]string, 0, len(model.devices))
	for name := range model.devices {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		dev := model.devices[name]
		model.Observer.OnNewDevice(dev)
		dev.QueryParams()
	}
	return nil
}

func (model *FakeModel) MakeDevice(name string, title string,
	paramTypes map[string]string) (dev *FakeDevice) {
	if _, dup := model.devices[name]; dup {
		// MakeDevice may be invoked not from the
		// test goroutine, but rather from driver's
		// primary goroutine, so can't use testing's
		// Fatalf here
		log.Panicf("duplicate device name %s", name)
	}
	dev = &FakeDevice{
		model: model,
		paramTypes: make(map[string]string),
		paramValues: make(map[string]string),
	}
	dev.DevName = name
	dev.DevTitle = title
	for k, v := range paramTypes {
		dev.paramTypes[k] = v
		dev.paramValues[k] = "0"
	}
	model.devices[name] = dev
	return
}

func (model *FakeModel) GetDevice(name string) *FakeDevice {
	if dev, found := model.devices[name]; !found {
		dev.model.T().Fatalf("unknown device %s", name)
		return nil
	} else {
		return dev
	}
}

func (dev *FakeDevice) SendValue(name, value string) bool {
	if _, found := dev.paramTypes[name]; !found {
		// cannot use dev.model.T().Fatalf() here because
		// SendValue is invoked from goroutine other
		// than the one running the test
		log.Panicf("trying to send unknown param %s (value %s)",
			name, value)
	}
	dev.paramValues[name] = value
	dev.model.Rec("send: %s.%s = %s", dev.DevName, name, value)
	return true
}

func (dev *FakeDevice) QueryParams() {
	for k, v := range dev.paramTypes {
		dev.Observer.OnNewControl(dev, k, v, dev.paramValues[k], false)
	}
}

func (dev *FakeDevice) ReceiveValue(name, value string) {
	if _, found := dev.paramValues[name]; !found {
		dev.model.T().Fatalf("trying to send unknown param %s (value %s)",
			name, value)
	} else {
		dev.paramValues[name] = value
		dev.Observer.OnValue(dev, name, value)
	}
}
