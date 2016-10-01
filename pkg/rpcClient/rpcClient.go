package rpcClient

import (
	"io"
	"net"

	"github.com/CyCoreSystems/go-kamailio/binrpc"
	"github.com/pkg/errors"
)

type binRPCClientCodec struct {
	c io.ReadWriteCloser
}

func (c *binRPCClientCodec) ReadResponseBody(body interface{}) error {
	return nil
}

func (c *binRPCClientCodec) WriteRequest(name string) error {
	var methodName = binrpc.BinRpcString(name)
	return methodName.Encode(c.c)
}

func newClientCodec(conn io.ReadWriteCloser) *binRPCClientCodec {
	return &binRPCClientCodec{
		c: conn,
	}
}

// InvokeMethod calls the given RPC method on the given host and port
func InvokeMethod(method string, host string, port string) error {

	conn, err := net.Dial("udp", host+":"+port)
	defer conn.Close()

	if err != nil {
		return errors.Wrap(err, "failed to connect to kamailio RPC server")
	}

	codec := newClientCodec(conn)
	err = codec.WriteRequest(method)

	if err != nil {
		return errors.Wrap(err, "failed to invoke RPC method")
	}

	return nil
}
