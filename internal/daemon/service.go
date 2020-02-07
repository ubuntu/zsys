package daemon

import (
	"fmt"

	"github.com/k0kubun/pp"
	"github.com/ubuntu/zsys"
	"github.com/ubuntu/zsys/internal/authorizer"
	"github.com/ubuntu/zsys/internal/config"
	"github.com/ubuntu/zsys/internal/i18n"
	"github.com/ubuntu/zsys/internal/log"
)

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
