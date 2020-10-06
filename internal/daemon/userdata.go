package daemon

import (
	"fmt"

	"github.com/ubuntu/zsys"
	"github.com/ubuntu/zsys/internal/authorizer"
	"github.com/ubuntu/zsys/internal/config"
	"github.com/ubuntu/zsys/internal/i18n"
	"github.com/ubuntu/zsys/internal/log"
)

// CreateUserData creates a new userdata for user and set it to homepath on current zsys system.
// if the user already exists for a dataset attached to the current system, set its mountpoint to homepath.
// This is called by zsys grpc request, once the server is registered
func (s *Server) CreateUserData(req *zsys.CreateUserDataRequest, stream zsys.Zsys_CreateUserDataServer) (err error) {
	if err := s.authorizer.IsAllowedFromContext(stream.Context(), authorizer.ActionSystemWrite); err != nil {
		return err
	}

	user := req.GetUser()
	encryptHome := req.GetEncryptHome()
	homepath := req.GetHomepath()
	s.RWRequest.Lock()
	defer s.RWRequest.Unlock()

	log.Infof(stream.Context(), i18n.G("Create user dataset for %q on %q"), user, homepath)

	if err := s.Machines.CreateUserData(stream.Context(), user, homepath, encryptHome); err != nil {
		return fmt.Errorf(i18n.G("couldn't create userdataset for %q: ")+config.ErrorFormat, homepath, err)
	}
	return nil
}

// ChangeHomeOnUserData tries to find an existing dataset matching home as a valid mountpoint and rename it to newhome
func (s *Server) ChangeHomeOnUserData(req *zsys.ChangeHomeOnUserDataRequest, stream zsys.Zsys_ChangeHomeOnUserDataServer) (err error) {
	if err := s.authorizer.IsAllowedFromContext(stream.Context(), authorizer.ActionSystemWrite); err != nil {
		return err
	}

	home := req.GetHome()
	newHome := req.GetNewHome()
	s.RWRequest.Lock()
	defer s.RWRequest.Unlock()

	log.Infof(stream.Context(), i18n.G("Rename home user dataset from %q to %q"), home, newHome)

	if err := s.Machines.ChangeHomeOnUserData(stream.Context(), home, newHome); err != nil {
		return fmt.Errorf(i18n.G("couldn't change home userdataset for %q: ")+config.ErrorFormat, home, err)
	}
	return nil
}

// DissociateUser removes user associated dataset association with current system.
// All history is kept though and the dataset are just unlinked, not removed.
func (s *Server) DissociateUser(req *zsys.DissociateUserRequest, stream zsys.Zsys_DissociateUserServer) (err error) {
	if err := s.authorizer.IsAllowedFromContext(stream.Context(), authorizer.ActionSystemWrite); err != nil {
		return err
	}

	user := req.GetUser()
	removeHome := req.GetRemoveHome()
	s.RWRequest.Lock()
	defer s.RWRequest.Unlock()

	log.Infof(stream.Context(), i18n.G("Dissociate user %q"), user)

	if err := s.Machines.DissociateUser(stream.Context(), user, removeHome); err != nil {
		return fmt.Errorf(i18n.G("couldn't dissociate user %q: ")+config.ErrorFormat, user, err)
	}
	return nil
}
