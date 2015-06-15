package wbgo

import (
	"encoding/json"
	"fmt"
	"log"
	"net/rpc"
	"strconv"
	"strings"
)

const (
	RPC_MESSAGE_QUEUE_LEN = 32
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
	prefix    string
	server    *rpc.Server
	client    MQTTClient
	messageCh chan MQTTMessage
}

func NewMQTTRPCServer(prefix string, client MQTTClient) (mqttRpc *MQTTRPCServer) {
	mqttRpc = &MQTTRPCServer{
		prefix:    prefix,
		server:    rpc.NewServer(),
		client:    client,
		messageCh: make(chan MQTTMessage, RPC_MESSAGE_QUEUE_LEN),
	}
	return
}

func (mqttRpc *MQTTRPCServer) Start() {
	mqttRpc.client.Start()
	mqttRpc.client.Subscribe(func(message MQTTMessage) {
		mqttRpc.messageCh <- message
	}, mqttRpc.prefix+"/+/+/+")
	go mqttRpc.processMessages()
}

func (mqttRpc *MQTTRPCServer) processMessages() {
	for {
		mqttRpc.handleMessage(<-mqttRpc.messageCh)
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
	serviceName := parts[len(parts)-2]
	methodName := parts[len(parts)-1]
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
