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
func (s *Server) CreateUserData(req *zsys.CreateUserDataRequest, stream zsys.Zsys_CreateUserDataServer) error {
	user := req.GetUser()
	homepath := req.GetHomepath()
	log.Infof(stream.Context(), "CreateUserData request received for %q on %q", user, homepath)

	ms, err := getMachines(stream.Context(), zfs.New(stream.Context()))
	if err != nil {
		return err
	}

	z := zfs.New(stream.Context(), zfs.WithTransactions())
	defer func() {
		if err != nil {
			z.Cancel()
			err = fmt.Errorf("couldn't create userdataset for %q: "+config.ErrorFormat, homepath, err)
		} else {
			z.Done()
		}
	}()

	return ms.CreateUserData(stream.Context(), user, homepath, z)
}

// ChangeHomeOnUserData tries to find an existing dataset matching home as a valid mountpoint and rename it to newhome
func (s *Server) ChangeHomeOnUserData(req *zsys.ChangeHomeOnUserDataRequest, stream zsys.Zsys_ChangeHomeOnUserDataServer) error {
	home := req.GetHome()
	newHome := req.GetNewHome()
	log.Infof(stream.Context(), "ChangeHomeOnUserData request received to rename %q to %q", home, newHome)

	ms, err := getMachines(stream.Context(), zfs.New(stream.Context()))
	if err != nil {
		return err
	}

	z := zfs.New(stream.Context(), zfs.WithTransactions())
	defer func() {
		if err != nil {
			z.Cancel()
			err = fmt.Errorf("couldn't change home userdataset for %q: "+config.ErrorFormat, home, err)
		} else {
			z.Done()
		}
	}()

	return ms.ChangeHomeOnUserData(stream.Context(), home, newHome, z)
}
