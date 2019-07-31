package machines

import (
	"math/rand"
	"strconv"
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
	Clone(name, suffix string, skipBootfs, recursive bool) (errClone error)
	Scan() ([]zfs.Dataset, error)
	zfsPropertySetter
}

// ZfsPropertyPromoteScanner interface can promote, set dataset property and scan
type ZfsPropertyPromoteScanner interface {
	Promote(name string) (errPromote error)
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
// the system, apart for canmount auto/on which are consolidated unconditionally on each boot anyway.
// Note that a rescan if performed if any modifications change the dataset layout. However, until ".Commit()" is called,
// machine.current will return the correct machine, but the main dataset switch won't be done. This allows us here and
// in .Commit()
// TODO: propagate error to user graphically
func (machines *Machines) EnsureBoot(z ZfsPropertyCloneScanner) error {
	if machines.current == nil || !machines.current.IsZsys {
		log.Infoln("current machine isn't Zsys, nothing to do on boot")
		return nil
	}

	root, revertUserData := bootParametersFromCmdline(machines.cmdline)
	m, bootedState := machines.findFromRoot(root)
	log.Infof("ensure boot on %q\n", root)

	bootedOnSnapshot := hasBootedOnSnapshot(machines.cmdline)
	// We are creating new clones (bootfs and optionnally, userdata) if wasn't promoted already
	if bootedOnSnapshot && machines.current.ID != bootedState.ID {
		log.Infof("booting on snapshot: %q to %q\n", root, bootedState.ID)
		// get current generated suffix by initramfs
		var suffix string
		if j := strings.LastIndex(bootedState.ID, "_"); j > 0 && !strings.HasSuffix(bootedState.ID, "_") {
			suffix = bootedState.ID[j+1:]
		} else {
			return xerrors.Errorf("Mounted clone bootFS dataset created by initramfs doesn't have a valid _suffix (at least .*_<onechar>): %q", bootedState.ID)
		}

		// Fetch every independent root datasets (like rpool, bpool, …) that needs to be cloned
		var datasetsToClone []string
		for _, n := range m.History[root].SystemDatasets {
			var found bool
			for _, rootName := range datasetsToClone {
				base, _ := splitSnapshotName(rootName)
				if strings.HasPrefix(n.Name, base+"/") {
					found = true
				}
			}
			if found {
				continue
			}
			datasetsToClone = append(datasetsToClone, n.Name)
		}

		// Skip bootfs datasets in the cloning phase. We assume any error would mean that EnsureBoot was called twice
		// before Commit() during this boot. A new boot will create a new suffix id, so we won't block the machine forever
		// in case of a real issue.
		// TODO: should test the clone return value (clone fails on system dataset already exists -> skip, other clone fails -> return error)
		for _, n := range datasetsToClone {
			log.Infof("cloning %q", n)
			if err := z.Clone(n, suffix, true, true); err != nil {
				// TODO: transaction fix (as it's now set in error)
				log.Warnf("couldn't create new subdatasets from %q. Assuming it has already been created successfully: %v", n, err)
			}
		}

		// Handle userdata by creating a new clone in case a revert was requested
		// We skip it if we booted on a snpashot with userdatasets already created. This would mean that EnsureBoot
		// was called twicebefore Commit() during this boot. A new boot will create a new suffix id, so we won't block
		// the machine forever in case of a real issue.
		var userDataSuffix string
		if revertUserData && !(bootedOnSnapshot && len(bootedState.UserDatasets) > 0) {
			log.Infoln("reverting user data")
			// Find user datasets attached to the snapshot and clone them
			// Only root datasets are cloned
			userDataSuffix = generateID(6)
			snapshot := m.History[root]
			var rootUserDatasets []zfs.Dataset
			for _, d := range snapshot.UserDatasets {
				parentFound := false
				for _, r := range rootUserDatasets {
					if base, _ := splitSnapshotName(r.Name); strings.Contains(d.Name, base+"/") {
						parentFound = true
						break
					}
				}
				if !parentFound {
					rootUserDatasets = append(rootUserDatasets, d)
					// Recursively clones childrens, which shouldn't have bootfs elements.
					if err := z.Clone(d.Name, userDataSuffix, false, true); err != nil {
						return xerrors.Errorf("couldn't create new user datasets from %q: %v", root, err)
					}
					// Associate this parent new user dataset to its parent system dataset
					base, _ := splitSnapshotName(d.Name)
					// Reformat the name with the new uuid and clone now the dataset.
					suffixIndex := strings.LastIndex(base, "_")
					userdatasetName := base[:suffixIndex] + "_" + userDataSuffix
					if err := z.SetProperty(zfs.BootfsDatasetsProp, bootedState.ID, userdatasetName, false); err != nil {
						return xerrors.Errorf("couldn't add %q to BootfsDatasets property of %q: "+config.ErrorFormat, bootedState.ID, d.Name, err)
					}
				}
			}
		}

		// Rescan here for getting accessing to new cloned datasets
		ds, err := z.Scan()
		if err != nil {
			return xerrors.Errorf("couldn't rescan after modifying boot: "+config.ErrorFormat, err)
		}
		*machines = New(ds, machines.cmdline)
		m, bootedState = machines.findFromRoot(root) // We did rescan, refresh pointers
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
		if d.CanMount != "on" || d.IsSnapshot {
			continue
		}
		log.Infof("switch dataset %q to mount noauto\n", d.Name)
		if err := z.SetProperty(zfs.CanmountProp, "noauto", d.Name, false); err != nil {
			return xerrors.Errorf("couldn't switch %q canmount property to noauto: "+config.ErrorFormat, d.Name, err)
		}
		needRescan = true
	}

	// Switch current state system and user datasets to on
	autoDatasets := append(bootedState.SystemDatasets, bootedState.UserDatasets...)
	for _, d := range autoDatasets {
		if d.CanMount != "noauto" {
			continue
		}
		log.Infof("switch dataset %q to mount on\n", d.Name)
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
		*machines = New(ds, machines.cmdline)
	}

	return nil
}

// Commit current state to be the active one by promoting its datasets if needed, set last used,
// associate user datasets to it and rebuilding grub menu.
// After this operation, every New() call will get the current and correct system state.
func (machines *Machines) Commit(z ZfsPropertyPromoteScanner) error {
	if machines.current == nil || !machines.current.IsZsys {
		log.Infoln("current machine isn't Zsys, nothing to commit on boot")
		return nil
	}

	root, revertUserData := bootParametersFromCmdline(machines.cmdline)
	m, bootedState := machines.findFromRoot(root)
	log.Infof("committing boot for %q\n", root)

	// Get user datasets. As we didn't tag the user datasets and promote the system one, the machines doesn't correspond
	// to the reality.

	// We don't revert userdata, so we are using main state machine userdata to keep on the same track.
	// It's a no-op if the active state was the main one already.
	// In case of system revert (either from cloning or rebooting this older dataset without user data revert), the newly
	// active state won't be the main one, and so, we only take its main state userdata.
	if !revertUserData {
		bootedState.UserDatasets = m.UserDatasets
	}

	// Untag non attached userdatasets
	for _, d := range diffDatasets(machines.allUsersDatasets, bootedState.UserDatasets) {
		if d.IsSnapshot {
			continue
		}
		var newTag string
		// Multiple elements, strip current bootfs dataset name
		if d.BootfsDatasets != "" && d.BootfsDatasets != bootedState.ID {
			newTag = strings.Replace(d.BootfsDatasets, bootedState.ID+":", "", -1)
			newTag = strings.TrimSuffix(newTag, ":"+bootedState.ID)
		}
		if newTag == d.BootfsDatasets {
			continue
		}
		log.Infof("untagging user dataset: %q\n", d.Name)
		if err := z.SetProperty(zfs.BootfsDatasetsProp, newTag, d.Name, false); err != nil {
			return xerrors.Errorf("couldn't remove %q to BootfsDatasets property of %q:"+config.ErrorFormat, bootedState.ID, d.Name, err)
		}
	}
	// Tag userdatasets to associate with this successful boot state, if wasn't tagged already
	// (case of no user data revert, associate with different previous main system)
	for _, d := range bootedState.UserDatasets {
		if d.BootfsDatasets == bootedState.ID ||
			strings.Contains(d.BootfsDatasets, bootedState.ID+":") ||
			strings.HasSuffix(d.BootfsDatasets, ":"+bootedState.ID) {
			continue
		}
		log.Infof("tag current user dataset: %q\n", d.Name)
		newTag := d.BootfsDatasets + ":" + bootedState.ID
		if err := z.SetProperty(zfs.BootfsDatasetsProp, newTag, d.Name, false); err != nil {
			return xerrors.Errorf("couldn't add %q to BootfsDatasets property of %q: "+config.ErrorFormat, bootedState.ID, d.Name, err)
		}
	}

	// System and users datasets: set lastUsed
	currentTime := strconv.Itoa(int(time.Now().Unix()))
	log.Infof("set current time to %q\n", currentTime)
	for _, d := range append(bootedState.SystemDatasets, bootedState.UserDatasets...) {
		if err := z.SetProperty(zfs.LastUsedProp, currentTime, d.Name, false); err != nil {
			return xerrors.Errorf("couldn't set last used time to %q: "+config.ErrorFormat, currentTime, err)
		}
	}

	kernel := kernelFromCmdline(machines.cmdline)
	log.Infof("set latest booted kernel to %q\n", kernel)
	if err := z.SetProperty(zfs.LastBootedKernelProp, kernel, bootedState.SystemDatasets[0].Name, false); err != nil {
		return xerrors.Errorf("couldn't set last booted kernel to %q "+config.ErrorFormat, kernel, err)
	}

	// Promotion needed for system and user datasets
	for _, d := range bootedState.UserDatasets {
		if d.Origin == "" {
			continue
		}
		log.Infof("promoting user dataset: %q\n", d.Name)
		if err := z.Promote(d.Name); err != nil {
			return xerrors.Errorf("couldn't promote %q user dataset: "+config.ErrorFormat, d.Name, err)
		}
	}
	for _, d := range bootedState.SystemDatasets {
		if d.Origin == "" {
			continue
		}
		log.Infof("promoting current state system dataset: %q\n", d.Name)

		if err := z.Promote(d.Name); err != nil {
			return xerrors.Errorf("couldn't set %q as current state: "+config.ErrorFormat, d.Name, err)
		}
	}

	// Rescan datasets, with current lastUsed, and main state.
	ds, err := z.Scan()
	if err != nil {
		return xerrors.Errorf("couldn't rescan after committing boot: "+config.ErrorFormat, err)
	}
	*machines = New(ds, machines.cmdline)

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

// splitSnapshotName return base and trailing names
func splitSnapshotName(name string) (string, string) {
	i := strings.LastIndex(name, "@")
	if i < 0 {
		return name, ""
	}
	return name[:i], name[i+1:]
}
