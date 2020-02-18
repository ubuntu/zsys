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
	"github.com/ubuntu/zsys/internal/zfs/libzfs"
	"google.golang.org/grpc"
)

// Server is used to implement zsys.ZsysServer.
type Server struct {
	// Machines scanned
	Machines machines.Machines

	// Requests mutex
	RWRequest sync.RWMutex

	socket     string
	lis        net.Listener
	grpcserver *grpc.Server

	// Those elements could be mocked in tests
	authorizer        *authorizer.Authorizer
	systemdSdNotifier func(unsetEnvironment bool, state string) (bool, error)
	idlerTimeout      idler
}

/*
 * Daemon options
 * These options and helpers are augmenting the daemon struct to make the code testable with
 * mock authorizer, systemd, timeout adjustments.
 */

// WithIdleTimeout changes server default idle timeout
func WithIdleTimeout(timeout time.Duration) func(o *options) error {
	return func(o *options) error {
		o.timeout = timeout
		return nil
	}
}

// WithLibZFS allows overriding default libzfs implementations with a mock
func WithLibZFS(libzfs libzfs.Interface) func(o *options) error {
	return func(o *options) error {
		o.libzfs = libzfs
		return nil
	}
}

type options struct {
	timeout                   time.Duration
	libzfs                    libzfs.Interface
	authorizer                *authorizer.Authorizer
	systemdActivationListener func() ([]net.Listener, error)
	systemdSdNotifier         func(unsetEnvironment bool, state string) (bool, error)
}

type option func(*options) error

// New returns an new, initialized daemon server, which handles systemd activation
// socket is ignored if we are using socket activation.
func New(socket string, opts ...option) (*Server, error) {
	args := options{
		timeout:                   config.DefaultServerIdleTimeout,
		systemdActivationListener: activation.Listeners,
		systemdSdNotifier:         daemon.SdNotify,
		libzfs:                    &libzfs.Adapter{},
	}
	for _, o := range opts {
		if err := o(&args); err != nil {
			return nil, fmt.Errorf(i18n.G("Couldn't apply option to server: %v"), err)
		}
	}

	// systemd socket activation or local creation
	listeners, err := args.systemdActivationListener()
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

	cmdline, err := procCmdline()
	if err != nil {
		return nil, fmt.Errorf(i18n.G("couldn't parse kernel command line: %v"), err)
	}
	ms, err := machines.New(context.Background(), cmdline, machines.WithLibZFS(args.libzfs))
	if err != nil {
		return nil, fmt.Errorf(i18n.G("couldn't create a new machine: %v"), err)
	}

	if args.authorizer == nil {
		args.authorizer, err = authorizer.New()
		if err != nil {
			return nil, fmt.Errorf(i18n.G("couldn't create new authorizer: %v"), err)
		}
	}

	s := &Server{
		Machines: ms,

		socket: socket,
		lis:    lis,

		authorizer:        args.authorizer,
		systemdSdNotifier: args.systemdSdNotifier,

		idlerTimeout: newIdler(args.timeout),
	}
	grpcserver := zsys.RegisterServer(s)
	s.grpcserver = grpcserver

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
	if sent, err := s.systemdSdNotifier(false, "READY=1"); err != nil {
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

// procCmdline returns kernel command line
func procCmdline() (string, error) {
	content, err := ioutil.ReadFile("/proc/cmdline")
	if err != nil {
		return "", err
	}

	return string(content), nil
}
