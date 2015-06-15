package wbgo

import (
	"errors"
	"fmt"
	"github.com/stretchr/objx"
	"strconv"
	"testing"
)

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

type RPCSuite struct {
	Suite
	*FakeMQTTFixture
	client *FakeMQTTClient
	rpc    *MQTTRPCServer
}

func (s *RPCSuite) SetupTest() {
	s.Suite.SetupTest()
	s.FakeMQTTFixture = NewFakeMQTTFixture(s.T())
	s.rpc = NewMQTTRPCServer("/wbrpc/SampleRpc", s.Broker.MakeClient("samplerpc"))
	if err := s.rpc.Register(new(Arith)); err != nil {
		s.Require().Fail("registration error", "%s", err)
	}
	s.client = s.Broker.MakeClient("tst")
	s.client.Start()
	s.rpc.Start()
	s.Verify("Subscribe -- samplerpc: /wbrpc/SampleRpc/+/+/+")
}

func (s *RPCSuite) TearDownTest() {
	s.rpc.Stop()
	s.Verify("Unsubscribe -- samplerpc: /wbrpc/SampleRpc/+/+/+")
	s.Suite.TearDownTest()
}

func (s *RPCSuite) publish(topic string, payload objx.Map) string {
	payloadStr := payload.MustJSON()
	s.client.Publish(MQTTMessage{topic, payloadStr, 1, false})
	return payloadStr
}

func (s *RPCSuite) TestRPC() {
	for i := 0; i < 10; i++ {
		jsonStr := s.publish("/wbrpc/SampleRpc/b692040b/Arith/Multiply", objx.Map{
			"id": strconv.Itoa(i),
			"params": []objx.Map{
				objx.Map{"A": i, "B": i + 1},
			},
		})
		s.Verify(
			fmt.Sprintf(
				"tst -> /wbrpc/SampleRpc/b692040b/Arith/Multiply: [%s] (QoS 1)", jsonStr),
			fmt.Sprintf(
				"samplerpc -> /wbrpc/SampleRpc/b692040b/Arith/Multiply/reply: [%s] (QoS 1)",
				objx.Map{
					"id":     strconv.Itoa(i),
					"result": i * (i + 1),
					"error":  nil,
				}.MustJSON()),
		)
	}
}

func (s *RPCSuite) TestErrors() {
	jsonStr := s.publish("/wbrpc/SampleRpc/b692040b/Arith/Divide", objx.Map{
		"id": "0",
		"params": []objx.Map{
			objx.Map{"A": 10, "B": 0},
		},
	})
	s.Verify(
		fmt.Sprintf(
			"tst -> /wbrpc/SampleRpc/b692040b/Arith/Divide: [%s] (QoS 1)", jsonStr),
		fmt.Sprintf(
			"samplerpc -> /wbrpc/SampleRpc/b692040b/Arith/Divide/reply: [%s] (QoS 1)",
			objx.Map{
				"id":     "0",
				"result": nil,
				"error":  "divide by zero",
			}.MustJSON()),
	)
}

func (s *RPCSuite) TestMalformedJSONRequest() {
	reqs := []struct {
		id     string
		topic  string
		params interface{}
	}{
		// no params
		{id: "0", topic: "/wbrpc/SampleRpc/b692040b/Arith/Multiply"},
		// params must be an array
		{id: "1", params: objx.Map{}, topic: "/wbrpc/SampleRpc/b692040b/Arith/Multiply"},
		// no id
		{params: []objx.Map{}, topic: "/wbrpc/SampleRpc/b692040b/Arith/Multiply"},
		// wrong types
		{
			id: "2",
			params: []objx.Map{
				objx.Map{"A": "xx", "B": 2},
			},
			topic: "/wbrpc/SampleRpc/b692040b/Arith/Multiply",
		},
	}

	for _, req := range reqs {
		jsonRequest := objx.Map{}
		if req.id != "" {
			jsonRequest["id"] = req.id
		}
		if req.params != nil {
			jsonRequest["params"] = req.params
		}
		s.publish(req.topic, jsonRequest)
		s.Verify(
			fmt.Sprintf(
				"tst -> /wbrpc/SampleRpc/b692040b/Arith/Multiply: [%s] (QoS 1)",
				jsonRequest.MustJSON()))
		s.VerifyEmpty()
		s.WaitForErrors()
	}
}

func (s *RPCSuite) TestMalformedJSON() {
	s.client.Publish(
		MQTTMessage{
			"/wbrpc/SampleRpc/b692040b/Arith/Multiply",
			"blabla",
			1,
			false,
		})
	s.Verify("tst -> /wbrpc/SampleRpc/b692040b/Arith/Multiply: [blabla] (QoS 1)")
	s.VerifyEmpty()
	s.WaitForErrors()
}

func TestRPCSuite(t *testing.T) {
	RunSuites(t, new(RPCSuite))
}

// Note for the js side: must obtain seq id by combining two numbers
// because JS Number cannot represent the whole range of uint64
