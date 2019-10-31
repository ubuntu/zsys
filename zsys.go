package zsys

import (
	"context"
	"fmt"
	"net"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/ubuntu/zsys/internal/streamlogger"
	"google.golang.org/grpc"
)

//go:generate protoc --go_out=plugins=grpc:. zsys.proto
// Takes output of protoc for second streamlogger generation.
//go:generate go run internal/streamlogger/generator.go -- zsys.pb.go

// NewZsysUnixSocketClient returns a new grpc zsys compatible client connection,
// via the unix socket path, with an initialized context and requester ID.
// It will send log request at level "level"
func NewZsysUnixSocketClient(socket string, level logrus.Level) (*ZsysLogClient, error) {
	conn, err := grpc.Dial(socket,
		grpc.WithInsecure(), grpc.WithDialer(unixConnect(socket)),
		grpc.WithStreamInterceptor(streamlogger.ClientRequestLogInterceptor))
	if err != nil {
		return nil, fmt.Errorf("couldn't connect to unix socket %q: %w", socket, err)
	}

	return newZsysClientWithLogs(context.Background(), conn, level), nil
}

// RegisterServer registers a ZsysServer after creating the grpc server which it returns.
func RegisterServer(srv ZsysServer) *grpc.Server {
	s := grpc.NewServer()
	registerZsysServerWithLogs(s, srv)
	return s
}

// unixConnect returns a given local connection on socket path.
func unixConnect(socket string) func(addr string, t time.Duration) (net.Conn, error) {
	return func(addr string, t time.Duration) (net.Conn, error) {
		unixAddr, err := net.ResolveUnixAddr("unix", socket)
		conn, err := net.DialUnix("unix", nil, unixAddr)
		return conn, err
	}
}