// Parts of this file are derived from net/rpc and net/rpc/jsonproc Go packages.
// Copyright/license info follow.
// Copyright 2009 The Go Authors. All rights reserved.
// License:
// Copyright (c) 2012 The Go Authors. All rights reserved.

// Redistribution and use in source and binary forms, with or without
// modification, are permitted provided that the following conditions are
// met:

//    * Redistributions of source code must retain the above copyright
// notice, this list of conditions and the following disclaimer.
//    * Redistributions in binary form must reproduce the above
// copyright notice, this list of conditions and the following disclaimer
// in the documentation and/or other materials provided with the
// distribution.
//    * Neither the name of Google Inc. nor the names of its
// contributors may be used to endorse or promote products derived from
// this software without specific prior written permission.

// THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS
// "AS IS" AND ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT
// LIMITED TO, THE IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR
// A PARTICULAR PURPOSE ARE DISCLAIMED. IN NO EVENT SHALL THE COPYRIGHT
// OWNER OR CONTRIBUTORS BE LIABLE FOR ANY DIRECT, INDIRECT, INCIDENTAL,
// SPECIAL, EXEMPLARY, OR CONSEQUENTIAL DAMAGES (INCLUDING, BUT NOT
// LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS OR SERVICES; LOSS OF USE,
// DATA, OR PROFITS; OR BUSINESS INTERRUPTION) HOWEVER CAUSED AND ON ANY
// THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT LIABILITY, OR TORT
// (INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF THE USE
// OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.

package wbgo

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math"
	"reflect"
	"sort"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"
)

const (
	RPC_MESSAGE_QUEUE_LEN = 32
	RPC_APP_PREFIX        = "/rpc/v1/"
	RPC_RESERVED_CODE_MIN = -32700
	RPC_RESERVED_CODE_MAX = -32000
)

func isReservedErrorCode(code int32) bool {
	return code >= RPC_RESERVED_CODE_MIN && code <= RPC_RESERVED_CODE_MAX
}

type RPCError interface {
	error
	ErrorCode() int32
}

type internalRPCError interface {
	error
	InternalErrorCode() int32
}

type internalErr struct {
	code    int32
	message string
}

func (err *internalErr) Error() string {
	return err.message
}

func (err *internalErr) InternalErrorCode() int32 {
	return err.code
}

var parseError = &internalErr{-32700, "Parse error"}
var invalidRequestError = &internalErr{-32600, "Invalid request"}
var methodNotFoundError = &internalErr{-32601, "Method not found"}
var invalidParamsError = &internalErr{-32602, "Invalid params"}

// Internal error (-32603) code is not used, this implementation
// chooses to panic instead
// var internalError = &internalErr{-32603, "Internal error"}

// Note that in structs below we maintain alphabetical order of JSON
// object membres to make testing easier. encoding/json serializes
// struct members in the order they're defined and in our test code we
// need object keys to be in alphabetical order to make JSON
// comparisons easier.
type rpcRequest struct {
	Id     *json.RawMessage `json:"id"`
	Params *json.RawMessage `json:"params"`
}

type rpcSuccessResponse struct {
	Id     string      `json:"id"`
	Result interface{} `json:"result"`
}

type rpcError struct {
	Code    interface{} `json:"code"`
	Message interface{} `json:"message"`
}

type rpcExtendedError struct {
	Code      interface{} `json:"code"`
	ErrorType string      `json:"data"`
	Message   interface{} `json:"message"`
}

type rpcErrorResponse struct {
	Error interface{} `json:"error"`
	Id    interface{} `json:"id"` // may be nil (null)
}

func validateId(id interface{}) error {
	// From JSON RPC2 spec:
	// An identifier established by the Client that MUST contain a
	// String, Number, or NULL value if included. If it is not included
	// it is assumed to be a notification. The value SHOULD normally not
	// be Null [1] and Numbers SHOULD NOT contain fractional parts [2]
	//
	// From Go JSON decoder description:
	// To unmarshal JSON into an interface value,
	// Unmarshal stores one of these in the interface value:
	//
	//	bool, for JSON booleans
	//	float64, for JSON numbers
	//	string, for JSON strings
	//	[]interface{}, for JSON arrays
	//	map[string]interface{}, for JSON objects
	//	nil for JSON null
	switch id.(type) {
	case nil:
	case string:
	case float64:
		if id.(float64) != math.Trunc(id.(float64)) {
			return invalidRequestError
		}
	default:
		return invalidRequestError
	}
	return nil
}

// Precompute the reflect type for error.  Can't use error directly
// because Typeof takes an empty interface value.  This is annoying.
var typeOfError = reflect.TypeOf((*error)(nil)).Elem()

type methodType struct {
	sync.Mutex // protects counters
	method     reflect.Method
	ArgType    reflect.Type
	ReplyType  reflect.Type
	numCalls   uint
}

func (m *methodType) NumCalls() (n uint) {
	m.Lock()
	n = m.numCalls
	m.Unlock()
	return n
}

type service struct {
	name   string                 // name of service
	rcvr   reflect.Value          // receiver of methods for the service
	typ    reflect.Type           // type of the receiver
	method map[string]*methodType // registered methods
}

func (s *service) call(mtype *methodType, argv, replyv reflect.Value) (reply interface{}, err error) {
	mtype.Lock()
	mtype.numCalls++
	mtype.Unlock()
	function := mtype.method.Func
	// Invoke the method, providing a new value for the reply.
	returnValues := function.Call([]reflect.Value{s.rcvr, argv, replyv})
	// The return value for the method is an error.
	errInter := returnValues[0].Interface()
	if errInter != nil {
		err = errInter.(error)
	}
	reply = replyv.Interface()
	return
}

// suitableMethods returns suitable Rpc methods of typ, it will report
// error using log if reportErr is true.
func suitableMethods(typ reflect.Type, reportErr bool) map[string]*methodType {
	methods := make(map[string]*methodType)
	for m := 0; m < typ.NumMethod(); m++ {
		method := typ.Method(m)
		mtype := method.Type
		mname := method.Name
		// Method must be exported.
		if method.PkgPath != "" {
			continue
		}
		// Method needs three ins: receiver, *args, *reply.
		if mtype.NumIn() != 3 {
			if reportErr {
				log.Println("method", mname, "has wrong number of ins:", mtype.NumIn())
			}
			continue
		}
		// First arg need not be a pointer.
		argType := mtype.In(1)
		if !isExportedOrBuiltinType(argType) {
			if reportErr {
				log.Println(mname, "argument type not exported:", argType)
			}
			continue
		}
		// Second arg must be a pointer.
		replyType := mtype.In(2)
		if replyType.Kind() != reflect.Ptr {
			if reportErr {
				log.Println("method", mname, "reply type not a pointer:", replyType)
			}
			continue
		}
		// Reply type must be exported.
		if !isExportedOrBuiltinType(replyType) {
			if reportErr {
				log.Println("method", mname, "reply type not exported:", replyType)
			}
			continue
		}
		// Method needs one out.
		if mtype.NumOut() != 1 {
			if reportErr {
				log.Println("method", mname, "has wrong number of outs:", mtype.NumOut())
			}
			continue
		}
		// The return type of the method must be error.
		if returnType := mtype.Out(0); returnType != typeOfError {
			if reportErr {
				log.Println("method", mname, "returns", returnType.String(), "not error")
			}
			continue
		}
		methods[mname] = &methodType{method: method, ArgType: argType, ReplyType: replyType}
	}
	return methods
}

type MQTTRPCServer struct {
	sync.RWMutex
	prefix     string
	mqttClient MQTTClient // TBD: rename to mqttClient
	messageCh  chan MQTTMessage
	active     bool
	quit       chan struct{}
	serviceMap map[string]*service
}

func NewMQTTRPCServer(appName string, mqttClient MQTTClient) (server *MQTTRPCServer) {
	server = &MQTTRPCServer{
		prefix:     RPC_APP_PREFIX + appName,
		mqttClient: mqttClient,
		messageCh:  make(chan MQTTMessage, RPC_MESSAGE_QUEUE_LEN),
		active:     false,
		serviceMap: make(map[string]*service),
	}
	return
}

func (server *MQTTRPCServer) Start() {
	server.Lock()
	defer server.Unlock()
	if server.active {
		return
	}
	server.active = true
	server.quit = make(chan struct{})
	server.mqttClient.Start()
	server.mqttClient.Subscribe(func(message MQTTMessage) {
		server.messageCh <- message
	}, server.subscriptionTopic())
	serviceNames := make([]string, 0, len(server.serviceMap))
	for serviceName, _ := range server.serviceMap {
		serviceNames = append(serviceNames, serviceName)
	}
	sort.Strings(serviceNames)
	for _, serviceName := range serviceNames {
		service := server.serviceMap[serviceName]
		methodNames := make([]string, 0, len(service.method))
		for methodName, _ := range service.method {
			methodNames = append(methodNames, methodName)
		}
		sort.Strings(methodNames)
		for _, methodName := range methodNames {
			server.mqttClient.Publish(
				MQTTMessage{
					fmt.Sprintf("%s/%s/%s",
						server.prefix,
						service.name,
						methodName),
					"1",
					1,
					true,
				})
		}
	}
	go server.processMessages()
}

func (server *MQTTRPCServer) Stop() {
	server.Lock()
	defer server.Unlock()
	if !server.active {
		return
	}
	close(server.quit)
}

func (server *MQTTRPCServer) subscriptionTopic() string {
	return server.prefix + "/+/+/+"
}

func (server *MQTTRPCServer) processMessages() {
	for {
		select {
		case <-server.quit:
			server.mqttClient.Unsubscribe(server.subscriptionTopic())
			return
		case msg := <-server.messageCh:
			server.handleMessage(msg)
		}
	}
}

func (server *MQTTRPCServer) locateMethod(topic string) (service *service, mtype *methodType, err error) {
	parts := strings.Split(topic, "/")
	if len(parts) < 3 {
		// this may happen only due to a bug
		log.Panicf("unexpected topic: %s", topic)
	}
	serviceName := parts[len(parts)-3]
	methodName := parts[len(parts)-2]

	// Look up the request.
	server.RLock()
	service = server.serviceMap[serviceName]
	server.RUnlock()
	if service == nil {
		Debug.Printf("rpc: service not found: %s", serviceName)
		err = methodNotFoundError
		return
	}
	mtype = service.method[methodName]
	if mtype == nil {
		Debug.Printf("rpc: method not found: %s (service %s)", methodName, serviceName)
		err = methodNotFoundError
	}
	return
}

func (server *MQTTRPCServer) parseRequest(payload string, target interface{}) (id interface{}, err error) {
	var req rpcRequest
	if err = json.Unmarshal([]byte(payload), &req); err != nil {
		Debug.Printf("rpc: bad json in request")
		err = parseError
		return
	}

	if req.Id == nil {
		// Notifications are not supported.
		// Not sure whether we should support them as it's
		// possible to just use plain MQTT messages.
		Debug.Printf("rpc: no request id (notifications not supported)")
		err = invalidRequestError
		return
	}

	if err = json.Unmarshal(*req.Id, &id); err != nil {
		Debug.Printf("failed ro parse id")
		err = parseError
		return
	}

	if err = validateId(id); err != nil {
		id = nil
		return
	}

	// in case the method wasn't found we can't parse params
	if target == nil {
		return
	}

	// Not specifying params at all is possible in general case
	// but in this implementation we always need params
	if req.Params == nil {
		Debug.Printf("rpc: no params")
		err = invalidParamsError
		return
	}

	// see whether params are array to distinguish between
	// invalidRequestError and invalidParamsError
	var p []interface{}
	if err = json.Unmarshal(*req.Params, &p); err != nil {
		// check whether we have valid JSON; it's not quite
		// clear whetherjson.RawMessage resulting from JSON
		// unmarshalling may contain invalid JSON, but's let's
		// be safe here
		if _, ok := err.(*json.SyntaxError); ok {
			// must return null as id in case the
			// request contains invalid JSON
			id = nil
			Debug.Printf("rpc: error parsing params")
			err = parseError
			return
		}

		Debug.Printf("rpc: params is not an array")
		err = invalidRequestError
		return
	}

	// make sure params contain just one argument
	if len(p) != 1 {
		Debug.Printf("rpc: more than single param")
		err = invalidParamsError
		return
	}

	var params [1]interface{}
	params[0] = target
	if err = json.Unmarshal(*req.Params, &params); err != nil {
		// wrong params
		Debug.Printf("rpc: wrong params")
		err = invalidParamsError
		return
	}
	return
}

func (server *MQTTRPCServer) parseMessage(message MQTTMessage) (service *service, mtype *methodType, replyId interface{}, argv, replyv reflect.Value, err error) {
	service, mtype, err = server.locateMethod(message.Topic)
	if err != nil {
		// method not found, but still need to try to extract
		// transaction id from the request
		id, err1 := server.parseRequest(message.Payload, nil)
		if err1 != nil {
			err = err1
			return
		}
		replyId = id
		return
	}

	// Decode the argument value.
	argIsValue := false // if true, need to indirect before calling.
	if mtype.ArgType.Kind() == reflect.Ptr {
		argv = reflect.New(mtype.ArgType.Elem())
	} else {
		argv = reflect.New(mtype.ArgType)
		argIsValue = true
	}

	// argv guaranteed to be a pointer now.
	replyId, err = server.parseRequest(message.Payload, argv.Interface())
	if argIsValue {
		argv = argv.Elem()
	}

	replyv = reflect.New(mtype.ReplyType.Elem())
	return
}

func (server *MQTTRPCServer) buildResponse(replyId interface{}, result interface{}, err error) (response interface{}) {
	if err == nil {
		response = &rpcSuccessResponse{
			Id:     replyId.(string),
			Result: result,
		}
		return
	}

	if ierr, ok := err.(internalRPCError); ok {
		// internal error, must have reserved code
		code := ierr.InternalErrorCode()
		if !isReservedErrorCode(code) {
			log.Panicf("invalid internal error code %d (error was: %s)", code, err)
		}
		return &rpcErrorResponse{
			Id: replyId,
			Error: rpcError{
				Code:    code,
				Message: ierr.Error(),
			},
		}
	}

	if rpcErr, ok := err.(RPCError); ok {
		// Extended RPC error, contins code & error type ('data' member).
		// Use error type name as error type.
		code := rpcErr.ErrorCode()
		if isReservedErrorCode(code) {
			log.Panicf("reserved error code detected: %d (error was: %s)", code, err)
		}
		return &rpcErrorResponse{
			Id: replyId,
			Error: &rpcExtendedError{
				Code:      code,
				ErrorType: reflect.Indirect(reflect.ValueOf(err)).Type().Name(),
				Message:   err.Error(),
			},
		}
	}

	// simple error, no code & error type ('data' member)
	return &rpcErrorResponse{
		Id: replyId,
		Error: &rpcError{
			Code:    -1,
			Message: err.Error(),
		},
	}
}

func (server *MQTTRPCServer) doHandleMessage(message MQTTMessage) (response interface{}) {
	var result interface{}
	service, mtype, replyId, argv, replyv, err := server.parseMessage(message)
	if err == nil {
		result, err = service.call(mtype, argv, replyv)
	}
	// Here replyId must be a string. It can only be non-string (nil)
	// in case of invalid request, which is handled above
	return server.buildResponse(replyId, result, err)
}

func (server *MQTTRPCServer) handleMessage(message MQTTMessage) {
	response := server.doHandleMessage(message)
	responseTopic := message.Topic + "/reply"
	responseBytes, err := json.Marshal(response)
	if err != nil {
		Error.Printf("error marshalling RPC response for topic %s: %s",
			message.Topic, err)
		return
	}
	server.mqttClient.Publish(MQTTMessage{responseTopic, string(responseBytes), 1, false})
}

func isExported(name string) bool {
	rune, _ := utf8.DecodeRuneInString(name)
	return unicode.IsUpper(rune)
}

// Is this type exported or a builtin?
func isExportedOrBuiltinType(t reflect.Type) bool {
	for t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	// PkgPath will be non-empty even for an exported type,
	// so we need to check the type name as well.
	return isExported(t.Name()) || t.PkgPath() == ""
}

// Register publishes in the server the set of methods of the
// receiver value that satisfy the following conditions:
//	- exported method
//	- two arguments, both of exported type
//	- the second argument is a pointer
//	- one return value, of type error
// It returns an error if the receiver is not an exported type or has
// no suitable methods. It also logs the error using package log.
// The client accesses each method using a string of the form "Type.Method",
// where Type is the receiver's concrete type.
func (server *MQTTRPCServer) Register(rcvr interface{}) error {
	return server.register(rcvr, "", false)
}

// RegisterName is like Register but uses the provided name for the type
// instead of the receiver's concrete type.
func (server *MQTTRPCServer) RegisterName(name string, rcvr interface{}) error {
	return server.register(rcvr, name, true)
}

func (server *MQTTRPCServer) register(rcvr interface{}, name string, useName bool) error {
	server.Lock()
	defer server.Unlock()
	if server.serviceMap == nil {
		server.serviceMap = make(map[string]*service)
	}
	s := new(service)
	s.typ = reflect.TypeOf(rcvr)
	s.rcvr = reflect.ValueOf(rcvr)
	sname := reflect.Indirect(s.rcvr).Type().Name()
	if useName {
		sname = name
	}
	if sname == "" {
		s := "rpc.Register: no service name for type " + s.typ.String()
		log.Print(s)
		return errors.New(s)
	}
	if !isExported(sname) && !useName {
		s := "rpc.Register: type " + sname + " is not exported"
		log.Print(s)
		return errors.New(s)
	}
	if _, present := server.serviceMap[sname]; present {
		return errors.New("rpc: service already defined: " + sname)
	}
	s.name = sname

	// Install the methods
	s.method = suitableMethods(s.typ, true)

	if len(s.method) == 0 {
		str := ""

		// To help the user, see if a pointer receiver would work.
		method := suitableMethods(reflect.PtrTo(s.typ), false)
		if len(method) != 0 {
			str = "rpc.Register: type " + sname +
				" has no exported methods of suitable type " +
				"(hint: pass a pointer to value of that type)"
		} else {
			str = "rpc.Register: type " + sname +
				" has no exported methods of suitable type"
		}
		log.Print(str)
		return errors.New(str)
	}
	server.serviceMap[s.name] = s
	return nil
}
