// Parts of this file are derived from net/rpc Go package.
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
	"fmt"
	"log"
	"net/rpc"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"
)

const (
	RPC_MESSAGE_QUEUE_LEN = 32
	RPC_APP_PREFIX        = "/rpc/v1/"
)

type rpcRequest struct {
	Id     string           `json:"id"`
	Params *json.RawMessage `json:"params"`
}

type rpcResponse struct {
	Error  interface{} `json:"error"`
	Id     string      `json:"id"`
	Result interface{} `json:"result"`
}

type mqttRpcCodec struct {
	methodName string
	request    *rpcRequest
	response   *rpcResponse
}

func (c *mqttRpcCodec) ReadRequestHeader(r *rpc.Request) error {
	id, err := strconv.ParseUint(c.request.Id, 10, 64)
	if err != nil {
		return fmt.Errorf("invalid request id received: %v", c.request.Id)
	}
	r.ServiceMethod = c.methodName
	r.Seq = id
	return nil
}

func (c *mqttRpcCodec) ReadRequestBody(x interface{}) (err error) {
	if x == nil {
		c.request = nil
		return nil
	}

	if c.request.Params == nil {
		return fmt.Errorf("no params provided for RPC request")
	}

	// based on idea from Go's json RPC server
	var params [1]interface{}
	params[0] = x
	err = json.Unmarshal(*c.request.Params, &params)
	c.request = nil
	return
}

func (c *mqttRpcCodec) WriteResponse(r *rpc.Response, x interface{}) error {
	c.response = &rpcResponse{Id: strconv.FormatUint(r.Seq, 10)}
	if r.Error == "" {
		c.response.Result = x
	} else {
		c.response.Error = r.Error
	}
	return nil
}

func (c *mqttRpcCodec) Close() error {
	return nil
}

// isExported / isExportedOrBuiltinType / suitableMethodNames are based on net/rpc code

// Precompute the reflect type for error.  Can't use error directly
// because Typeof takes an empty interface value.  This is annoying.
var typeOfError = reflect.TypeOf((*error)(nil)).Elem()

// Is this an exported - upper case - name?
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

func suitableMethodNames(typ reflect.Type) []string {
	methodNames := make([]string, 0, 32)
	for m := 0; m < typ.NumMethod(); m++ {
		method := typ.Method(m)
		mtype := method.Type
		mname := method.Name
		if method.PkgPath != "" {
			continue
		}
		if mtype.NumIn() != 3 {
			continue
		}
		argType := mtype.In(1)
		if !isExportedOrBuiltinType(argType) {
			continue
		}
		replyType := mtype.In(2)
		if replyType.Kind() != reflect.Ptr {
			continue
		}
		if !isExportedOrBuiltinType(replyType) {
			continue
		}
		if mtype.NumOut() != 1 {
			continue
		}
		if returnType := mtype.Out(0); returnType != typeOfError {
			continue
		}
		methodNames = append(methodNames, mname)
	}
	sort.Strings(methodNames)
	return methodNames
}

type serviceDesc struct {
	name        string
	methodNames []string
}

type MQTTRPCServer struct {
	sync.Mutex
	prefix    string
	server    *rpc.Server
	client    MQTTClient
	messageCh chan MQTTMessage
	active    bool
	quit      chan struct{}
	services  []*serviceDesc
}

func NewMQTTRPCServer(appName string, client MQTTClient) (mqttRpc *MQTTRPCServer) {
	mqttRpc = &MQTTRPCServer{
		prefix:    RPC_APP_PREFIX + appName,
		server:    rpc.NewServer(),
		client:    client,
		messageCh: make(chan MQTTMessage, RPC_MESSAGE_QUEUE_LEN),
		active:    false,
		services:  make([]*serviceDesc, 0, 32),
	}
	return
}

func (mqttRpc *MQTTRPCServer) Start() {
	mqttRpc.Lock()
	defer mqttRpc.Unlock()
	if mqttRpc.active {
		return
	}
	mqttRpc.active = true
	mqttRpc.quit = make(chan struct{})
	mqttRpc.client.Start()
	mqttRpc.client.Subscribe(func(message MQTTMessage) {
		mqttRpc.messageCh <- message
	}, mqttRpc.subscriptionTopic())
	for _, service := range mqttRpc.services {
		for _, methodName := range service.methodNames {
			mqttRpc.client.Publish(
				MQTTMessage{
					fmt.Sprintf("%s/%s/%s",
						mqttRpc.prefix,
						service.name,
						methodName),
					"1",
					1,
					true,
				})
		}
	}
	go mqttRpc.processMessages()
}

func (mqttRpc *MQTTRPCServer) Stop() {
	mqttRpc.Lock()
	defer mqttRpc.Unlock()
	if !mqttRpc.active {
		return
	}
	close(mqttRpc.quit)
}

func (mqttRpc *MQTTRPCServer) subscriptionTopic() string {
	return mqttRpc.prefix + "/+/+/+"
}

func (mqttRpc *MQTTRPCServer) processMessages() {
	for {
		select {
		case <-mqttRpc.quit:
			mqttRpc.client.Unsubscribe(mqttRpc.subscriptionTopic())
			return
		case msg := <-mqttRpc.messageCh:
			mqttRpc.handleMessage(msg)
		}
	}
}

func (mqttRpc *MQTTRPCServer) handleMessage(message MQTTMessage) {
	parts := strings.Split(message.Topic, "/")
	if len(parts) < 3 {
		// this may happen only due to a bug
		log.Panicf("unexpected topic: %s", message.Topic)
	}
	var req rpcRequest
	if err := json.Unmarshal([]byte(message.Payload), &req); err != nil {
		Error.Printf("invalid MQTT rpc message received for topic %s", message.Topic)
		return
	}
	serviceName := parts[len(parts)-3]
	methodName := parts[len(parts)-2]
	// parts[len(parts)-2] is client id
	codec := &mqttRpcCodec{
		request:    &req,
		methodName: serviceName + "." + methodName,
	}
	if err := mqttRpc.server.ServeRequest(codec); err != nil {
		Error.Printf("error serving RPC request for topic %s: %s", message.Topic, err)
		return
	}
	if codec.response != nil {
		responseTopic := message.Topic + "/reply"
		responseBytes, err := json.Marshal(codec.response)
		if err != nil {
			Error.Printf("error marshalling RPC response for topic %s: %s",
				message.Topic, err)
			return
		}
		mqttRpc.client.Publish(MQTTMessage{responseTopic, string(responseBytes), 1, false})
	}
}

func (mqttRpc *MQTTRPCServer) Register(service interface{}) error {
	serviceName := reflect.Indirect(reflect.ValueOf(service)).Type().Name()
	methodNames := suitableMethodNames(reflect.TypeOf(service))
	mqttRpc.services = append(mqttRpc.services, &serviceDesc{serviceName, methodNames})
	return mqttRpc.server.Register(service)
}
