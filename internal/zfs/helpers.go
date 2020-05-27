package zfs

import (
	"context"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/ubuntu/zsys/internal/config"
	"github.com/ubuntu/zsys/internal/i18n"
	"github.com/ubuntu/zsys/internal/log"
	"github.com/ubuntu/zsys/internal/zfs/libzfs"
)

// RefreshProperties refreshes all the properties for a given dataset and the source of them.
func (d *Dataset) refreshProperties(ctx context.Context) error {
	sources := datasetSources{}
	dZFSprops := *d.dZFS.Properties()
	name := dZFSprops[libzfs.DatasetPropName].Value

	var mounted bool
	var mountpoint, canMount string
	var sourceMountPoint, sourceCanMount string
	// On snapshots, take mount* properties from stored user property on dataset
	if d.IsSnapshot {
		var err error

		mountpoint, sourceMountPoint, err = getUserPropertyFromSys(ctx, libzfs.SnapshotMountpointProp, d.dZFS)
		if err != nil {
			log.Debugf(ctx, i18n.G("%q isn't a zsys snapshot with a valid %q property: %v"), name, libzfs.SnapshotMountpointProp, err)
		}

		canMount, sourceCanMount, err = getUserPropertyFromSys(ctx, libzfs.SnapshotCanmountProp, d.dZFS)
		if err != nil {
			log.Debugf(ctx, i18n.G("%q isn't a zsys snapshot with a valid %q property: %v"), name, libzfs.SnapshotCanmountProp, err)
		}
	} else {
		mp := dZFSprops[libzfs.DatasetPropMountpoint]

		p, err := d.dZFS.Pool()
		if err != nil {
			return fmt.Errorf(i18n.G("can't get associated pool: ")+config.ErrorFormat, err)
		}
		poolRoot := p.Properties[libzfs.PoolPropAltroot].Value
		mountpoint = strings.TrimPrefix(mp.Value, poolRoot)
		if mountpoint == "" {
			mountpoint = "/"
		}
		sourceMountPoint = mp.Source

		cm := dZFSprops[libzfs.DatasetPropCanmount]
		canMount = cm.Value
		sourceCanMount = cm.Source

		mountedp := dZFSprops[libzfs.DatasetPropMounted]
		if mountedp.Value == "yes" {
			mounted = true
		}
	}
	switch sourceMountPoint {
	case "local":
		sources.Mountpoint = "local"
	case "default":
		sources.Mountpoint = "inherited"
	default:
		if mountpoint != "" {
			sources.Mountpoint = "inherited"
		}
	}

	switch sourceCanMount {
	case "local":
		sources.CanMount = "local"
	case "default":
		sources.CanMount = ""
	default:
		// this shouldn't happen on non snapshot
		if !d.IsSnapshot {
			log.Warningf(ctx, i18n.G("CanMount property for %q has an unexpected source: %q"), name, sourceCanMount)
		}
		sources.CanMount = ""
	}

	origin := dZFSprops[libzfs.DatasetPropOrigin].Value

	bfs, srcBootFS, err := getUserPropertyFromSys(ctx, libzfs.BootfsProp, d.dZFS)
	if err != nil {
		log.Warningf(ctx, i18n.G("can't read bootfs property, ignoring: ")+config.ErrorFormat, err)
	}
	var bootFS bool
	if bfs == "yes" {
		bootFS = true
	}
	sources.BootFS = srcBootFS

	var lu, srcLastUsed string
	if !d.IsSnapshot {
		lu, srcLastUsed, err = getUserPropertyFromSys(ctx, libzfs.LastUsedProp, d.dZFS)
		if err != nil {
			log.Warningf(ctx, i18n.G("can't read source of LastUsed property, ignoring:")+config.ErrorFormat, err)
		}
	} else {
		lu = dZFSprops[libzfs.DatasetPropCreation].Value
	}
	if lu == "" {
		lu = "0"
	}
	lastUsed, err := strconv.Atoi(lu)
	if err != nil {
		log.Warningf(ctx, i18n.G("%q property isn't an int: ")+config.ErrorFormat, libzfs.LastUsedProp, err)
		srcLastUsed = ""
	}
	sources.LastUsed = srcLastUsed

	lastBootedKernel, srcLastBootedKernel, err := getUserPropertyFromSys(ctx, libzfs.LastBootedKernelProp, d.dZFS)
	if err != nil {
		log.Warningf(ctx, i18n.G("can't read lastBootedKernel property, ignoring: ")+config.ErrorFormat, err)
	}
	sources.LastBootedKernel = srcLastBootedKernel

	var bootfsDatasets, srcBootfsDatasets string
	if !d.IsSnapshot {
		if bootfsDatasets, srcBootfsDatasets, err = getUserPropertyFromSys(ctx, libzfs.BootfsDatasetsProp, d.dZFS); err != nil {
			log.Warningf(ctx, i18n.G("can't read bootfsdataset property, ignoring: ")+config.ErrorFormat, err)
		}
		// TOREMOVE: transition from 19.10 installation without zsys to 20.04 after installing zsys
		if strings.Contains(d.Name, "/"+UserdataPrefix+"/") && bootfsDatasets == "" {
			oldBootfsDatasets, oldSrcBootfsDatasets, _ := getUserPropertyFromSys(ctx, "org.zsys:bootfs-datasets", d.dZFS)
			if oldBootfsDatasets != "" {
				if err := d.dZFS.SetUserProperty(libzfs.BootfsDatasetsProp, oldBootfsDatasets); err != nil {
					log.Warningf(ctx, i18n.G("can't transition bootfsdataset property, ignoring: ")+config.ErrorFormat, err)
				}
			}
			if oldSrcBootfsDatasets != "" {
				srcBootfsDatasets = oldBootfsDatasets
			}
		}
	}
	sources.BootfsDatasets = srcBootfsDatasets

	d.DatasetProp = DatasetProp{
		Mountpoint:       mountpoint,
		CanMount:         canMount,
		Mounted:          mounted,
		BootFS:           bootFS,
		LastUsed:         lastUsed,
		LastBootedKernel: lastBootedKernel,
		BootfsDatasets:   bootfsDatasets,
		Origin:           origin,
		sources:          sources,
	}
	return nil
}

// getUserPropertyFromSys returns the value of a user property and its source from the underlying
// ZFS system dataset state.
// It also sanitize the sources to only return "local" or "inherited".
func getUserPropertyFromSys(ctx context.Context, prop string, dZFS libzfs.DZFSInterface) (value, source string, err error) {
	name := (*dZFS.Properties())[libzfs.DatasetPropName].Value

	p, err := dZFS.GetUserProperty(prop)
	if err != nil {
		return "", "", fmt.Errorf(i18n.G("can't get %q property: ")+config.ErrorFormat, prop, err)
	}

	// User property doesn't exist for this dataset
	// On undefined user property sources, ZFS returns "-" but the API returns "none" check both for safety
	if p.Value == "-" && (p.Source == "-" || p.Source == "none") {
		return "", "", nil
	}
	// The user property isn't set explicitly on the snapshot (inherited from non snapshot parent): ignore it.
	if dZFS.IsSnapshot() && p.Source != "local" {
		return "", "", nil
	}

	if dZFS.IsSnapshot() {
		log.Debugf(ctx, "property %q on snapshot %q: %q", prop, name, p.Value)
		idx := strings.LastIndex(p.Value, ":")
		if idx < 0 {
			log.Warningf(ctx, i18n.G("%q isn't a 'value:source' format type for %q"), prop, name)
			return
		}
		value = p.Value[:idx]
		source = p.Value[idx+1:]
	} else {
		value = p.Value
		source = p.Source
		log.Debugf(ctx, "property %q on dataset %q: value: %q source: %q", prop, name, value, source)
	}

	if source != "local" && source != "default" {
		source = "inherited"
	}

	return value, source, nil
}

// newDatasetTree returns a Dataset and a populated tree of all its children
// It returns a nil Dataset with a nil error for unsupported dataset type (DatasetTypeVolume or DatasetTypeBookmark)
func newDatasetTree(ctx context.Context, dZFS libzfs.DZFSInterface, allDatasets *map[string]*Dataset) (*Dataset, error) {
	// Skip non file system or snapshot datasets
	if dZFS.Type() == libzfs.DatasetTypeVolume || dZFS.Type() == libzfs.DatasetTypeBookmark {
		return nil, nil
	}

	name := (*dZFS.Properties())[libzfs.DatasetPropName].Value
	log.Debugf(ctx, i18n.G("New dataset found: %q"), name)
	node := Dataset{
		Name:       name,
		IsSnapshot: dZFS.IsSnapshot(),
		dZFS:       dZFS,
	}
	if err := node.refreshProperties(ctx); err != nil {
		log.Warningf(ctx, i18n.G("couldn't refresh properties of %q: %v"), node.Name, err)
	}

	var children []*Dataset
	for i := range dZFS.Children() {
		// WARNING: We are using a single Dataset reference to avoid desync between golibzfs.Dataset state and our
		// internal dZFS elements. golibzfs.Dataset doesn't handle Children properly and don't have a way to reach
		// out to other datasets, like parents, without a full rescan.
		// We are using our own dZFS as the primary reference object. As we always copy the golibzfs.Dataset object,
		// we are using the same Dataset.list internal C reference pointer, having thus only one dataset in C cache.
		// This is why we don't .Close() libzfs Datasets after the copy, as it references the same underlying pointed
		// element.
		// For security, Children are removed from libzfs in caller.
		c, err := newDatasetTree(ctx, dZFS.Children()[i], allDatasets)
		if err != nil {
			return nil, fmt.Errorf("couldn't scan dataset: %v", err)
		}
		// Not a filesystem or snapshot dataset: skipping
		if c == nil {
			continue
		}
		children = append(children, c)
	}
	node.children = children
	*node.dZFS.DZFSChildren() = nil

	// Populate direct access map
	(*allDatasets)[node.Name] = &node

	return &node, nil
}

// splitSnapshotName return base and trailing names
func splitSnapshotName(name string) (string, string) {
	i := strings.LastIndex(name, "@")
	if i < 0 {
		return name, ""
	}
	return name[:i], name[i+1:]
}

// checkSnapshotHierarchyIntegrity checks that the hierarchy follow the correct rules.
// There are multiple cases:
// * All children datasets with a snapshot with the same name exists -> OK, nothing in particular to deal with
// * One dataset doesn't have a snapshot with the same name:
//   - If none of its children of this dataset has a snapshot with the same name:
//     . the dataset (and its children) has been created after the snapshot was taken -> OK
//     . the dataset snapshot (and all its children snapshots) have been removed entirely: no way to detect the difference from above -> consider OK
//   - If one of its children has a snapshot with the same name: clearly a case where something went wrong during snapshot -> error OUT
// Said differently:
// if a dataset has a snapshot with a given name, all its parents should have a snapshot with the same name (up to base snapshotName)
func (d Dataset) checkSnapshotHierarchyIntegrity(snapshotName string, snapshotOnParent bool) error {
	var found bool
	for _, cd := range d.children {
		if cd.Name == d.Name+"@"+snapshotName {
			found = true
			break
		}
	}

	// No more snapshot was expected for children (parent dataset didn't have a snapshot, so all children shouldn't have them)
	if found && !snapshotOnParent {
		return fmt.Errorf(i18n.G("parent of %q doesn't have a snapshot named %q. Every of its children shouldn't have a snapshot. However %q exists"),
			d.Name, snapshotName, d.Name+"@"+snapshotName)
	}

	for _, cd := range d.children {
		if cd.IsSnapshot {
			continue
		}
		if err := cd.checkSnapshotHierarchyIntegrity(snapshotName, found); err != nil {
			return err
		}
	}
	return nil
}

// HasSnapshotInHierarchy checks that the hierarchy of current dataset has a snapshot
func (d Dataset) HasSnapshotInHierarchy() bool {
	if d.IsSnapshot {
		return false
	}
	for _, cd := range d.children {
		if cd.IsSnapshot || cd.HasSnapshotInHierarchy() {
			return true
		}
	}
	return false
}

// IsUserDataset returns if this filesystem dataset is or has been a userdataset, even if unlinked to any filesystem dataset
// Note that it doesn’t take into account if the dataset is a clone of a userdataset.
// Snapshots will always return an error, check the filesystem dataset first.
func (d Dataset) IsUserDataset() (bool, error) {
	if !strings.Contains(d.Name, "/"+UserdataPrefix+"/") {
		return false, nil
	}

	if d.IsSnapshot {
		return false, fmt.Errorf(i18n.G("IsUserDataset called on snapshot %q."), d.Name)
	}
	return d.sources.BootfsDatasets != "", nil
}

// checkNoClone checks that the hierarchy has no clone.
func (d *Dataset) checkNoClone() error {
	// TODO: this reopens the pool entirely, so can be a little bit slow. Could be reimplemented ourselves.
	clones, err := d.dZFS.Clones()
	if err != nil {
		return fmt.Errorf(i18n.G("couldn't scan %q for clones"), d.Name)
	}
	if len(clones) > 0 {
		return fmt.Errorf(i18n.G("%q has some clones (%v) when it shouldn't"), d.Name, clones)
	}

	for _, dc := range d.children {
		if err := dc.checkNoClone(); err != nil {
			return err
		}
	}
	return nil
}

// getPropertyFromName abstracts getting from a zfs or user property from a name.
// It returns the value and our simplified source (local or inherited).
func (d *Dataset) getPropertyFromName(name string) (value, source string) {
	_, _, v, s := d.stringToProp(name)
	return *v, *s
}

// setProperty abstracts setting value to a zfs native or user property.
// It refreshes the local object.
// Note: source isn't taken into account from inheriting on the ZFS dataset
func (d *Dataset) setProperty(name, value, source string) (err error) {
	np, up, destV, destS := d.stringToProp(name)

	// TODO: go-libzfs doesn't support "inherited" (C.zfs_prop_inherit).
	// If source isn't local, we should rather revert to "inherit" which isn't possible atm.
	// if source == "inherited" …

	// libzfs.Prop is a literal (int) and cannot be created empty and compared directly
	var empty libzfs.Prop
	if np != empty {
		err = d.dZFS.SetProperty(np, value)
	} else {
		v := value
		// we set value:source for values on snapshots to retain original state
		if d.IsSnapshot {
			if source != "" {
				v = fmt.Sprintf("%s:%s", value, source)
			}
		}

		// Ensure libzfs.LastUsedProp is valid before setting it
		if name == libzfs.LastUsedProp {
			if value == "" {
				value = "0"
			}
			if _, err := strconv.Atoi(value); err != nil {
				return fmt.Errorf(i18n.G("%q property isn't an int: ")+config.ErrorFormat, libzfs.LastUsedProp, err)
			}
		}

		err = d.dZFS.SetUserProperty(up, v)
		if err != nil {
			return err
		}

		// TODO: remove once we mock rather time.Now() for mock tests
		// Reload last used property from backend (as we can have set it to magic time)
		if name == libzfs.LastUsedProp {
			if prop, errRead := d.dZFS.GetUserProperty(up); errRead == nil {
				value = prop.Value
			}
			if value == "" {
				value = "0"
			}
			if _, err := strconv.Atoi(value); err != nil {
				return fmt.Errorf(i18n.G("%q property isn't an int: ")+config.ErrorFormat, libzfs.LastUsedProp, err)
			}
		}
	}

	if err != nil {
		return err
	}

	// Don’t propagate BootfsDataset on snapshot which is ignored on refresh
	if d.IsSnapshot && name == libzfs.BootfsDatasetsProp {
		return nil
	}

	// In case we change the mountpoint, we need to translate the whole hierarchy for children.
	// Store initial mountpoint path.
	var oldMountPoint string
	// Refresh local values on dataset object
	switch name {
	case libzfs.BootfsProp:
		var bootFS bool
		if value == "yes" {
			bootFS = true
		}
		d.BootFS = bootFS
	case libzfs.LastUsedProp:
		lastUsed, err := strconv.Atoi(value)
		if err != nil {
			panic(fmt.Sprintf("%q property isn't an int: %v, while it has already been checked for main dataset and passed", libzfs.LastUsedProp, err))
		}
		d.LastUsed = lastUsed
	case libzfs.MountPointProp:
		oldMountPoint = *destV
		fallthrough
	default:
		*destV = value
	}
	*destS = source

	// Refresh all children that inherits from this property.
	children := make(chan *Dataset)
	var getInheritedChildren func(d *Dataset)
	getInheritedChildren = func(d *Dataset) {
		for _, c := range d.children {
			np, _, _, destS := c.stringToProp(name)
			// We ignore snapshots from inheritance: we only take user properties (even for canmount or mountpoint)
			// that we have frozen when taking our own snapshots. The other properties will ofc be changed, but
			// we don't care about them in our local cache.
			if c.IsSnapshot {
				continue
			}
			// Only take inherited properties OR
			// default user property (unset user property)
			if *destS != "inherited" && !(*destS == "" && np == empty) {
				continue
			}
			children <- c
			getInheritedChildren(c)
		}
	}
	go func() {
		getInheritedChildren(d)
		close(children)
	}()

	for c := range children {
		np, _, destV, destS := c.stringToProp(name)

		// Native dataset: we need to refresh dZFS Properties (user properties aren't cached)
		if np != empty {
			props := c.dZFS.Properties()
			(*props)[np] = libzfs.Property{
				Value:  filepath.Join(value, strings.TrimPrefix(*destV, oldMountPoint)),
				Source: (*props)[np].Source,
			}
		}

		// Refresh dataset object
		switch name {
		case libzfs.BootfsProp:
			var bootFS bool
			if value == "yes" {
				bootFS = true
			}
			c.BootFS = bootFS
		case libzfs.LastUsedProp:
			lastUsed, err := strconv.Atoi(value)
			if err != nil {
				// Shouldn't happen: it's been already checked above from main dataset
				panic(fmt.Sprintf("%q property isn't an int: %v, while it has already been checked for main dataset and passed", libzfs.LastUsedProp, err))
			}
			c.LastUsed = lastUsed
		case libzfs.MountPointProp:
			*destV = filepath.Join(value, strings.TrimPrefix(*destV, oldMountPoint))
		default:
			*destV = value
		}
		*destS = "inherited"
	}

	return err
}

// stringToProp converts a string our object properties.
// proZfs is empty for user properties. We get pointer on both Dataset object prop and our source
func (d *Dataset) stringToProp(name string) (nativeProp libzfs.Prop, userProp string, value, simplifiedSource *string) {
	userProp = name
	switch name {
	case libzfs.CanmountProp:
		if !d.IsSnapshot {
			nativeProp = libzfs.DatasetPropCanmount
		} else {
			// this should have been called with libzfs.SnapshotCanmountProp, but map it for the user
			userProp = libzfs.SnapshotCanmountProp
		}
		fallthrough
	case libzfs.SnapshotCanmountProp:
		value = &d.CanMount
		simplifiedSource = &d.sources.CanMount
	case libzfs.MountPointProp:
		if !d.IsSnapshot {
			nativeProp = libzfs.DatasetPropMountpoint
		} else {
			// this should have been called with libzfs.SnapshotMountpointProp, but map it for the user
			userProp = libzfs.SnapshotMountpointProp
		}
		value = &d.Mountpoint
		simplifiedSource = &d.sources.Mountpoint
	case libzfs.SnapshotMountpointProp:
		value = &d.Mountpoint
		simplifiedSource = &d.sources.Mountpoint
	// Bootfs and LastUsed are non string. Return a local string
	case libzfs.BootfsProp:
		bootfs := "yes"
		if !d.BootFS {
			bootfs = "no"
		}
		value = &bootfs
		simplifiedSource = &d.sources.BootFS
	case libzfs.LastUsedProp:
		lu := strconv.Itoa(d.LastUsed)
		value = &lu
		simplifiedSource = &d.sources.LastUsed
	case libzfs.BootfsDatasetsProp:
		value = &d.BootfsDatasets
		simplifiedSource = &d.sources.BootfsDatasets
	case libzfs.LastBootedKernelProp:
		value = &d.LastBootedKernel
		simplifiedSource = &d.sources.LastBootedKernel
	default:
		panic(fmt.Sprintf("unsupported property %q", name))
	}
	return nativeProp, userProp, value, simplifiedSource
}

// inverseOrigin inverses on the Dataset object themselves the dependence hierarchy.
// It refreshes the global hierarchy as well, as snapshots are migrating.
func (t *nestedTransaction) inverseOrigin(oldOrigDataset, newOrigDataset *Dataset) error {
	baseSnapshot, err := t.Zfs.findDatasetByName(newOrigDataset.Origin)
	if err != nil {
		return fmt.Errorf(i18n.G("cannot find base snapshot %q: %v"), newOrigDataset.Origin, err)
	}

	// Collect all snapshots to migrate to newOrigDataset
	var snapshotsToMigrate []*Dataset
	for i := range oldOrigDataset.children {
		c := oldOrigDataset.children[i]
		if !c.IsSnapshot {
			continue
		}
		if c.LastUsed > baseSnapshot.LastUsed {
			continue
		}
		snapshotsToMigrate = append(snapshotsToMigrate, c)
	}

	for i := range snapshotsToMigrate {
		s := snapshotsToMigrate[i]

		oldName := s.Name
		_, n := splitSnapshotName(oldName)

		s.Name = newOrigDataset.Name + "@" + n
		// Add new child to promoted dataset
		newOrigDataset.children = append(newOrigDataset.children, s)

		// Find and remove child from demoted dataset (using new name)
		if err := oldOrigDataset.removeChild(s.Name); err != nil {
			return fmt.Errorf(i18n.G("cannot remove snapshot %q on old dataset %q: %v"), oldName, oldOrigDataset.Name, err)
		}

		// this snapshot dZFS handler can't be renamed or refreshed to its new resource snapshot name. Close it.
		s.dZFS.Close()

		dZFSlibzfs, err := t.Zfs.libzfs.DatasetOpen(s.Name)
		if err != nil {
			return fmt.Errorf("cannot open new snapshot %q: %v", s.Name, err)
		}
		s.dZFS = dZFSlibzfs

		// Refresh our global map
		t.Zfs.allDatasets[s.Name] = s
		delete(t.Zfs.allDatasets, oldName)

		// Move all datasets which origin depends on that snapshot to the new one
		for dName, d := range t.Zfs.allDatasets {
			if d.Origin != oldName || strings.HasPrefix(s.Name, dName+"@") {
				continue
			}
			d.Origin = s.Name

			// Ensure we reloaded the properties of this dataset as the underlying ZFS has changed as well.
			if err := d.dZFS.ReloadProperties(); err != nil {
				return fmt.Errorf("cannot Refresh properties for %q: %v", dName, err)
			}
		}
	}

	orig := oldOrigDataset.Origin
	oldOrigDataset.Origin = baseSnapshot.Name
	newOrigDataset.Origin = orig

	return nil
}

// removeChild on our Dataset object
func (d *Dataset) removeChild(name string) error {
	i := -1
	for idx, dc := range d.children {
		if dc.Name == name {
			i = idx
			break
		}
	}

	if i < 0 {
		return fmt.Errorf(i18n.G("cannot find %q as child of parent %q"), name, d.Name)
	}

	if i < len(d.children)-1 {
		copy(d.children[i:], d.children[i+1:])
	}
	// avoid memory leak by having a pointer reachable by underlying array
	d.children[len(d.children)-1] = nil
	d.children = d.children[:len(d.children)-1]
	return nil
}
