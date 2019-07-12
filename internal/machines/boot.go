package machines

import (
	"math/rand"
	"strings"
	"sync"
	"time"

	"github.com/ubuntu/zsys/internal/config"
	"github.com/ubuntu/zsys/internal/zfs"
	"golang.org/x/xerrors"

	log "github.com/sirupsen/logrus"
)

// ZfsPropertyCloneScanner interface can clone, set dataset property and scan
type ZfsPropertyCloneScanner interface {
	Clone(name, suffix string, skipBootfs, recursive bool, options ...zfs.DatasetCloneOption) (errClone error)
	Scan() ([]zfs.Dataset, error)
	zfsPropertySetter
}

// zfsPropertySetter can only SetProperty
type zfsPropertySetter interface {
	SetProperty(name, value, datasetName string, force bool) error
}

// EnsureBoot consolidates canmount states for early boot.
// It will create any needed clones and userdata if required.
// A transactional zfs element should be passed to optionally revert if an error is returned (only part of the datasets
// were changed).
// We ensure that we don't modify any existing tags (those will be done in commit()) so that failing boots didn't modify
// the system, apart for canmount auto/on which are consolidated unconditionnally on each boot anyway.
// Note that a rescan if performed if any modifications change the dataset layout.
// TODO: propagate error to user
func (machines *Machines) EnsureBoot(z ZfsPropertyCloneScanner, cmdline string) error {
	if !machines.current.IsZsys {
		log.Debugln("current machine isn't Zsys, nothing to do")
		return nil
	}

	root, revertUserData := parseCmdLine(cmdline)
	m, bootedState := machines.findFromRoot(root)

	bootedOnSnapshot := hasBootedOnSnapshot(cmdline)
	// We are creating new clones (bootfs and optionnally, userdata)
	if bootedOnSnapshot {
		// get current generated suffix by initramfs
		var suffix string
		if j := strings.LastIndex(bootedState.ID, "_"); j > 0 && !strings.HasSuffix(bootedState.ID, "_") {
			suffix = bootedState.ID[j+1:]
		} else {
			return xerrors.Errorf("Mounted clone bootFS dataset created by initramfs doesn't have a valid _suffix (at least .*_<onechar>): %q", s.ID)
		}

		if err := z.Clone(root, suffix, true, true); err != nil {
			return xerrors.Errorf("couldn't create new subdatasets from %q: %v", root, err)
		}

		// Handle userdata by creating a new clone in case a revert was requested
		var userDataSuffix string
		if revertUserData {
			// Find user datasets attached to the snapshot and clone them
			userDataSuffix = generateID(6)
			snapshot := m.History[root]
			for _, d := range snapshot.UserDatasets {
				// Don't do recursive cloning. The datasets are ordered, but not necessarily child of each other.
				// We will tag them at the same time.
				if err := z.Clone(d.Name, userDataSuffix, false, true); err != nil {
					return xerrors.Errorf("couldn't create new user datasets from %q: %v", root, err)
				}
			}
		}

		// Rescan here for getting accessing to new cloned datasets
		ds, err := z.Scan()
		if err != nil {
			return xerrors.Errorf("couldn't rescan after modifying boot: "+config.ErrorFormat, err)
		}
		*machines = New(ds, cmdline)

		// reassociate newly cloned userdata to state (the userDatasets liste is empty as those were not tagged yet)
		if revertUserData {
			m, bootedState = machines.findFromRoot(root) // We did rescan, refresh pointers
			var newUserDatasets []zfs.Dataset
			for _, d := range ds {
				if !strings.Contains(strings.ToLower(d.Name), userdatasetsContainerName) {
					continue
				}
				if strings.HasSuffix(d.Name, "_"+userDataSuffix) || strings.Contains(d.Name, "_"+userDataSuffix+"/") {
					newUserDatasets = append(newUserDatasets, d)
					continue
				}
			}
			bootedState.UserDatasets = newUserDatasets
		}
	}

	// We don't revert userdata, so we are using main state machine userdata to keep on the same track.
	// It's a no-op if the active state was the main one already.
	// In case of system revert (either from cloning or rebooting this older dataset without user data revert), the newly
	// active state won't be the main one, and so, we only take its main state userdata.
	if !revertUserData {
		bootedState.UserDatasets = m.UserDatasets
	}

	var needRescan bool
	// Start switching every non desired system and user datasets to noauto
	noAutoDatasets := diffDatasets(machines.allSystemDatasets, bootedState.SystemDatasets)
	noAutoDatasets = append(noAutoDatasets, diffDatasets(machines.allUsersDatasets, bootedState.UserDatasets)...)
	for _, d := range noAutoDatasets {
		if d.CanMount != "on" {
			continue
		}
		if err := z.SetProperty(zfs.CanmountProp, "noauto", d.Name, false); err != nil {
			return xerrors.Errorf("couldn't switch %q canmount property to noauto: "+config.ErrorFormat, d.Name, err)
		}
		needRescan = true
	}

	// Switch current state system and user datasets to on
	autoDatasets := append(bootedState.SystemDatasets, bootedState.UserDatasets...)
	for _, d := range noAutoDatasets {
		if d.CanMount != "noauto" {
			continue
		}
		if err := z.SetProperty(zfs.CanmountProp, "on", d.Name, false); err != nil {
			return xerrors.Errorf("couldn't switch %q canmount property to on: "+config.ErrorFormat, d.Name, err)
		}
		needRescan = true
	}

	// Rescan if we changed the dataset properties
	if needRescan {
		ds, err := z.Scan()
		if err != nil {
			return xerrors.Errorf("couldn't rescan after modifying boot: "+config.ErrorFormat, err)
		}
		*machines = New(ds, cmdline)
	}

	// Change the state to set current state as main machine state may have changed
	if bootedOnSnapshot {
		machines.ensureCurrentState(cmdline)
	}

	return nil
}

var seedOnce = sync.Once{}

// generateID with n ascii or digits, lowercase, characters
func generateID(n int) string {
	seedOnce.Do(func() { rand.Seed(time.Now().UnixNano()) })

	var allowedRunes = []rune("abcdefghijklmnopqrstuvwxyz0123456789")

	b := make([]rune, n)
	for i := range b {
		b[i] = allowedRunes[rand.Intn(len(allowedRunes))]
	}
	return string(b)
}

// diffDatasets returns datasets in a that aren't in b
func diffDatasets(a, b []zfs.Dataset) []zfs.Dataset {
	mb := make(map[string]struct{}, len(b))
	for _, x := range b {
		mb[x.Name] = struct{}{}
	}
	var diff []zfs.Dataset
	for _, x := range a {
		if _, found := mb[x.Name]; !found {
			diff = append(diff, x)
		}
	}
	return diff
}
