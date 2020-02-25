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
	"github.com/ubuntu/zsys/internal/i18n"
	"github.com/ubuntu/zsys/internal/log"
	"github.com/ubuntu/zsys/internal/zfs"
	"github.com/ubuntu/zsys/internal/zfs/libzfs"
)

// CreateUserData creates a new dataset for homepath and attach to current system.
// It creates intermediates user datasets if needed.
func (ms *Machines) CreateUserData(ctx context.Context, user, homepath string) error {
	if !ms.current.isZsys() {
		return errors.New(i18n.G("Current machine isn't Zsys, nothing to create"))
	}
	if user == "" {
		return errors.New(i18n.G("Needs a valid user name, got nothing"))
	}
	if homepath == "" {
		return errors.New(i18n.G("Needs a valid home path, got nothing"))
	}

	t, cancel := ms.z.NewTransaction(ctx)
	defer t.Done()

	// If there this user already attached to this machine: retarget home
	reused, err := ms.tryReuseUserDataSet(t, user, "", homepath)
	if err != nil {
		cancel()
		return err
	} else if reused {
		return ms.Refresh(ctx)
	}

	log.Infof(ctx, i18n.G("Create user dataset for %q"), homepath)

	// Take same pool as existing userdatasets for current system
	var userdatasetRoot string
	for p := range ms.current.UserDatasets {
		userdatasetRoot = getUserDatasetPath(p)
		break
	}
	// If there is none attached to the current system, try to take first existing userdataset detected pool
	if userdatasetRoot == "" && len(ms.allUsersDatasets) > 0 {
		userdatasetRoot = getUserDatasetPath(ms.allUsersDatasets[0].Name)
	}

	// If there is still none found, check if there is only USERDATA with no user under it as it won't shows up in machines
	if userdatasetRoot == "" {
		for _, d := range ms.z.Datasets() {
			if strings.HasSuffix(strings.ToLower(d.Name)+"/", userdatasetsContainerName) {
				userdatasetRoot = d.Name
				break
			}
		}
	}

	// If there is still none found, take the current system pool and create one
	if userdatasetRoot == "" {
		p := ms.current.ID
		i := strings.Index(p, "/")
		if i != -1 {
			p = p[0:i]
		}
		userdatasetRoot = filepath.Join(p, "USERDATA")

		// Create parent USERDATA
		if err := t.Create(userdatasetRoot, "/", "off"); err != nil {
			cancel()
			return fmt.Errorf(i18n.G("couldn't create user data embedder dataset: ")+config.ErrorFormat, err)
		}
	}

	userdataset := filepath.Join(userdatasetRoot, fmt.Sprintf("%s_%s", user, t.Zfs.GenerateID(6)))
	if err := t.Create(userdataset, homepath, "on"); err != nil {
		cancel()
		return err
	}

	// Tag to associate with current system and lastUsed
	if err := t.SetProperty(libzfs.BootfsDatasetsProp, ms.current.ID, userdataset, false); err != nil {
		cancel()
		return fmt.Errorf(i18n.G("couldn't add %q to BootfsDatasets property of %q: ")+config.ErrorFormat, ms.current.ID, userdataset, err)
	}

	currentTime := strconv.Itoa(int(time.Now().Unix()))
	if err := t.SetProperty(libzfs.LastUsedProp, currentTime, userdataset, false); err != nil {
		cancel()
		return fmt.Errorf(i18n.G("couldn't set last used time to %q: ")+config.ErrorFormat, currentTime, err)
	}

	return ms.Refresh(ctx)
}

// ChangeHomeOnUserData tries to find an existing dataset matching home as a valid mountpoint and rename it to newhome
func (ms *Machines) ChangeHomeOnUserData(ctx context.Context, home, newHome string) error {
	if !ms.current.isZsys() {
		return errors.New(i18n.G("Current machine isn't Zsys, nothing to modify"))
	}
	if home == "" {
		return fmt.Errorf(i18n.G("can't use empty string for existing home directory"))
	}
	if newHome == "" {
		return fmt.Errorf(i18n.G("can't use empty string for new home directory"))
	}

	t, cancel := ms.z.NewTransaction(ctx)
	defer t.Done()

	log.Infof(ctx, i18n.G("Reset user dataset path from %q to %q"), home, newHome)
	found, err := ms.tryReuseUserDataSet(t, "", home, newHome)
	if err != nil {
		cancel()
		return err
	}

	if !found {
		cancel()
		return fmt.Errorf(i18n.G("didn't find any existing dataset matching %q"), home)
	}
	return ms.Refresh(ctx)
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
func (ms *Machines) tryReuseUserDataSet(t *zfs.Transaction, user string, oldhome, newhome string) (bool, error) {
	log.Debugf(t.Context(), i18n.G("Trying to check if there is a user or home directory already attached to this machine"))

	// If there this user or home already attached to this machine: retarget home
	for _, ds := range ms.current.UserDatasets {
		for _, d := range ds {
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
					return false, fmt.Errorf(i18n.G("%q is already associated to %q, which is for a different user name (%q) than %q"), newhome, d.Name, userName, user)
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
				log.Infof(t.Context(), i18n.G("Reusing %q as matching user name or old mountpoint"), d.Name)
				if err := t.SetProperty(libzfs.MountPointProp, newhome, d.Name, false); err != nil {
					return false, fmt.Errorf(i18n.G("couldn't set new home %q to %q: ")+config.ErrorFormat, newhome, d.Name, err)
				}
				return true, nil
			}
		}
	}

	return false, nil
}
