package daemon

import (
	"context"
	"fmt"
	"os/exec"

	"github.com/ubuntu/zsys"
	"github.com/ubuntu/zsys/internal/authorizer"
	"github.com/ubuntu/zsys/internal/config"
	"github.com/ubuntu/zsys/internal/i18n"
	"github.com/ubuntu/zsys/internal/log"
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

	if stateName != "" {
		log.Infof(stream.Context(), i18n.G("Requesting saving current system state %q"), stateName)
	} else {
		log.Info(stream.Context(), i18n.G("Requesting saving current system state"))
	}

	if err := s.Machines.CreateSystemSnapshot(stream.Context(), stateName); err != nil {
		return fmt.Errorf(i18n.G("couldn't save system state: ")+config.ErrorFormat, err)
	}

	cmd := exec.Command(updateGrubCmd)
	logger := &logWriter{ctx: stream.Context()}
	cmd.Stdout = logger
	cmd.Stderr = logger
	if err := cmd.Run(); err != nil {
		return fmt.Errorf(i18n.G("%q returned an error: ")+config.ErrorFormat, updateGrubCmd, err)
	}
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
		log.Infof(stream.Context(), i18n.G("Requesting saving state %q for user %q"), stateName, userName)
	} else {
		log.Infof(stream.Context(), i18n.G("Requesting saving state for user %q"), userName)
	}

	if err := s.Machines.CreateUserSnapshot(stream.Context(), userName, stateName); err != nil {
		return fmt.Errorf(i18n.G("couldn't save state for user %q:")+config.ErrorFormat, userName, err)
	}

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

	log.Infof(stream.Context(), i18n.G("Requesting removing system state %q"), stateName)

	sysStates, additionalUserStates, err := s.Machines.GetStateAndDependencies(stateName)
	if err != nil {
		return err
	}

	if !req.GetForce() {
		if len(sysStates) > 1 || len(additionalUserStates) > 1 {
			var statesToRemoveMsg string
			if len(sysStates) > 1 {
				statesToRemoveMsg = fmt.Sprintf(i18n.G("Removing %s will also remove the following system states:\n"), stateName)
				for _, s := range sysStates[1:] {
					statesToRemoveMsg += fmt.Sprintf(i18n.G(" - %s (%s)\n"), s.ID, s.LastUsed)
				}
			}
			if len(additionalUserStates) > 0 {
				statesToRemoveMsg += fmt.Sprintf(i18n.G("Removing %s will also remove the following user states:\n"), stateName)
				for _, s := range additionalUserStates {
					statesToRemoveMsg += fmt.Sprintf(i18n.G(" - %s (%s)\n"), s.ID, s.LastUsed)
				}
			}
			stream.Send(&zsys.RemoveStateResponse{Reply: &zsys.RemoveStateResponse_AdditionalRemovals{
				AdditionalRemovals: statesToRemoveMsg}})
			return nil
		}
	}

	if err := s.Machines.RemoveSystemStates(stream.Context(), sysStates); err != nil {
		return fmt.Errorf(i18n.G("couldn't remove system state %s: ")+config.ErrorFormat, stateName, err)
	}

	cmd := exec.Command(updateGrubCmd)
	logger := &logWriter{ctx: stream.Context()}
	cmd.Stdout = logger
	cmd.Stderr = logger
	if err := cmd.Run(); err != nil {
		return fmt.Errorf(i18n.G("%q returned an error: ")+config.ErrorFormat, updateGrubCmd, err)
	}
	return nil
}

func (s *Server) RemoveUserState(req *zsys.RemoveUserStateRequest, stream zsys.Zsys_RemoveUserStateServer) error {
	userName := req.GetUserName()

	if err := s.authorizer.IsAllowedFromContext(context.WithValue(stream.Context(), authorizer.OnUserKey, userName),
		authorizer.ActionUserWrite); err != nil {
		return err
	}

	stateName := req.GetStateName()

	s.RWRequest.Lock()
	defer s.RWRequest.Unlock()

	log.Infof(stream.Context(), i18n.G("Requesting removing user state %q for user %s"), stateName, userName)

	userStates, err := s.Machines.GetUserStateAndDependencies(userName, stateName, false)
	if err != nil {
		return err
	}

	if !req.GetForce() {
		if len(userStates) > 1 {
			statesToRemoveMsg := fmt.Sprintf(i18n.G("Removing %s will also remove the following user states:\n"), stateName)
			for _, s := range userStates[1:] {
				statesToRemoveMsg += fmt.Sprintf(i18n.G(" - %s (%s)\n"), s.ID, s.LastUsed)
			}
			stream.Send(&zsys.RemoveStateResponse{Reply: &zsys.RemoveStateResponse_AdditionalRemovals{
				AdditionalRemovals: statesToRemoveMsg}})
			return nil
		}
	}

	if err := s.Machines.RemoveUserStates(stream.Context(), userStates, ""); err != nil {
		return fmt.Errorf(i18n.G("couldn't remove state %s for user %s: ")+config.ErrorFormat, stateName, userName, err)
	}

	return nil
}
