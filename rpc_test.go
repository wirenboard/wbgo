package wbgo

import (
	"errors"
	"fmt"
	"github.com/stretchr/objx"
	"strconv"
	"strings"
	"testing"
)

type ErrorWithCode struct {
	msg  string
	code int32
}

func (err *ErrorWithCode) Error() string {
	return err.msg
}

func (err *ErrorWithCode) ErrorCode() int32 {
	return err.code
}

// from Go rpc examples
type Args struct {
	A, B int
}

type Quotient struct {
	Quo, Rem int
}

type Arith int

func (*Arith) Multiply(args *Args, reply *int) error {
	*reply = args.A * args.B
	return nil
}

func (*Arith) Divide(args *Args, quo *Quotient) error {
	if args.B == 0 {
		return errors.New("divide by zero")
	}
	quo.Quo = args.A / args.B
	quo.Rem = args.A % args.B
	return nil
}

func (*Arith) MakeAnError(args *struct{}, r *int) error {
	return &ErrorWithCode{"app error with code", -42}
}

type RPCSuite struct {
	Suite
	*FakeMQTTFixture
	client *FakeMQTTClient
	rpc    *MQTTRPCServer
}

// Usage: verifyMessages(fmt1, v1_1, v1_2, fmt2, v2, ...)  The number
// of v_* arguments depends on the number of unescaped '%' signs in
// the corresponding format string.
func (s *RPCSuite) verifyMessages(items ...interface{}) {
	if len(items)%2 != 0 {
		s.Require().Fail("verifyMessages(): odd number of items")
		return
	}

	strs := make([]string, 0, len(items))
	for n := 0; n < len(items); {
		formatStr := items[n].(string)
		n++
		// Note that here we count arguments by number of
		// unescaped '%' characters. This ignores [n] and *
		// modifiers, but will do for our purposes here.
		numArgs := strings.Count(strings.Replace(formatStr, "%%", "", -1), "%")
		args := make([]interface{}, numArgs)
		for i := 0; i < numArgs; i++ {
			item := items[n]
			n++
			if m, ok := item.(objx.Map); ok {
				item = m.MustJSON()
			}
			args[i] = item
		}
		strs = append(strs, fmt.Sprintf(formatStr, args...))
	}
	s.Verify(strs...)
}

func (s *RPCSuite) SetupTest() {
	s.Suite.SetupTest()
	s.FakeMQTTFixture = NewFakeMQTTFixture(s.T())
	s.rpc = NewMQTTRPCServer("SampleRpc", s.Broker.MakeClient("samplerpc"))
	if err := s.rpc.Register(new(Arith)); err != nil {
		s.Require().Fail("registration error", "%s", err)
	}
	s.client = s.Broker.MakeClient("tst")
	s.client.Start()
	s.rpc.Start()
	s.Verify(
		"Subscribe -- samplerpc: /rpc/v1/SampleRpc/+/+/+",
		"samplerpc -> /rpc/v1/SampleRpc/Arith/Divide: [1] (QoS 1, retained)",
		"samplerpc -> /rpc/v1/SampleRpc/Arith/MakeAnError: [1] (QoS 1, retained)",
		"samplerpc -> /rpc/v1/SampleRpc/Arith/Multiply: [1] (QoS 1, retained)",
	)
}

func (s *RPCSuite) TearDownTest() {
	s.rpc.Stop()
	s.Verify("Unsubscribe -- samplerpc: /rpc/v1/SampleRpc/+/+/+")
	s.Suite.TearDownTest()
}

func (s *RPCSuite) publish(topic string, payload interface{}) string {
	payloadStr, ok := payload.(string)
	if !ok {
		payloadStr = payload.(objx.Map).MustJSON()
	}
	s.client.Publish(MQTTMessage{topic, payloadStr, 1, false})
	return payloadStr
}

func (s *RPCSuite) verifyCall(i int, id interface{}) {
	jsonStr := s.publish("/rpc/v1/SampleRpc/Arith/Multiply/b692040b", objx.Map{
		"id": id,
		"params": []objx.Map{
			objx.Map{"A": i, "B": i + 1},
		},
	})
	s.verifyMessages(
		"tst -> /rpc/v1/SampleRpc/Arith/Multiply/b692040b: [%s] (QoS 1)",
		jsonStr,
		"samplerpc -> /rpc/v1/SampleRpc/Arith/Multiply/b692040b/reply: [%s] (QoS 1)",
		objx.Map{
			"id":     id,
			"result": i * (i + 1),
		},
	)
}

func (s *RPCSuite) TestRPC() {
	for i := 0; i < 10; i++ {
		s.verifyCall(i, i)
		s.verifyCall(i, strconv.Itoa(i))
	}
}

func (s *RPCSuite) TestErrors() {
	jsonStr := s.publish("/rpc/v1/SampleRpc/Arith/Divide/b692040b", objx.Map{
		"id": "0",
		"params": []objx.Map{
			objx.Map{"A": 10, "B": 0},
		},
	})
	s.verifyMessages(
		"tst -> /rpc/v1/SampleRpc/Arith/Divide/b692040b: [%s] (QoS 1)",
		jsonStr,
		"samplerpc -> /rpc/v1/SampleRpc/Arith/Divide/b692040b/reply: [%s] (QoS 1)",
		objx.Map{
			"error": objx.Map{
				"code":    -1,
				"message": "divide by zero",
			},
			"id": "0",
		},
	)
}

var rpcErrorTestCases = []struct {
	name     string
	id       string
	topic    string
	raw      string
	params   interface{}
	response interface{}
}{
	{
		name:  "no params",
		id:    "0",
		topic: "/rpc/v1/SampleRpc/Arith/Multiply/b692040b",
		response: objx.Map{
			"id": "0",
			"error": objx.Map{
				"code":    -32602,
				"message": "Invalid params",
			},
		},
	},
	{
		name:   "params must be an array",
		id:     "1",
		params: objx.Map{},
		topic:  "/rpc/v1/SampleRpc/Arith/Multiply/b692040b",
		response: objx.Map{
			"id": "1",
			"error": objx.Map{
				"code":    -32600,
				"message": "Invalid request",
			},
		},
	},
	{
		name:   "no id",
		params: []objx.Map{},
		topic:  "/rpc/v1/SampleRpc/Arith/Multiply/b692040b",
		response: objx.Map{
			"id": nil,
			"error": objx.Map{
				"code":    -32600,
				"message": "Invalid request",
			},
		},
	},
	{
		name: "wrong types",
		id:   "2",
		params: []objx.Map{
			objx.Map{"A": "xx", "B": 2},
		},
		topic: "/rpc/v1/SampleRpc/Arith/Multiply/b692040b",
		response: objx.Map{
			"id": "2",
			"error": objx.Map{
				"code":    -32602,
				"message": "Invalid params",
			},
		},
	},
	{
		name: "wrong number of params",
		id:   "3",
		params: []objx.Map{
			objx.Map{"A": 1, "B": 2},
			objx.Map{},
		},
		topic: "/rpc/v1/SampleRpc/Arith/Multiply/b692040b",
		response: objx.Map{
			"id": "3",
			"error": objx.Map{
				"code":    -32602,
				"message": "Invalid params",
			},
		},
	},
	{
		name:   "service not found",
		id:     "4",
		params: []objx.Map{},
		topic:  "/rpc/v1/SampleRpc/NoSuchService/Multiply/b692040b",
		response: objx.Map{
			"id": "4",
			"error": objx.Map{
				"code":    -32601,
				"message": "Method not found",
			},
		},
	},
	{
		name:   "service not found",
		id:     "5",
		params: []objx.Map{},
		topic:  "/rpc/v1/SampleRpc/Arith/NoSuchMethod/b692040b",
		response: objx.Map{
			"id": "5",
			"error": objx.Map{
				"code":    -32601,
				"message": "Method not found",
			},
		},
	},
	{
		name:  "parse error",
		id:    "6",
		raw:   "blabla",
		topic: "/rpc/v1/SampleRpc/Arith/NoSuchMethod/b692040b",
		response: objx.Map{
			"id": nil,
			"error": objx.Map{
				"code":    -32700,
				"message": "Parse error",
			},
		},
	},
	{
		name: "application error without code",
		id:   "7",
		params: []objx.Map{
			objx.Map{"A": 10, "B": 0},
		},
		topic: "/rpc/v1/SampleRpc/Arith/Divide/b692040b",
		response: objx.Map{
			"id": "7",
			"error": objx.Map{
				"code":    -1,
				"message": "divide by zero",
			},
		},
	},
	{
		name:   "application error with code",
		id:     "8",
		params: []objx.Map{objx.Map{}},
		topic:  "/rpc/v1/SampleRpc/Arith/MakeAnError/b692040b",
		response: objx.Map{
			"id": "8",
			"error": objx.Map{
				"code":    -42,
				"message": "app error with code",
				"data":    "ErrorWithCode",
			},
		},
	},
}

func (s *RPCSuite) TestRequestErrors() {
	for _, tcase := range rpcErrorTestCases {
		Debug.Printf("--- TEST CASE: %s ---", tcase.name)
		var toSend interface{}
		if tcase.raw != "" {
			toSend = tcase.raw
		} else {
			jsonRequest := objx.Map{}
			if tcase.id != "" {
				jsonRequest["id"] = tcase.id
			}
			if tcase.params != nil {
				jsonRequest["params"] = tcase.params
			}
			toSend = jsonRequest
		}
		s.publish(tcase.topic, toSend)
		s.verifyMessages(
			"tst -> %s: [%s] (QoS 1)",
			tcase.topic, toSend,
			"samplerpc -> %s/reply: [%s] (QoS 1)",
			tcase.topic, tcase.response,
		)
		s.VerifyEmpty()
	}
}

func TestRPCSuite(t *testing.T) {
	RunSuites(t, new(RPCSuite))
}
