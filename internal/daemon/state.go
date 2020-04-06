package daemon

import (
	"context"
	"errors"
	"fmt"

	"github.com/ubuntu/zsys"
	"github.com/ubuntu/zsys/internal/authorizer"
	"github.com/ubuntu/zsys/internal/config"
	"github.com/ubuntu/zsys/internal/i18n"
	"github.com/ubuntu/zsys/internal/log"
	"github.com/ubuntu/zsys/internal/machines"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// SaveSystemState creates a snapshot of a system and all users datasets.
// If stateName is not empty, it is used as the id of the snapshot otherwise an id
// is generated with a random string.
func (s *Server) SaveSystemState(req *zsys.SaveSystemStateRequest, stream zsys.Zsys_SaveSystemStateServer) (err error) {
	if err := s.authorizer.IsAllowedFromContext(stream.Context(), authorizer.ActionSystemWrite); err != nil {
		return err
	}

	stateName := req.GetStateName()

	s.RWRequest.Lock()
	defer s.RWRequest.Unlock()

	// autosave triggered by apt or other system on non zsys system. Do nothing
	if !s.Machines.CurrentIsZsys() && req.GetAutosave() {
		return nil
	}

	if stateName != "" {
		log.Infof(stream.Context(), i18n.G("Requesting to save current system state %q"), stateName)
	} else {
		msg := i18n.G("Requesting to save current system state")
		// Always print the message as it was automatically requested
		if req.GetAutosave() {
			log.RemotePrint(stream.Context(), msg)
		} else {
			log.Info(stream.Context(), msg)
		}
	}

	if stateName, err = s.Machines.CreateSystemSnapshot(stream.Context(), stateName); err != nil {
		return fmt.Errorf(i18n.G("couldn't save system state: ")+config.ErrorFormat, err)
	}

	if req.GetUpdateBootMenu() {
		if err := updateBootMenu(stream.Context()); err != nil {
			return err
		}
	}

	stream.Send(&zsys.CreateSaveStateResponse{
		Reply: &zsys.CreateSaveStateResponse_StateName{StateName: stateName},
	})

	return nil
}

// SaveUserState creates a snapshot for the provided user.
// If snapshotName is not empty, it is used as the id of the snapshot otherwise an id
// is generated with a random string.
// userName is the name of the user to snapshot the datasets from.
func (s *Server) SaveUserState(req *zsys.SaveUserStateRequest, stream zsys.Zsys_SaveUserStateServer) (err error) {
	userName := req.GetUserName()
	fmt.Println(userName)

	if err := s.authorizer.IsAllowedFromContext(context.WithValue(stream.Context(), authorizer.OnUserKey, userName),
		authorizer.ActionUserWrite); err != nil {
		return err
	}

	stateName := req.GetStateName()

	s.RWRequest.Lock()
	defer s.RWRequest.Unlock()

	if stateName != "" {
		log.Infof(stream.Context(), i18n.G("Requesting to save state %q for user %q"), stateName, userName)
	} else {
		log.Infof(stream.Context(), i18n.G("Requesting to save state for user %q"), userName)
	}

	if stateName, err = s.Machines.CreateUserSnapshot(stream.Context(), userName, stateName); err != nil {
		return fmt.Errorf(i18n.G("couldn't save state for user %q:")+config.ErrorFormat, userName, err)
	}

	stream.Send(&zsys.CreateSaveStateResponse{
		Reply: &zsys.CreateSaveStateResponse_StateName{StateName: stateName},
	})

	return nil
}

// RemoveSystemState removes this and all depending states from system.
func (s *Server) RemoveSystemState(req *zsys.RemoveSystemStateRequest, stream zsys.Zsys_RemoveSystemStateServer) (err error) {
	if err := s.authorizer.IsAllowedFromContext(stream.Context(), authorizer.ActionSystemWrite); err != nil {
		return err
	}

	stateName := req.GetStateName()

	s.RWRequest.Lock()
	defer s.RWRequest.Unlock()

	if stateName == "" {
		return fmt.Errorf(i18n.G("System state name is required"))
	}

	log.Infof(stream.Context(), i18n.G("Requesting to remove system state %q"), stateName)

	err = s.Machines.RemoveState(stream.Context(), stateName, "", req.GetForce(), req.GetDryrun())
	if err != nil {
		var e *machines.ErrStateRemovalNeedsConfirmation
		if errors.As(err, &e) {
			st := status.New(codes.FailedPrecondition, config.UserConfirmationNeeded)
			stdetails, err := st.WithDetails(&errdetails.ErrorInfo{
				Type:   config.UserConfirmationNeeded,
				Domain: "",
				Metadata: map[string]string{
					"msg": e.Error(),
				},
			})
			if err != nil {
				return st.Err()
			}

			return stdetails.Err()
		}
		return fmt.Errorf(i18n.G("couldn't remove system state %s: ")+config.ErrorFormat, stateName, err)
	}

	if req.GetDryrun() {
		return nil
	}
	return updateBootMenu(stream.Context())
}

// RemoveUserState removes a user state
func (s *Server) RemoveUserState(req *zsys.RemoveUserStateRequest, stream zsys.Zsys_RemoveUserStateServer) error {
	userName := req.GetUserName()

	if err := s.authorizer.IsAllowedFromContext(context.WithValue(stream.Context(), authorizer.OnUserKey, userName),
		authorizer.ActionUserWrite); err != nil {
		return err
	}

	stateName := req.GetStateName()

	s.RWRequest.Lock()
	defer s.RWRequest.Unlock()

	if stateName == "" {
		return fmt.Errorf(i18n.G("State name is required"))
	}

	log.Infof(stream.Context(), i18n.G("Requesting to remove user state %q for user %s"), stateName, userName)

	err := s.Machines.RemoveState(stream.Context(), stateName, userName, req.GetForce(), req.GetDryrun())
	if err != nil {
		var e *machines.ErrStateRemovalNeedsConfirmation
		if errors.As(err, &e) {
			st := status.New(codes.FailedPrecondition, config.UserConfirmationNeeded)
			stdetails, err := st.WithDetails(&errdetails.ErrorInfo{
				Type:   config.UserConfirmationNeeded,
				Domain: "",
				Metadata: map[string]string{
					"msg": e.Error(),
				},
			})
			if err != nil {
				return st.Err()
			}

			return stdetails.Err()
		}
		return fmt.Errorf(i18n.G("couldn't remove user state %s: ")+config.ErrorFormat, stateName, err)
	}

	return nil
}
