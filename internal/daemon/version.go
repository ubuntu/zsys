package daemon

import (
	"github.com/ubuntu/zsys"
	"github.com/ubuntu/zsys/internal/authorizer"
	"github.com/ubuntu/zsys/internal/config"
	"github.com/ubuntu/zsys/internal/i18n"
	"github.com/ubuntu/zsys/internal/log"
)

// Version returns the version of the daemon
func (s *Server) Version(req *zsys.Empty, stream zsys.Zsys_VersionServer) (err error) {
	if err := s.authorizer.IsAllowedFromContext(stream.Context(), authorizer.ActionAlwaysAllowed); err != nil {
		return err
	}

	log.Info(stream.Context(), i18n.G("Retrieving version of daemon"))

	stream.Send(&zsys.VersionResponse{
		Reply: &zsys.VersionResponse_Version{
			Version: config.Version,
		},
	})

	return nil
}
