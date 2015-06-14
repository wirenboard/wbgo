package wbgo

import (
	"sort"
	"testing"
)

type FakeModel struct {
	*Recorder
	ModelBase
	devices map[string]FakeDev
	started bool
}

type FakeDev interface {
	DeviceModel
	GetValue(name string) string
	GetType(name string) string
}

type FakeDevice struct {
	DeviceBase
	model       *FakeModel
	paramTypes  map[string]string
	paramValues map[string]string
}

type FakeLocalDevice struct {
	FakeDevice
	virtual bool
}

type FakeExtDevice struct {
	FakeDevice
}

func NewFakeModel(t *testing.T) (model *FakeModel) {
	model = &FakeModel{
		Recorder: NewRecorder(t),
		devices:  make(map[string]FakeDev),
		started:  false,
	}
	return
}

func (model *FakeModel) Poll() {
	model.Rec("poll")
}

func (model *FakeModel) Start() error {
	names := make([]string, 0, len(model.devices))
	for name := range model.devices {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		dev, ok := model.devices[name].(LocalDeviceModel)
		if ok {
			model.Observer.OnNewDevice(dev)
			dev.(*FakeLocalDevice).QueryParams()
		}
	}
	model.started = true
	return nil
}

func newFakeDevice(model *FakeModel, name string, title string) (dev *FakeDevice) {
	dev = &FakeDevice{
		model:       model,
		paramTypes:  make(map[string]string),
		paramValues: make(map[string]string),
	}
	dev.DevName = name
	dev.DevTitle = title
	return dev
}

func (model *FakeModel) makeLocalDevice(name string, title string,
	paramTypes map[string]string, virtual bool) (dev *FakeLocalDevice) {
	dev = &FakeLocalDevice{*newFakeDevice(model, name, title), virtual}
	for k, v := range paramTypes {
		dev.paramTypes[k] = v
		dev.paramValues[k] = "0"
	}
	model.devices[name] = dev
	if model.started {
		model.Observer.OnNewDevice(dev)
		dev.QueryParams()
	}
	return
}

func (model *FakeModel) MakeLocalDevice(name string, title string,
	paramTypes map[string]string) (dev *FakeLocalDevice) {
	return model.makeLocalDevice(name, title, paramTypes, false)
}

func (model *FakeModel) MakeLocalVirtualDevice(name string, title string,
	paramTypes map[string]string) (dev *FakeLocalDevice) {
	return model.makeLocalDevice(name, title, paramTypes, true)
}

func (model *FakeModel) AddExternalDevice(name string) (ExternalDeviceModel, error) {
	dev := &FakeExtDevice{*newFakeDevice(model, name, name)}
	model.devices[name] = dev
	return dev, nil
}

func (model *FakeModel) HasDevice(name string) bool {
	_, found := model.devices[name]
	return found
}

func (model *FakeModel) GetDevice(name string) FakeDev {
	if dev, found := model.devices[name]; !found {
		model.t.Fatalf("unknown device %s", name)
		return nil
	} else {
		return dev
	}
}

// TBD: rename ReceiveValue

func (dev *FakeDevice) AcceptValue(name, value string) {
	if _, found := dev.paramTypes[name]; !found {
		dev.paramTypes[name] = "text"
	}
	dev.paramValues[name] = value
	dev.model.Rec("AcceptValue: %s.%s = %s", dev.DevName, name, value)
	return
}

func (dev *FakeDevice) ReceiveValue(name, value string) {
	if _, found := dev.paramValues[name]; !found {
		dev.model.t.Fatalf("trying to send unknown param %s (value %s)",
			name, value)
	} else {
		dev.paramValues[name] = value
		dev.Observer.OnValue(dev, name, value)
	}
}

func (dev *FakeDevice) GetValue(name string) string {
	return dev.paramValues[name]
}

func (dev *FakeDevice) GetType(name string) string {
	return dev.paramTypes[name]
}

func (dev *FakeLocalDevice) AcceptOnValue(name, value string) bool {
	if _, found := dev.paramTypes[name]; !found {
		dev.paramTypes[name] = "text"
	}
	dev.paramValues[name] = value
	dev.model.Rec("AcceptOnValue: %s.%s = %s", dev.DevName, name, value)
	return true
}

func (dev *FakeLocalDevice) QueryParams() {
	// FIXME! sort
	keys := make([]string, 0, len(dev.paramTypes))
	for k := range dev.paramTypes {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		paramType := dev.paramTypes[k]
		dev.Observer.OnNewControl(dev, k, paramType, dev.paramValues[k], false, -1,
			paramType != "pushbutton")
	}
}

func (dev *FakeLocalDevice) IsVirtual() bool {
	return dev.virtual
}

func (dev *FakeExtDevice) AcceptControlType(name, controlType string) {
	dev.paramTypes[name] = controlType
	dev.model.Rec("the type of %s.%s is: %s", dev.DevName, name, controlType)
}

func (dev *FakeExtDevice) AcceptControlRange(name string, max float64) {
	dev.model.Rec("max value for %s.%s is: %v", dev.DevName, name, max)
}
