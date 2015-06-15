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
}

func (s *RPCSuite) SetupTest() {
	s.Suite.SetupTest()
	s.FakeMQTTFixture = NewFakeMQTTFixture(s.T())
	rpc := NewMQTTRPCServer("/wbrpc/SampleRpc", s.Broker.MakeClient("samplerpc"))
	if err := rpc.Register(new(Arith)); err != nil {
		s.Require().Fail("registration error", "%s", err)
	}
	s.client = s.Broker.MakeClient("tst")
	s.client.Start()
	rpc.Start()
	s.Verify("Subscribe -- samplerpc: /wbrpc/SampleRpc/+/+/+")
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

func TestRPCSuite(t *testing.T) {
	RunSuites(t, new(RPCSuite))
}

// TBD: test errors returned by the service
// TBD: test no params and other invalid requests
// TBD: test stopping service (may just check it in TearDownTest)
