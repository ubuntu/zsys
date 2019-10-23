package machines

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/ubuntu/zsys/internal/config"
	"github.com/ubuntu/zsys/internal/log"
	"github.com/ubuntu/zsys/internal/zfs"
	"golang.org/x/xerrors"
)

// ZfsSetPropertyScanCreater can only Create, Scan and SetProperty on datasets
type ZfsSetPropertyScanCreater interface {
	Create(path, mountpoint, canmount string) error
	zfsScanner
	zfsPropertySetter
}

// zfsSetPropertyScanner can only SetProperty and Scan
type zfsSetPropertyScanner interface {
	zfsScanner
	zfsPropertySetter
}

// CreateUserData creates a new dataset for homepath and attach to current system.
// It creates intermediates user datasets if needed.
func (ms *Machines) CreateUserData(ctx context.Context, user, homepath string, z ZfsSetPropertyScanCreater) error {
	if !ms.current.isZsys() {
		return errors.New("Current machine isn't Zsys, nothing to create")
	}

	if user == "" {
		return errors.New("Needs a valid user name, got nothing")
	}
	if homepath == "" {
		return errors.New("Needs a valid home path, got nothing")
	}
	// If there this user already attached to this machine: retarget home
	reused, err := ms.tryReuseUserDataSet(ctx, user, "", homepath, z)
	if err != nil {
		return err
	} else if reused {
		return nil
	}

	log.Infof(ctx, "Create user dataset for %q", homepath)

	// Take same pool as existing userdatasets for current system
	userdatasetRoot := ""
	if len(ms.current.UserDatasets) > 0 {
		userdatasetRoot = getUserDatasetPath(ms.current.UserDatasets[0].Name)
	}
	// If there is none attached to the current system, try to take first existing userdataset detected pool
	if userdatasetRoot == "" && len(ms.allUsersDatasets) > 0 {
		userdatasetRoot = getUserDatasetPath(ms.allUsersDatasets[0].Name)
	}

	// If there is still none found, check if there is only USERDATA with no user under it as it won't shows up in machines
	if userdatasetRoot == "" {
		ds, err := z.Scan()
		if err != nil {
			// don't fail if Scan is failing, as the dataset was created
			return xerrors.Errorf("couldn't rescan for checking empty USERDATA: "+config.ErrorFormat, err)
		}
		for _, d := range ds {
			if strings.HasSuffix(strings.ToLower(d.Name)+"/", userdatasetsContainerName) {
				userdatasetRoot = d.Name
				break
			}
		}
	}

	// If there is still none found, take the current system pool and create one
	if userdatasetRoot == "" {
		p := ms.current.SystemDatasets[0].Name
		i := strings.Index(p, "/")
		if i != -1 {
			p = p[0:i]
		}
		userdatasetRoot = filepath.Join(p, "USERDATA")

		// Create parent USERDATA
		if err := z.Create(userdatasetRoot, "/", "off"); err != nil {
			return xerrors.Errorf("couldn't create user data embedder dataset: "+config.ErrorFormat, err)
		}
	}

	userdataset := filepath.Join(userdatasetRoot, fmt.Sprintf("%s_%s", user, generateID(6)))
	if err := z.Create(userdataset, homepath, "on"); err != nil {
		return err
	}

	// Tag to associate with current system and lastUsed
	if err := z.SetProperty(zfs.BootfsDatasetsProp, ms.current.ID, userdataset, false); err != nil {
		return xerrors.Errorf("couldn't add %q to BootfsDatasets property of %q: "+config.ErrorFormat, ms.current.ID, userdataset, err)
	}

	currentTime := strconv.Itoa(int(time.Now().Unix()))
	if err := z.SetProperty(zfs.LastUsedProp, currentTime, userdataset, false); err != nil {
		return xerrors.Errorf("couldn't set last used time to %q: "+config.ErrorFormat, currentTime, err)
	}

	// Rescan datasets, with new user datasets
	ds, err := z.Scan()
	if err != nil {
		// don't fail if Scan is failing, as the dataset was created
		log.Warningf(ctx, "couldn't rescan after committing boot: "+config.ErrorFormat, err)
	} else {
		*ms = New(ctx, ds, ms.cmdline)
	}

	return nil
}

// ChangeHomeOnUserData tries to find an existing dataset matching home and rename it to newhome
func (ms *Machines) ChangeHomeOnUserData(ctx context.Context, home, newHome string, z ZfsSetPropertyScanCreater) error {
	if !ms.current.isZsys() {
		return errors.New("Current machine isn't Zsys, nothing to modify")
	}

	if home == "" {
		return xerrors.Errorf("can't use empty string for existing home directory")
	}
	if newHome == "" {
		return xerrors.Errorf("can't use empty string for new home directory")
	}

	log.Infof(ctx, "Reset user dataset path from %q to %q", home, newHome)
	found, err := ms.tryReuseUserDataSet(ctx, "", home, newHome, z)
	if err != nil {
		return err
	}

	if !found {
		return xerrors.Errorf("didn't find any existing dataset matching %q", home)
	}
	return nil
}

func getUserDatasetPath(path string) string {
	lpath := strings.ToLower(path)
	i := strings.Index(lpath, userdatasetsContainerName)
	if i == -1 {
		return ""
	}
	return path[0 : i+len(userdatasetsContainerName)]
}

// tryReuseUserDataSet tries to match an existing user dataset for the current machine.
// user match is used first, if empty, it will try to match old home directory.
func (ms *Machines) tryReuseUserDataSet(ctx context.Context, user string, oldhome, newhome string, z zfsSetPropertyScanner) (bool, error) {
	log.Debugf(ctx, "Trying to check if there is a user or home directory already attached to this machine")

	// If there this user or home already attached to this machine: retarget home
	for _, d := range ms.current.UserDatasets {

		var match bool
		// try handling user dataset
		if user != "" {
			// get user name from dataset
			n := strings.Split(d.Name, "/")
			userName := n[len(n)-1]
			n = strings.Split(userName, "_")
			userName = strings.Join(n[0:len(n)-1], "_")

			// Home path is already attached to current system, but with a different user name. Fail
			if d.Mountpoint == newhome && user != userName {
				return false, xerrors.Errorf("%q is already associated to %q, which is for a different user name (%q) than %q", newhome, d.Name, userName, user)
			}
			if userName == user {
				match = true
			}
		} else if oldhome != "" {
			if d.Mountpoint == oldhome {
				match = true
			}
		}

		// We'll reuse that dataset
		if match {
			log.Infof(ctx, "Reusing %q as matching user name or old mountpoint", d.Name)
			if err := z.SetProperty(zfs.MountPointProp, newhome, d.Name, false); err != nil {
				return false, xerrors.Errorf("couldn't set new home %q to %q: "+config.ErrorFormat, newhome, d.Name, err)
			}
			ds, err := z.Scan()
			if err != nil {
				// don't fail if Scan is failing, as the dataset was created
				log.Warningf(ctx, "Couldn't rescan after committing boot: "+config.ErrorFormat, err)
			} else {
				*ms = New(ctx, ds, ms.cmdline)
			}
			return true, nil
		}
	}

	return false, nil
}
