package zsys

import (
	"context"
	"fmt"
	"net"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/ubuntu/zsys/internal/authorizer"
	"github.com/ubuntu/zsys/internal/i18n"
	"github.com/ubuntu/zsys/internal/streamlogger"
	"google.golang.org/grpc"
)

//go:generate sh -c "if go run internal/generators/can_modify_repo.go 2>/dev/null; then PATH=\"$PATH:`go env GOPATH`/bin\" protoc --go_out=. --go_opt=paths=source_relative --go-grpc_out=. --go-grpc_opt=paths=source_relative,require_unimplemented_servers=false zsys.proto; fi"
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
		return nil, fmt.Errorf(i18n.G("couldn't connect to unix socket %q: %w"), socket, err)
	}

	return newZsysClientWithLogs(context.Background(), conn, level), nil
}

// RegisterServer registers a ZsysServer after creating the grpc server which it returns.
func RegisterServer(srv ZsysServerIdleTimeout) *grpc.Server {
	s := grpc.NewServer(grpc.StreamInterceptor(streamlogger.ServerIdleTimeoutInterceptor), authorizer.WithUnixPeerCreds())
	registerZsysServerIdleWithLogs(s, srv)
	return s
}

// unixConnect returns a given local connection on socket path.
func unixConnect(socket string) func(addr string, t time.Duration) (net.Conn, error) {
	return func(addr string, t time.Duration) (net.Conn, error) {
		unixAddr, err := net.ResolveUnixAddr("unix", socket)
		if err != nil {
			return nil, err
		}
		conn, err := net.DialUnix("unix", nil, unixAddr)
		return conn, err
	}
}
