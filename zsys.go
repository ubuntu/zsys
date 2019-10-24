package zsys

import (
	"context"
	"fmt"
	"net"
	"os"
	"time"

	"github.com/coreos/go-systemd/activation"
	"github.com/coreos/go-systemd/daemon"
	"github.com/sirupsen/logrus"
	"github.com/ubuntu/zsys/internal/log"
	"github.com/ubuntu/zsys/internal/streamlogger"
	"google.golang.org/grpc"
)

//go:generate protoc --go_out=plugins=grpc:. zsys.proto
// Takes output of protoc for second streamlogger generation.
//go:generate go run internal/streamlogger/generator.go -- zsys.pb.go

const (
	// DefaultSocket path.
	DefaultSocket = "/run/zsysd.sock"

	// DefaultTimeout for client requests
	DefaultTimeout = 30 * time.Second
)

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

// RegisterAndListenZsysUnixSocketServer serves on an unix socket path, and register a ZsysServer.
// It handles systemd activation and notifications.
// The listener can be cancelled to remove the socket file properly.
func RegisterAndListenZsysUnixSocketServer(ctx context.Context, socket string, srv ZsysServer) error {
	// systemd socket activation or local creation
	listeners, err := activation.Listeners()
	if err != nil {
		return fmt.Errorf("cannot retrieve systemd listeners: %v", err)
	}
	var lis net.Listener
	switch len(listeners) {
	case 0:
		l, err := net.Listen("unix", socket)
		if err != nil {
			return fmt.Errorf("failed to listen on %q: %w", socket, err)
		}
		defer os.Remove(socket)
		lis = l
	case 1:
		lis = listeners[0]
	default:
		return fmt.Errorf("unexpected number of systemd socket activation (%d != 1)", len(listeners))
	}

	s := grpc.NewServer()
	registerZsysServerWithLogs(s, srv)

	// systemd activation
	if sent, err := daemon.SdNotify(false, "READY=1"); err != nil {
		return fmt.Errorf("couldn't send ready notification to systemd while supported: %v", err)
	} else if sent {
		log.Debug(ctx, "ready state sent to systemd")
	}

	go func() {
		<-ctx.Done()
		log.Debug(ctx, "stopping daemon requested. Wait for active requests to close")
		s.GracefulStop()
		log.Debug(ctx, "all connexions closed")
	}()
	log.Debug(ctx, "daemon serving")
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
