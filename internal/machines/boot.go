package machines

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/ubuntu/zsys/internal/config"
	"github.com/ubuntu/zsys/internal/log"
	"github.com/ubuntu/zsys/internal/zfs"
)

// ZfsPropertyCloneScanner interface can clone, set dataset property and scan
type ZfsPropertyCloneScanner interface {
	zfsScanner
	zfsPropertyCloneSetter
}

// ZfsPropertyPromoteScanner interface can promote, set dataset property and scan
type ZfsPropertyPromoteScanner interface {
	zfsScanner
	zfsPropertySetter
	zfsPromoter
}

// zfsPropertyCloneSetter can SetProperty and Clone
type zfsPropertyCloneSetter interface {
	Clone(name, suffix string, skipBootfs, recursive bool) (errClone error)
	zfsPropertySetter
}

// zfsScanner can only Scan
type zfsScanner interface {
	Scan() ([]zfs.Dataset, error)
}

// zfsPropertySetter can only SetProperty
type zfsPropertySetter interface {
	SetProperty(name, value, datasetName string, force bool) error
}

// zfsPromoter can only promote datasets
type zfsPromoter interface {
	Promote(name string) (errPromote error)
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
// Return if any dataset / machine changed has been done during boot and an error if any encountered.
// TODO: propagate error to user graphically
func (machines *Machines) EnsureBoot(ctx context.Context, z ZfsPropertyCloneScanner) (bool, error) {
	if !machines.current.isZsys() {
		log.Info(ctx, "Current machine isn't Zsys, nothing to do on boot")
		return false, nil
	}

	root, revertUserData := bootParametersFromCmdline(machines.cmdline)
	m, bootedState := machines.findFromRoot(root)
	log.Infof(ctx, "Ensure boot on %q", root)

	bootedOnSnapshot := hasBootedOnSnapshot(machines.cmdline)
	// We are creating new clones (bootfs and optionnally, userdata) if wasn't promoted already
	if bootedOnSnapshot && machines.current.ID != bootedState.ID {
		log.Infof(ctx, "Booting on snapshot: %q cloned to %q\n", root, bootedState.ID)

		// We skip it if we booted on a snapshot with userdatasets already created. This would mean that EnsureBoot
		// was called twice before Commit() during this boot. A new boot will create a new suffix id, so we won't block
		// the machine forever in case of a real issue.
		needCreateUserDatas := revertUserData && !(bootedOnSnapshot && len(bootedState.UserDatasets) > 0)
		if err := m.History[root].createClones(ctx, z, bootedState.ID, needCreateUserDatas); err != nil {
			return false, err
		}

		// Rescan here for getting accessing to new cloned datasets
		ds, err := z.Scan()
		if err != nil {
			return false, fmt.Errorf("couldn't rescan after modifying boot: "+config.ErrorFormat, err)
		}
		*machines = New(ctx, ds, machines.cmdline)
		m, bootedState = machines.findFromRoot(root) // We did rescan, refresh pointers
	}

	// We don't revert userdata, so we are using main state machine userdata to keep on the same track.
	// It's a no-op if the active state was the main one already.
	// In case of system revert (either from cloning or rebooting this older dataset without user data revert), the newly
	// active state won't be the main one, and so, we only take its main state userdata.
	if !revertUserData {
		bootedState.UserDatasets = m.UserDatasets
	}

	// Start switching every non desired system and user datasets to noauto
	noAutoDatasets := diffDatasets(machines.allSystemDatasets, bootedState.SystemDatasets)
	noAutoDatasets = append(noAutoDatasets, diffDatasets(machines.allUsersDatasets, bootedState.UserDatasets)...)
	needRescan, err := switchDatasetsCanMount(ctx, z, noAutoDatasets, "noauto")
	if err != nil {
		return false, err
	}

	// Switch current state system and user datasets to on
	autoDatasets := append(bootedState.SystemDatasets, bootedState.UserDatasets...)
	ok, err := switchDatasetsCanMount(ctx, z, autoDatasets, "on")
	if err != nil {
		return false, err
	}
	if ok {
		needRescan = true
	}

	// Rescan if we changed the dataset properties
	if needRescan {
		ds, err := z.Scan()
		if err != nil {
			return false, fmt.Errorf("couldn't rescan after modifying boot: "+config.ErrorFormat, err)
		}
		*machines = New(ctx, ds, machines.cmdline)
	}

	return needRescan, nil
}

// Commit current state to be the active one by promoting its datasets if needed, set last used,
// associate user datasets to it and rebuilding grub menu.
// After this operation, every New() call will get the current and correct system state.
// Return if any dataset / machine changed has been done during boot commit and an error if any encountered.
func (machines *Machines) Commit(ctx context.Context, z ZfsPropertyPromoteScanner) (bool, error) {
	if !machines.current.isZsys() {
		log.Info(ctx, "Current machine isn't Zsys, nothing to commit on boot")
		return false, nil
	}

	root, revertUserData := bootParametersFromCmdline(machines.cmdline)
	m, bootedState := machines.findFromRoot(root)
	log.Infof(ctx, "Committing boot for %q", root)

	// Get user datasets. As we didn't tag the user datasets and promote the system one, the machines doesn't correspond
	// to the reality.

	// We don't revert userdata, so we are using main state machine userdata to keep on the same track.
	// It's a no-op if the active state was the main one already.
	// In case of system revert (either from cloning or rebooting this older dataset without user data revert), the newly
	// active state won't be the main one, and so, we only take its main state userdata.
	if !revertUserData {
		bootedState.UserDatasets = m.UserDatasets
	}

	// Retag new userdatasets if needed
	if err := switchUsersDatasetsTags(ctx, z, bootedState.ID, machines.allUsersDatasets, bootedState.UserDatasets); err != nil {
		return false, err
	}

	// System and users datasets: set lastUsed
	currentTime := strconv.Itoa(int(time.Now().Unix()))
	// Last used is not a relevant change for signalling a change and justify bootloader rebuild: last-used is not
	// displayed for current system dataset.
	log.Infof(ctx, "set current time to %q", currentTime)
	for _, d := range append(bootedState.SystemDatasets, bootedState.UserDatasets...) {
		if err := z.SetProperty(zfs.LastUsedProp, currentTime, d.Name, false); err != nil {
			return false, fmt.Errorf("couldn't set last used time to %q: "+config.ErrorFormat, currentTime, err)
		}
	}

	var changed bool

	kernel := kernelFromCmdline(machines.cmdline)
	log.Infof(ctx, "Set latest booted kernel to %q\n", kernel)
	if bootedState.SystemDatasets[0].LastBootedKernel != kernel {
		// Signal last booted kernel changes.
		// This will help the bootloader, like grub, to rebuild and adjust the marker for last successfully booted kernel in advanced options.
		changed = true
		if err := z.SetProperty(zfs.LastBootedKernelProp, kernel, bootedState.SystemDatasets[0].Name, false); err != nil {
			return false, fmt.Errorf("couldn't set last booted kernel to %q "+config.ErrorFormat, kernel, err)
		}
	}

	// Promotion needed for system and user datasets
	log.Info(ctx, "Promoting user datasets")
	chg, err := promoteDatasets(ctx, z, bootedState.UserDatasets)
	if err != nil {
		return false, err
	}
	changed = changed || chg
	log.Info(ctx, "Promoting system datasets")
	chg, err = promoteDatasets(ctx, z, bootedState.SystemDatasets)
	if err != nil {
		return false, err
	}
	changed = changed || chg

	// Rescan datasets, with current lastUsed, and main state.
	ds, err := z.Scan()
	if err != nil {
		return false, fmt.Errorf("couldn't rescan after committing boot: "+config.ErrorFormat, err)
	}
	*machines = New(ctx, ds, machines.cmdline)

	return changed, nil
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

// getRootDatasets returns the name of any independent root datasets from a list
func getRootDatasets(ds []zfs.Dataset) (rds []string) {
	for _, n := range ds {
		var found bool
		for _, rootName := range rds {
			base, _ := splitSnapshotName(rootName)
			if strings.HasPrefix(n.Name, base+"/") {
				found = true
			}
		}
		if found {
			continue
		}
		rds = append(rds, n.Name)
	}

	return rds
}

func (snapshot State) createClones(ctx context.Context, z zfsPropertyCloneSetter, bootedStateID string, needCreateUserDatas bool) error {
	// get current generated suffix by initramfs
	j := strings.LastIndex(bootedStateID, "_")
	if j < 0 || strings.HasSuffix(bootedStateID, "_") {
		return fmt.Errorf("Mounted clone bootFS dataset created by initramfs doesn't have a valid _suffix (at least .*_<onechar>): %q", bootedStateID)
	}
	suffix := bootedStateID[j+1:]

	// Fetch every independent root datasets (like rpool, bpool, …) that needs to be cloned
	datasetsToClone := getRootDatasets(snapshot.SystemDatasets)

	// Skip bootfs datasets in the cloning phase. We assume any error would mean that EnsureBoot was called twice
	// before Commit() during this boot. A new boot will create a new suffix id, so we won't block the machine forever
	// in case of a real issue.
	// TODO: should test the clone return value (clone fails on system dataset already exists -> skip, other clone fails -> return error)
	for _, n := range datasetsToClone {
		log.Infof(ctx, "cloning %q", n)
		if err := z.Clone(n, suffix, true, true); err != nil {
			// TODO: transaction fix (as it's now set in error)
			log.Warningf(ctx, "Couldn't create new subdatasets from %q. Assuming it has already been created successfully: %v", n, err)
		}
	}

	// Handle userdata by creating new clones in case a revert was requested
	if !needCreateUserDatas {
		return nil
	}

	log.Info(ctx, "Reverting user data")
	// Find user datasets attached to the snapshot and clone them
	// Only root datasets are cloned
	userDataSuffix := generateID(6)
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
				return fmt.Errorf("couldn't create new user datasets from %q: %v", snapshot.ID, err)
			}
			// Associate this parent new user dataset to its parent system dataset
			base, _ := splitSnapshotName(d.Name)
			// Reformat the name with the new uuid and clone now the dataset.
			suffixIndex := strings.LastIndex(base, "_")
			userdatasetName := base[:suffixIndex] + "_" + userDataSuffix
			if err := z.SetProperty(zfs.BootfsDatasetsProp, bootedStateID, userdatasetName, false); err != nil {
				return fmt.Errorf("couldn't add %q to BootfsDatasets property of %q: "+config.ErrorFormat, bootedStateID, d.Name, err)
			}
		}
	}

	return nil
}

func switchDatasetsCanMount(ctx context.Context, z zfsPropertySetter, ds []zfs.Dataset, canMount string) (needRescan bool, err error) {
	// Only handle on and noauto datasets, not off
	initialCanMount := "on"
	if canMount == "on" {
		initialCanMount = "noauto"
	}

	for _, d := range ds {
		if d.CanMount != initialCanMount || d.IsSnapshot {
			continue
		}
		log.Infof(ctx, "Switch dataset %q to mount %q", d.Name, canMount)
		if err := z.SetProperty(zfs.CanmountProp, canMount, d.Name, false); err != nil {
			return false, fmt.Errorf("couldn't switch %q canmount property to %q: "+config.ErrorFormat, d.Name, canMount, err)
		}
		needRescan = true
	}

	return needRescan, nil
}

// switchUsersDatasetsTags tags and untags users datasets to associate with current main system dataset id.
func switchUsersDatasetsTags(ctx context.Context, z zfsPropertySetter, id string, allUsersDatasets, currentUsersDatasets []zfs.Dataset) error {
	// Untag non attached userdatasets
	for _, d := range diffDatasets(allUsersDatasets, currentUsersDatasets) {
		if d.IsSnapshot {
			continue
		}
		var newTag string
		// Multiple elements, strip current bootfs dataset name
		if d.BootfsDatasets != "" && d.BootfsDatasets != id {
			newTag = strings.Replace(d.BootfsDatasets, id+":", "", -1)
			newTag = strings.TrimSuffix(newTag, ":"+id)
		}
		if newTag == d.BootfsDatasets {
			continue
		}
		log.Infof(ctx, "Untagging user dataset: %q", d.Name)
		if err := z.SetProperty(zfs.BootfsDatasetsProp, newTag, d.Name, false); err != nil {
			return fmt.Errorf("couldn't remove %q to BootfsDatasets property of %q:"+config.ErrorFormat, id, d.Name, err)
		}
	}
	// Tag userdatasets to associate with this successful boot state, if wasn't tagged already
	// (case of no user data revert, associate with different previous main system)
	// TOREMOVE in 20.04 once compatible ubiquity is uploaded: && d.LastUsed != 0)
	// We want to transition to newer tag format com.ubuntu.zsys the first time we
	// set it.
	for _, d := range currentUsersDatasets {
		if (d.BootfsDatasets == id && d.LastUsed != 0) ||
			strings.Contains(d.BootfsDatasets, id+":") ||
			strings.HasSuffix(d.BootfsDatasets, ":"+id) {
			continue
		}
		log.Infof(ctx, "Tag current user dataset: %q", d.Name)
		newTag := d.BootfsDatasets + ":" + id
		// TOREMOVE in 20.04: this double check as well (due to && d.LastUsed != 0)
		if d.BootfsDatasets == id && d.LastUsed == 0 {
			newTag = d.BootfsDatasets
		}
		if err := z.SetProperty(zfs.BootfsDatasetsProp, newTag, d.Name, false); err != nil {
			return fmt.Errorf("couldn't add %q to BootfsDatasets property of %q: "+config.ErrorFormat, id, d.Name, err)
		}
	}

	return nil
}

func promoteDatasets(ctx context.Context, z zfsPromoter, ds []zfs.Dataset) (changed bool, err error) {
	for _, d := range ds {
		if d.Origin == "" {
			continue
		}
		changed = true
		log.Infof(ctx, "Promoting dataset: %q", d.Name)
		if err := z.Promote(d.Name); err != nil {
			return false, fmt.Errorf("couldn't promote dataset %q: "+config.ErrorFormat, d.Name, err)
		}
	}

	return changed, nil
}
