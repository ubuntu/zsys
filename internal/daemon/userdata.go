package daemon

import (
	"fmt"

	"github.com/ubuntu/zsys"
	"github.com/ubuntu/zsys/internal/config"
	"github.com/ubuntu/zsys/internal/log"
	"github.com/ubuntu/zsys/internal/zfs"
)

// CreateUserData creates a new userdata for user and set it to homepath on current zsys system.
// if the user already exists for a dataset attached to the current system, set its mountpoint to homepath.
// This is called by zsys grpc request, once the server is registered
func (s *Server) CreateUserData(req *zsys.CreateUserDataRequest, stream zsys.Zsys_CreateUserDataServer) (err error) {
	user := req.GetUser()
	homepath := req.GetHomepath()
	log.Infof(stream.Context(), "Create user dataset for %q on %q", user, homepath)

	z := zfs.NewWithAutoCancel(stream.Context())
	defer z.DoneCheckErr(&err)

	if err := s.Machines.CreateUserData(stream.Context(), user, homepath, z); err != nil {
		return fmt.Errorf("couldn't create userdataset for %q: "+config.ErrorFormat, homepath, err)
	}
	return nil
}

// ChangeHomeOnUserData tries to find an existing dataset matching home as a valid mountpoint and rename it to newhome
func (s *Server) ChangeHomeOnUserData(req *zsys.ChangeHomeOnUserDataRequest, stream zsys.Zsys_ChangeHomeOnUserDataServer) (err error) {
	home := req.GetHome()
	newHome := req.GetNewHome()
	log.Infof(stream.Context(), "Rename home user dataset from %q to %q", home, newHome)

	z := zfs.NewWithAutoCancel(stream.Context())
	defer z.DoneCheckErr(&err)

	if err := s.Machines.ChangeHomeOnUserData(stream.Context(), home, newHome, z); err != nil {
		return fmt.Errorf("couldn't change home userdataset for %q: "+config.ErrorFormat, home, err)
	}
	return nil
}
