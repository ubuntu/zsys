package daemon

import (
	"errors"
	"fmt"
	"runtime"
	"runtime/pprof"
	"time"

	"github.com/k0kubun/pp"
	"github.com/ubuntu/zsys"
	"github.com/ubuntu/zsys/internal/authorizer"
	"github.com/ubuntu/zsys/internal/config"
	"github.com/ubuntu/zsys/internal/i18n"
	"github.com/ubuntu/zsys/internal/log"
)

// DaemonStop stops zsys daemon
func (s *Server) DaemonStop(req *zsys.Empty, stream zsys.Zsys_DaemonStopServer) error {
	if err := s.authorizer.IsAllowedFromContext(stream.Context(), authorizer.ActionManageService); err != nil {
		return err
	}
	log.Info(stream.Context(), i18n.G("Requesting zsys daemon stop"))

	go func() {
		// Give some time for all clients (especially the current one) to close their unix socket connection:
		// systemd think, even if the GRPC connections are all closed, that a client is
		// using the socket, and so will retrigger the daemon.
		time.Sleep(2 * time.Second)

		s.Stop()
	}()
	return nil
}

// DumpStates dumps the entire internal state of zsys daemon
func (s *Server) DumpStates(req *zsys.Empty, stream zsys.Zsys_DumpStatesServer) error {
	if err := s.authorizer.IsAllowedFromContext(stream.Context(), authorizer.ActionSystemList); err != nil {
		return err
	}

	s.RWRequest.RLock()
	defer s.RWRequest.RUnlock()

	log.Info(stream.Context(), i18n.G("Requesting service states dump"))

	if err := stream.Send(&zsys.DumpStatesResponse{
		Reply: &zsys.DumpStatesResponse_States{
			States: pp.Sprint(s.Machines),
		},
	}); err != nil {
		return fmt.Errorf(i18n.G("couldn't dump machine state")+config.ErrorFormat, err)
	}

	return nil
}

// LoggingLevel set the verbosity of the logger
func (s *Server) LoggingLevel(req *zsys.LoggingLevelRequest, stream zsys.Zsys_LoggingLevelServer) error {
	if err := s.authorizer.IsAllowedFromContext(stream.Context(), authorizer.ActionManageService); err != nil {
		return err
	}

	logginglevel := req.GetLogginglevel()
	log.Infof(stream.Context(), i18n.G("Setting logging level to %d"), logginglevel)

	config.SetVerboseMode(int(logginglevel))
	return nil
}

// Refresh reloads the state of zfs from the system
func (s *Server) Refresh(req *zsys.Empty, stream zsys.Zsys_RefreshServer) error {
	if err := s.authorizer.IsAllowedFromContext(stream.Context(), authorizer.ActionManageService); err != nil {
		return err
	}
	log.Info(stream.Context(), i18n.G("Requesting a refresh"))

	return s.Machines.Refresh(stream.Context())
}

type traceForwarder struct {
	zsys.Zsys_TraceServer
}

func (t traceForwarder) Write(p []byte) (int, error) {
	err := t.Send(&zsys.TraceResponse{
		Reply: &zsys.TraceResponse_Trace{
			Trace: p,
		},
	})

	return len(p), err
}

const defaultMemProfileRate = 4096

// Trace performs CPU of MEM profiling and returns the trace to the client
func (s *Server) Trace(req *zsys.TraceRequest, stream zsys.Zsys_TraceServer) error {
	if err := s.authorizer.IsAllowedFromContext(stream.Context(), authorizer.ActionManageService); err != nil {
		return err
	}

	traceType := req.GetType()
	traceDuration := req.GetDuration()
	log.Infof(stream.Context(), i18n.G("Requesting %s profiling"), traceType)

	w := traceForwarder{Zsys_TraceServer: stream}

	switch traceType {
	case "cpu":
		pprof.StartCPUProfile(w)
		defer pprof.StopCPUProfile()
	case "mem":
		old := runtime.MemProfileRate
		runtime.MemProfileRate = defaultMemProfileRate
		defer func() {
			pprof.Lookup("heap").WriteTo(w, 0)
			runtime.MemProfileRate = old
		}()
	default:
		return errors.New(i18n.G("unknown type of profiling"))
	}

	time.Sleep(time.Duration(traceDuration) * time.Second)

	return nil
}

// Status returns the status of the daemion
func (s *Server) Status(req *zsys.Empty, stream zsys.Zsys_StatusServer) error {
	rErr := make(chan error)
	go func() {
		if err := s.authorizer.IsAllowedFromContext(stream.Context(), authorizer.ActionAlwaysAllowed); err != nil {
			rErr <- err
			return
		}
		log.Info(stream.Context(), i18n.G("Requesting zsys daemon status"))
		s.RWRequest.RLock()
		defer s.RWRequest.RUnlock()

		// TODO: replace with machines.List
		_, err := s.Machines.EnsureBoot(stream.Context())
		rErr <- err
	}()

	select {
	case err := <-rErr:
		return err
	case <-time.After(3 * time.Second):
		return errors.New(i18n.G("No response within few seconds"))
	}
}
