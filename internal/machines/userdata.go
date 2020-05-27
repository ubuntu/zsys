package machines

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
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
selectUserDataset:
	for _, us := range ms.current.Users {
		for p := range us.Datasets {
			userdatasetRoot = getUserDatasetRoot(p)
			break selectUserDataset
		}
	}
	// If there is none attached to the current system, try to take first existing userdataset detected pool
	if userdatasetRoot == "" && len(ms.allUsersDatasets) > 0 {
		userdatasetRoot = getUserDatasetRoot(ms.allUsersDatasets[0].Name)
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
			p = p[:i]
		}
		userdatasetRoot = filepath.Join(p, zfs.UserdataPrefix)

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
	// FIXME: mount the dataset here, we should have that in Create() but mitigate the impact for focal release
	if err := syscall.Mount(userdataset, homepath, "zfs", 0, "zfsutil"); err != nil {
		log.Warningf(ctx, i18n.G("Couldn't mount %s: %v"), homepath, err)
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

// DissociateUser tries to unattach current user dataset to current system state
func (ms *Machines) DissociateUser(ctx context.Context, username string) error {
	if !ms.current.isZsys() {
		return errors.New(i18n.G("Current machine isn't Zsys, nothing to modify"))
	}
	if username == "" {
		return fmt.Errorf(i18n.G("need an user name"))
	}

	log.Infof(ctx, i18n.G("Dissociate user %q from current state"), username)

	// If there this user or home already attached to this machine: retarget home
	us, ok := ms.current.Users[username]
	if !ok {
		return fmt.Errorf(i18n.G("user %q not found on current state"), username)
	}

	t, cancel := ms.z.NewTransaction(ctx)
	defer t.Done()

	for _, ds := range us.Datasets {
		for _, d := range ds {
			var newTags []string
			for _, n := range strings.Split(d.BootfsDatasets, bootfsdatasetsSeparator) {
				if n != ms.current.ID {
					newTags = append(newTags, n)
					break
				}
			}

			newTag := strings.Join(newTags, bootfsdatasetsSeparator)

			if newTag == d.BootfsDatasets {
				continue
			}

			log.Debugf(ctx, i18n.G("Setting new bootfs tag %s on %s\n"), newTag, d.Name)
			if err := t.SetProperty(libzfs.BootfsDatasetsProp, newTag, d.Name, false); err != nil {
				cancel()
				return fmt.Errorf(i18n.G("couldn't remove %q to BootfsDatasets property of %q: ")+config.ErrorFormat, ms.current.ID, d.Name, err)
			}
			if err := t.SetProperty(libzfs.CanmountProp, "noauto", d.Name, false); err != nil {
				cancel()
				return fmt.Errorf(i18n.G("couldn't set %q to canmount=noauto: ")+config.ErrorFormat, ms.current.ID, d.Name, err)
			}
		}
	}

	return ms.Refresh(ctx)
}

func getUserDatasetRoot(path string) string {
	lpath := strings.ToLower(path)
	i := strings.Index(lpath, userdatasetsContainerName)
	if i == -1 {
		return ""
	}
	return path[:i+len(userdatasetsContainerName)]
}

// tryReuseUserDataSet tries to match an existing user dataset for the current machine.
// user match is used first, if empty, it will try to match old home directory.
func (ms *Machines) tryReuseUserDataSet(t *zfs.Transaction, user string, oldhome, newhome string) (bool, error) {
	log.Debugf(t.Context(), i18n.G("Trying to check if there is a user or home directory already attached to this machine"))

	// If there this user or home already attached to this machine: retarget home
	for userName, us := range ms.current.Users {
		for _, ds := range us.Datasets {
			for _, d := range ds {
				var match bool
				// try handling user dataset
				if user != "" {
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
					var uid, gid int
					if oldhome != "" {
						if info, err := os.Stat(oldhome); err == nil {
							if stat, ok := info.Sys().(*syscall.Stat_t); ok {
								uid = int(stat.Uid)
								gid = int(stat.Gid)
							}
						}

					}
					if err := t.SetProperty(libzfs.MountPointProp, newhome, d.Name, false); err != nil {
						return false, fmt.Errorf(i18n.G("couldn't set new home %q to %q: ")+config.ErrorFormat, newhome, d.Name, err)
					}
					// Reset owner on newly created mountpoint
					// FIXME: this should be in zfs itself when changing mount property and restore all properties on mountpoint itself
					if uid != 0 {
						if err := syscall.Unmount(newhome, 0); err == nil {
							if err := os.Chown(newhome, uid, gid); err != nil {
								log.Warningf(t.Context(), i18n.G("Couldn't restore permission on new home directory %s: %v"), newhome, err)
							}
							if err := syscall.Mount(d.Name, newhome, "zfs", 0, "zfsutil"); err != nil {
								log.Warningf(t.Context(), i18n.G("Couldn't mount %s: %v"), newhome, err)
							}
						}
					}
					if err := os.Remove(oldhome); err != nil {
						log.Warningf(t.Context(), i18n.G("couldn't cleanup %s directory: %v"), oldhome, err)
					}
					return true, nil
				}
			}
		}
	}

	return false, nil
}
