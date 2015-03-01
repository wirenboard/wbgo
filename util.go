package wbgo

import (
	"sync"
	"reflect"
)

const DEFERRED_CAPACITY = 256

func doVisit(visitor interface{}, thing interface{}, methodName string, args []interface{}) bool {
	if method, found := reflect.TypeOf(visitor).MethodByName(methodName); !found {
		return false
	} else {
		moreValues := make([]reflect.Value, len(args))
		for i, arg := range(args) {
			moreValues[i] = reflect.ValueOf(arg)
		}
		method.Func.Call(append([]reflect.Value{
			reflect.ValueOf(visitor),
			reflect.ValueOf(thing),
		}, moreValues...))
		return true
	}
}

func Visit(visitor interface{}, thing interface{}, prefix string, args... interface{}) {
	typeName := reflect.Indirect(reflect.ValueOf(thing)).Type().Name()
	methodName := prefix + typeName
	if !doVisit(visitor, thing, methodName, args) &&
		!doVisit(visitor, thing, prefix + "Anything", args) {
		Debug.Printf("visit: no visitor method for %s", typeName)
		return
	}
}

type DeferredList struct {
	sync.Mutex
	fns []func()
	executor func(func())
}

func NewDeferredList(executor func(func())) *DeferredList {
	return &DeferredList{fns: make([]func(), 0, DEFERRED_CAPACITY)}
}

func (dl *DeferredList) MaybeDefer(thunk func()) {
	dl.Lock()
	if dl.fns != nil {
		dl.fns = append(dl.fns, thunk)
		dl.Unlock()
		return
	}
	dl.Unlock()
	if dl.executor != nil {
		dl.executor(thunk)
	} else {
		thunk()
	}
}

func (dl *DeferredList) Ready() {
	dl.Lock()
	defer dl.Unlock()
	for _, fn := range dl.fns {
		fn()
	}
	dl.fns = nil
}
