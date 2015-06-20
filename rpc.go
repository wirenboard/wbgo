package wbgo

import (
	"encoding/json"
	"fmt"
	"log"
	"net/rpc"
	"strconv"
	"strings"
	"sync"
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

type MQTTRPCServer struct {
	sync.Mutex
	prefix    string
	server    *rpc.Server
	client    MQTTClient
	messageCh chan MQTTMessage
	active    bool
	quit      chan struct{}
}

func NewMQTTRPCServer(appName string, client MQTTClient) (mqttRpc *MQTTRPCServer) {
	mqttRpc = &MQTTRPCServer{
		prefix:    RPC_APP_PREFIX + appName,
		server:    rpc.NewServer(),
		client:    client,
		messageCh: make(chan MQTTMessage, RPC_MESSAGE_QUEUE_LEN),
		active:    false,
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
	return mqttRpc.server.Register(service)
}
