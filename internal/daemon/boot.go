package daemon

import (
	"fmt"

	"github.com/ubuntu/zsys"
	"github.com/ubuntu/zsys/internal/authorizer"
	"github.com/ubuntu/zsys/internal/config"
	"github.com/ubuntu/zsys/internal/i18n"
	"github.com/ubuntu/zsys/internal/log"
)

// PrepareBoot consolidates canmount states for early boot.
// Return if any dataset / machine changed has been done during boot and an error if any encountered.
func (s *Server) PrepareBoot(req *zsys.Empty, stream zsys.Zsys_PrepareBootServer) (err error) {
	if err := s.authorizer.IsAllowedFromContext(stream.Context(), authorizer.ActionSystemWrite); err != nil {
		return err
	}

	s.RWRequest.Lock()
	defer s.RWRequest.Unlock()

	log.Infof(stream.Context(), i18n.G("Prepare current boot state"))

	changed, err := s.Machines.EnsureBoot(stream.Context())
	if err != nil {
		return fmt.Errorf(i18n.G("couldn't ensure boot: ")+config.ErrorFormat, err)
	}
	stream.Send(&zsys.PrepareBootResponse{
		Reply: &zsys.PrepareBootResponse_Changed{Changed: changed},
	})

	return nil
}

// CommitBoot commits current state to be the active one by promoting its datasets if needed, set last used,
// associate user datasets to it and rebuilding grub menu.
// After this operation, every New() call will get the current and correct system state.
// Return if any dataset / machine changed has been done during boot commit and an error if any encountered.
func (s *Server) CommitBoot(req *zsys.Empty, stream zsys.Zsys_CommitBootServer) (err error) {
	if err := s.authorizer.IsAllowedFromContext(stream.Context(), authorizer.ActionSystemWrite); err != nil {
		return err
	}

	s.RWRequest.Lock()
	defer s.RWRequest.Unlock()

	log.Infof(stream.Context(), i18n.G("Commit current boot state"))

	changed, err := s.Machines.Commit(stream.Context())
	if err != nil {
		return fmt.Errorf(i18n.G("couldn't commit: ")+config.ErrorFormat, err)
	}
	stream.Send(&zsys.CommitBootResponse{
		Reply: &zsys.CommitBootResponse_Changed{Changed: changed},
	})

	if !changed {
		return nil
	}

	return updateBootMenu(stream.Context())
}

// UpdateBootMenu updates machine bootmenu.
func (s *Server) UpdateBootMenu(req *zsys.UpdateBootMenuRequest, stream zsys.Zsys_UpdateBootMenuServer) (err error) {
	if err := s.authorizer.IsAllowedFromContext(stream.Context(), authorizer.ActionSystemWrite); err != nil {
		return err
	}

	s.RWRequest.Lock()
	defer s.RWRequest.Unlock()

	// update triggered by apt or other system on non zsys system. Do nothing
	if !s.Machines.CurrentIsZsys() && req.GetAuto() {
		return nil
	}

	log.Infof(stream.Context(), i18n.G("Updating system boot menu"))

	return updateBootMenu(stream.Context())
}
