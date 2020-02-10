package daemon

import (
	"fmt"
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
