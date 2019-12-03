package zfs

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	libzfs "github.com/bicomsystems/go-libzfs"
	"github.com/ubuntu/zsys/internal/config"
	"github.com/ubuntu/zsys/internal/i18n"
	"github.com/ubuntu/zsys/internal/log"
)

// RefreshProperties refreshes all the properties for a given dataset and the source of them.
// for snapshots, we'll take the parent dataset for the mount properties.
func (d *Dataset) RefreshProperties(ctx context.Context, dZFS *libzfs.Dataset) error {
	sources := datasetSources{}
	isSnapshot := dZFS.IsSnapshot()
	name := dZFS.Properties[libzfs.DatasetPropName].Value

	var mounted bool
	var mountpoint, canMount string

	// On snapshots, take mount* properties from stored user property on dataset
	if isSnapshot {
		var srcMountpoint, srcCanMount string
		var err error

		mountpoint, srcMountpoint, err = getUserPropertyFromSys(ctx, SnapshotMountpointProp, dZFS)
		if err != nil {
			log.Debugf(ctx, i18n.G("%q isn't a zsys snapshot with a valid %q property: %v"), name, SnapshotMountpointProp, err)
		}
		sources.Mountpoint = srcMountpoint

		canMount, srcCanMount, err = getUserPropertyFromSys(ctx, SnapshotCanmountProp, dZFS)
		if err != nil {
			log.Debugf(ctx, i18n.G("%q isn't a zsys snapshot with a valid  %q property: %v"), name, SnapshotCanmountProp, err)
		}
		sources.CanMount = srcCanMount
	} else {
		mp := dZFS.Properties[libzfs.DatasetPropMountpoint]

		p, err := dZFS.Pool()
		if err != nil {
			return fmt.Errorf(i18n.G("can't get associated pool: ")+config.ErrorFormat, err)
		}
		poolRoot := p.Properties[libzfs.PoolPropAltroot].Value
		mountpoint = strings.TrimPrefix(mp.Value, poolRoot)
		if mountpoint == "" {
			mountpoint = "/"
		}
		srcMountpoint := "local"
		if mp.Source != "local" {
			srcMountpoint = "inherited"
		}
		sources.Mountpoint = srcMountpoint

		cm := dZFS.Properties[libzfs.DatasetPropCanmount]
		canMount = cm.Value
		srcCanMount := "local"
		if cm.Source != "local" {
			srcCanMount = "inherited"
		}
		sources.CanMount = srcCanMount

		mountedp := dZFS.Properties[libzfs.DatasetPropMounted]
		if mountedp.Value == "yes" {
			mounted = true
		}
	}

	origin := dZFS.Properties[libzfs.DatasetPropOrigin].Value

	bfs, srcBootFS, err := getUserPropertyFromSys(ctx, BootfsProp, dZFS)
	if err != nil {
		return err
	}
	var bootFS bool
	if bfs == "yes" {
		bootFS = true
	}
	sources.BootFS = srcBootFS

	var lu, srcLastUsed string
	if !isSnapshot {
		lu, srcLastUsed, err = getUserPropertyFromSys(ctx, LastUsedProp, dZFS)
		if err != nil {
			return err
		}
	} else {
		lu = dZFS.Properties[libzfs.DatasetPropCreation].Value
	}
	if lu == "" {
		lu = "0"
	}
	lastUsed, err := strconv.Atoi(lu)
	if err != nil {
		return fmt.Errorf(i18n.G("%q property isn't an int: ")+config.ErrorFormat, LastUsedProp, err)
	}
	sources.LastUsed = srcLastUsed

	lastBootedKernel, srcLastBootedKernel, err := getUserPropertyFromSys(ctx, LastBootedKernelProp, dZFS)
	if err != nil {
		return err
	}
	sources.LastBootedKernel = srcLastBootedKernel

	bootfsDatasets, srcBootfsDatasets, err := getUserPropertyFromSys(ctx, BootfsDatasetsProp, dZFS)
	if err != nil {
		return err
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
func getUserPropertyFromSys(ctx context.Context, prop string, dZFS *libzfs.Dataset) (value, source string, err error) {
	name := dZFS.Properties[libzfs.DatasetPropName].Value

	p, err := dZFS.GetUserProperty(prop)
	if err != nil {
		return "", "", fmt.Errorf(i18n.G("can't get %q property: ")+config.ErrorFormat, prop, err)
	}

	// User property doesn't exist for this dataset
	// On undefined user property sources, ZFS returns "-" but the API returns "none" check both for safety
	if p.Value == "-" && (p.Source == "-" || p.Source == "none") {
		return "", "", nil
	}
	// The user property isn't set explicitely on the snapshot (inherited from non snapshot parent): ignore it.
	if dZFS.IsSnapshot() && p.Source != "local" {
		return "", "", nil
	}

	if dZFS.IsSnapshot() {
		log.Debugf(ctx, "property %q on snapshot %q: %q", prop, name, value)
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

	if source != "local" {
		source = "inherited"
	}

	return value, source, nil
}

// newDatasetTree returns a Dataset and a populated tree of all its children
func newDatasetTree(ctx context.Context, dZFS *libzfs.Dataset, allDatasets *map[string]*Dataset) (*Dataset, error) {
	// Skip non file system or snapshot datasets
	if dZFS.Type == libzfs.DatasetTypeVolume || dZFS.Type == libzfs.DatasetTypeBookmark {
		return nil, nil
	}

	name := dZFS.Properties[libzfs.DatasetPropName].Value
	log.Debugf(ctx, i18n.G("New dataNew dataset found: %q"), name)
	node := Dataset{
		Name:       name,
		IsSnapshot: dZFS.IsSnapshot(),
		dZFS:       dZFS,
	}
	if err := node.RefreshProperties(ctx, dZFS); err != nil {
		return nil, fmt.Errorf("couldn't refresh properties of %q: %v", node.Name, err)
	}

	var children []*Dataset
	for _, dc := range dZFS.Children {
		c, err := newDatasetTree(ctx, &dc, allDatasets)
		if err != nil {
			return nil, fmt.Errorf("couldn't scan dataset: %v", err)
		}
		if c == nil {
			continue
		}
		children = append(children, c)
	}
	node.children = children

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
// - All children datasets with a snapshot with the same name exists -> OK, nothing in particular to deal with
// - One dataset doesn't have a snapshot with the same name:
//   * If no of its children of this dataset has a snapshot with the same name:
//     * the dataset (and its children) has been created after the snapshot was taken -> OK
//     * the dataset snapshot (and all its children snapshots) have been removed entirely: no way to detect the difference from above -> consider OK
//   * If one of its children has a snapshot with the same name: clearly a case where something went wrong during snapshot -> error OUT
// Said differently:
// if a dataset has a snapshot with a given, all its parents should have a snapshot with the same name (up to base snapshotName)
func checkSnapshotHierarchyIntegrity(d libzfs.Dataset, snapshotName string, snapshotExpected bool) error {
	found, _ := d.FindSnapshotName("@" + snapshotName)

	// No more snapshot was expected for children (parent dataset didn't have a snapshot, so all children shouldn't have them)
	if found && !snapshotExpected {
		name := d.Properties[libzfs.DatasetPropName].Value
		return fmt.Errorf(i18n.G("parent of %q doesn't have a snapshot named %q. Every of its children shouldn't have a snapshot. However %q exists"),
			name, snapshotName, name+"@"+snapshotName)
	}

	for _, cd := range d.Children {
		if err := checkSnapshotHierarchyIntegrity(cd, snapshotName, found); err != nil {
			return err
		}
	}
	return nil
}

// checkNoClone checks that the hierarchy has no clone.
func (d *Dataset) checkNoClone() error {
	clones, err := d.dZFS.Clones()
	if err != nil {
		return fmt.Errorf(i18n.G("couldn't scan %q for clones"), d.Name)
	}
	if len(clones) > 0 {
		return fmt.Errorf(i18n.G("%q has some clones when it shouldn't"), d.Name)
	}

	for _, dc := range d.children {
		if err := dc.checkNoClone(); err != nil {
			return err
		}
	}
	return nil
}

// getProperty abstracts getting from a zfs or user property. It returns the property object.
func getProperty(d libzfs.Dataset, name string) (libzfs.Property, error) {
	// TODO: or use getDatasetProp() and cache on Scan() to always have "none" checked.
	var prop libzfs.Property
	if !strings.Contains(name, ":") {
		propName, err := stringToProp(name)
		if err != nil {
			return prop, err
		}
		return d.GetProperty(propName)
	}

	return d.GetUserProperty(name)
}

// setProperty abstracts setting  value to a zfs or user property from a zfs or user property.
func setProperty(d libzfs.Dataset, name, value string) error {
	if !strings.Contains(name, ":") {
		propName, err := stringToProp(name)
		if err != nil {
			return err
		}
		return d.SetProperty(propName, value)
	}
	return d.SetUserProperty(name, value)
}

// stringToProp converts a string to a validated zfs property (user properties aren't supported here).
func stringToProp(name string) (libzfs.Prop, error) {
	var prop libzfs.Prop
	switch name {
	case CanmountProp:
		prop = libzfs.DatasetPropCanmount
	case MountPointProp:
		prop = libzfs.DatasetPropMountpoint
	default:
		return prop, fmt.Errorf(i18n.G("unsupported property %q"), name)
	}
	return prop, nil
}

type datasetFuncRecursive func(d libzfs.Dataset) error

// recurseFileSystemDatasets takes all children of d, and if it's not a snpashot, run f() over there while
// returning an error if raised on any children.
func recurseFileSystemDatasets(d libzfs.Dataset, f datasetFuncRecursive) error {
	for _, cd := range d.Children {
		if cd.IsSnapshot() {
			continue
		}

		if err := f(cd); err != nil {
			return err
		}
	}
	return nil
}
