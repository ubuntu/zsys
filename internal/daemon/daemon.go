package daemon

import (
	"context"
	"fmt"
	"io/ioutil"
	"net"
	"time"

	"github.com/coreos/go-systemd/activation"
	"github.com/coreos/go-systemd/daemon"
	"github.com/ubuntu/zsys"
	"github.com/ubuntu/zsys/internal/log"
	"github.com/ubuntu/zsys/internal/machines"
	"github.com/ubuntu/zsys/internal/zfs"
	"google.golang.org/grpc"
)

// Server is used to implement zsys.ZsysServer.
type Server struct {
	socket     string
	lis        net.Listener
	grpcserver *grpc.Server

	idleTimeout time.Duration
	reset       chan struct{}
}

// New returns an new, initialized daemon server, which handles systemd activation
func New(socket string) (*Server, error) {
	// systemd socket activation or local creation
	listeners, err := activation.Listeners()
	if err != nil {
		return nil, fmt.Errorf("cannot retrieve systemd listeners: %v", err)
	}
	var lis net.Listener
	switch len(listeners) {
	case 0:
		l, err := net.Listen("unix", socket)
		if err != nil {
			return nil, fmt.Errorf("failed to listen on %q: %w", socket, err)
		}
		defer os.Remove(socket)
		lis = l
	case 1:
		socket = ""
		lis = listeners[0]
	default:
		return nil, fmt.Errorf("unexpected number of systemd socket activation (%d != 1)", len(listeners))
	}

	s := &Server{
		socket:      socket,
		lis:         lis,
		idleTimeout: 10 * time.Second,
		reset:       make(chan struct{}),
	}
	grpcserver := zsys.RegisterServer(s)
	s.grpcserver = grpcserver

	// Handle idle timeout
	go func() {
		timeout := time.NewTimer(s.idleTimeout)

	out:
		for {
			select {
			case <-timeout.C:
				Log.Debug(context.Background(), "Idle timeout expired")
				break out
			case <-s.reset:
				if !timeout.Stop() {
					<-timeout.C
				}
				timeout.Reset(s.idleTimeout)
			}
		}
		s.Stop()
	}()

	return s, nil
}

// Listen serves on its unix socket path.
// It handles systemd activation notification.
// When the server stop listening, it will remove the socket file properly.
func (s *Server) Listen() error {
	log.Infof(context.Background(), "Daemon serving on %s", s.lis.Addr().String())

	// systemd activation
	if sent, err := daemon.SdNotify(false, "READY=1"); err != nil {
		return fmt.Errorf("couldn't send ready notification to systemd while supported: %v", err)
	} else if sent {
		log.Debug(context.Background(), "Ready state sent to systemd")
	}

	return s.grpcserver.Serve(s.lis)
}

// Stop gracefully stops the grpc server
func (s *Server) Stop() {
	log.Debug(context.Background(), "Stopping daemon requested. Wait for active requests to close")
	s.grpcserver.GracefulStop()
	log.Debug(context.Background(), "All connexions closed")
}

// resetTimeout resets the idling timeout on the server
func (s *Server) resetTimeout() {
	log.Debugf(context.Background(), "Reset idle timeout to %s", s.idleTimeout)
	s.reset <- struct{}{}
}

// getMachines returns all scanned machines on the current system
func getMachines(ctx context.Context, z *zfs.Zfs) (*machines.Machines, error) {
	ds, err := z.Scan()
	if err != nil {
		return nil, err
	}
	cmdline, err := procCmdline()
	if err != nil {
		return nil, err
	}
	ms := machines.New(ctx, ds, cmdline)

	return &ms, nil
}

// procCmdline returns kernel command line
func procCmdline() (string, error) {
	content, err := ioutil.ReadFile("/proc/cmdline")
	if err != nil {
		return "", err
	}

	return string(content), nil
}
