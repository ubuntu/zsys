package daemon

import (
	"context"
	"fmt"
	"io/ioutil"
	"net"
	"os"
	"sync"
	"time"

	"github.com/coreos/go-systemd/activation"
	"github.com/coreos/go-systemd/daemon"
	"github.com/ubuntu/zsys"
	"github.com/ubuntu/zsys/internal/authorizer"
	"github.com/ubuntu/zsys/internal/config"
	"github.com/ubuntu/zsys/internal/i18n"
	"github.com/ubuntu/zsys/internal/log"
	"github.com/ubuntu/zsys/internal/machines"
	"github.com/ubuntu/zsys/internal/zfs"
	"google.golang.org/grpc"
)

// Server is used to implement zsys.ZsysServer.
type Server struct {
	// Machines scanned
	Machines *machines.Machines

	// Requests mutex
	RWRequest sync.RWMutex

	socket     string
	lis        net.Listener
	grpcserver *grpc.Server

	authorizer *authorizer.Authorizer

	idlerTimeout idler
}

// IdleTimeout changes server default idle timeout
func IdleTimeout(timeout time.Duration) func(s *Server) error {
	return func(s *Server) error {
		s.idlerTimeout = newIdler(timeout)
		return nil
	}
}

// New returns an new, initialized daemon server, which handles systemd activation
// socket is ignored if we are using socket activation.
func New(socket string, options ...func(s *Server) error) (*Server, error) {
	// systemd socket activation or local creation
	listeners, err := activation.Listeners()
	if err != nil {
		return nil, fmt.Errorf(i18n.G("cannot retrieve systemd listeners: %v"), err)
	}

	var lis net.Listener
	switch len(listeners) {
	case 0:
		l, err := net.Listen("unix", socket)
		if err != nil {
			return nil, fmt.Errorf(i18n.G("failed to listen on %q: %w"), socket, err)
		}
		os.Chmod(socket, 0666)
		lis = l
	case 1:
		socket = ""
		lis = listeners[0]
	default:
		return nil, fmt.Errorf(i18n.G("unexpected number of systemd socket activation (%d != 1)"), len(listeners))
	}

	z := zfs.New(context.Background())
	defer z.Done()
	ms, err := getMachines(z)
	if err != nil {
		return nil, fmt.Errorf(i18n.G("couldn't scan machines: %v"), err)
	}

	a, err := authorizer.New()
	if err != nil {
		return nil, fmt.Errorf(i18n.G("couldn't create new authorizer: %v"), err)
	}

	s := &Server{
		Machines: ms,

		socket: socket,
		lis:    lis,

		authorizer: a,

		idlerTimeout: newIdler(config.DefaultServerIdleTimeout),
	}
	grpcserver := zsys.RegisterServer(s)
	s.grpcserver = grpcserver

	for _, option := range options {
		if err := option(s); err != nil {
			log.Warningf(context.Background(), i18n.G("Couldn't apply option to server: %v"), err)
		}
	}

	// Handle idle timeout
	go s.idlerTimeout.start(s)

	return s, nil
}

// Listen serves on its unix socket path.
// It handles systemd activation notification.
// When the server stop listening, the socket is removed automatically.
func (s *Server) Listen() error {
	log.Infof(context.Background(), i18n.G("Serving on %s"), s.lis.Addr().String())

	// systemd activation
	if sent, err := daemon.SdNotify(false, "READY=1"); err != nil {
		return fmt.Errorf(i18n.G("couldn't send ready notification to systemd while supported: %v"), err)
	} else if sent {
		log.Debug(context.Background(), i18n.G("Ready state sent to systemd"))
	}

	return s.grpcserver.Serve(s.lis)
}

// Stop gracefully stops the grpc server
func (s *Server) Stop() {
	log.Debug(context.Background(), i18n.G("Stopping daemon requested. Wait for active requests to close"))
	s.grpcserver.GracefulStop()
	log.Debug(context.Background(), i18n.G("All connections closed"))
}

// TrackRequest prevents the idling timeout to fire up and return the function to reset it.
func (s *Server) TrackRequest() func() {
	s.idlerTimeout.addRequest()
	return func() {
		log.Debugf(context.Background(), i18n.G("Reset idle timeout to %s"), s.idlerTimeout.timeout)
		s.idlerTimeout.endRequest()
	}
}

// getMachines returns all scanned machines on the current system
func getMachines(z *zfs.Zfs) (*machines.Machines, error) {
	ds, err := z.Scan()
	if err != nil {
		return nil, err
	}
	cmdline, err := procCmdline()
	if err != nil {
		return nil, err
	}
	ms := machines.New(z.Context(), ds, cmdline)

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
