package zsys

import (
	"context"
	"fmt"
	"net"
	"os"
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
		grpc.WithStreamInterceptor(streamlogger.ClientRequestIDInterceptor))
	if err != nil {
		return nil, fmt.Errorf("couldn't connect to unix socket %q: %w", socket, err)
	}

	return NewZsysClientWithLogs(context.Background(), conn, level), nil
}

// RegisterAndListenZsysUnixSocketServer serves on an unix socket path, and register a ZsysServer.
// The listener can be cancelled to remove the socket file properly.
func RegisterAndListenZsysUnixSocketServer(ctx context.Context, socket string, srv ZsysServer) error {
	lis, err := net.Listen("unix", socket)
	if err != nil {
		return fmt.Errorf("failed to listen on %q: %w", socket, err)
	}
	defer os.Remove(socket)

	s := grpc.NewServer()
	RegisterZsysServerWithLogs(s, srv)

	go func() {
		<-ctx.Done()
		s.Stop()
	}()
	return s.Serve(lis)
}

// unixConnect returns a given local connection on socket path.
func unixConnect(socket string) func(addr string, t time.Duration) (net.Conn, error) {
	return func(addr string, t time.Duration) (net.Conn, error) {
		unixAddr, err := net.ResolveUnixAddr("unix", socket)
		conn, err := net.DialUnix("unix", nil, unixAddr)
		return conn, err
	}
}
