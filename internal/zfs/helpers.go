package zfs

import (
	"fmt"
	"log"
	"strconv"
	"strings"

	libzfs "github.com/bicomsystems/go-libzfs"
	"github.com/ubuntu/zsys/internal/config"
	"github.com/ubuntu/zsys/internal/i18n"
)

// local cache of properties, refreshed on Scan()
var datasetPropertiesCache map[string]*DatasetProp

// getDatasetsProp returns all properties for a given dataset and the source of them.
// for snapshots, we'll take the parent dataset for the mount properties.
func getDatasetProp(d libzfs.Dataset) (*DatasetProp, error) {
	sources := datasetSources{}
	name := d.Properties[libzfs.DatasetPropName].Value

	isSnapshot := d.IsSnapshot()
	var parentName string

	var mounted bool
	var mountpoint, canMount string

	if isSnapshot {
		parentName = name[:strings.LastIndex(name, "@")]
		p, ok := datasetPropertiesCache[parentName]
		if ok != true {
			return nil, fmt.Errorf(i18n.G("couldn't find %q in cache for getting properties of snapshot %q"), parentName, name)
		}
		mountpoint = p.Mountpoint
		sources.Mountpoint = p.sources.Mountpoint
		canMount = p.CanMount
	} else {
		mp, err := d.GetProperty(libzfs.DatasetPropMountpoint)
		if err != nil {
			return nil, fmt.Errorf(i18n.G("can't get mountpoint: ")+config.ErrorFormat, err)
		}
		if mp.Source != "none" {
			sources.Mountpoint = mp.Source
		}

		p, err := d.Pool()
		if err != nil {
			return nil, fmt.Errorf(i18n.G("can't get associated pool: ")+config.ErrorFormat, err)
		}
		poolRoot, err := p.GetProperty(libzfs.PoolPropAltroot)
		if err != nil {
			return nil, fmt.Errorf(i18n.G("can't get altroot for associated pool: ")+config.ErrorFormat, err)
		}
		mountpoint = strings.TrimPrefix(mp.Value, poolRoot.Value)
		if mountpoint == "" {
			mountpoint = "/"
		}

		cm, err := d.GetProperty(libzfs.DatasetPropCanmount)
		if err != nil {
			return nil, fmt.Errorf(i18n.G("can't get canmount property: ")+config.ErrorFormat, err)
		}
		canMount = cm.Value

		mountedp, err := d.GetProperty(libzfs.DatasetPropMounted)
		if err != nil {
			return nil, fmt.Errorf(i18n.G("can't get mounted: ")+config.ErrorFormat, err)
		}
		if mountedp.Value == "yes" {
			mounted = true
		}
	}

	// libzfs is accessing the property itself like this. There are issues when we do the check regularly with "no error"
	// returned, or dataset doesn't exists…
	origin := d.Properties[libzfs.DatasetPropOrigin].Value

	bfs, err := d.GetUserProperty(BootfsProp)
	if err != nil {
		return nil, fmt.Errorf(i18n.G("can't get bootfs property: ")+config.ErrorFormat, err)
	}
	var bootfs bool
	// Only consider local, explicitly set bootfs as meaningful
	if bfs.Source == "local" || (parentName != "" && parentName == bfs.Source) {
		if bfs.Value == "yes" {
			bootfs = true
		}
		sources.BootFS = bfs.Source
	}

	var lu libzfs.Property
	if !d.IsSnapshot() {
		lu, err = d.GetUserProperty(LastUsedProp)
		if err != nil {
			return nil, fmt.Errorf(i18n.G("can't get %q property: ")+config.ErrorFormat, LastUsedProp, err)
		}
	} else {
		lu, err = d.GetProperty(libzfs.DatasetPropCreation)
		if err != nil {
			return nil, fmt.Errorf(i18n.G("can't get creation property: ")+config.ErrorFormat, err)
		}
	}
	if lu.Source != "none" {
		sources.LastUsed = lu.Source
	}
	if lu.Value == "-" {
		lu.Value = "0"
	}
	lastused, err := strconv.Atoi(lu.Value)
	if err != nil {
		return nil, fmt.Errorf(i18n.G("%q property isn't an int: ")+config.ErrorFormat, LastUsedProp, err)
	}

	lbk, err := d.GetUserProperty(LastBootedKernelProp)
	if err != nil {
		return nil, fmt.Errorf(i18n.G("can't get %q property: ")+config.ErrorFormat, LastBootedKernelProp, err)
	}
	lastBootedKernel := lbk.Value
	if lastBootedKernel == "-" {
		lastBootedKernel = ""
	}
	if lbk.Source != "none" {
		sources.LastBootedKernel = lbk.Source
	}

	// TOREMOVE in 20.04 once compatible ubiquity is uploaded
	// Temporary compatibility harness for old org.zsys prefix
	var BootfsDatasets string
	for _, userdataTag := range []string{BootfsDatasetsProp, strings.Replace(BootfsDatasetsProp, "com.ubuntu", "org", -1)} {
		sDataset, err := d.GetUserProperty(userdataTag)
		if err != nil {
			return nil, fmt.Errorf(i18n.G("can't get %q property: ")+config.ErrorFormat, userdataTag, err)
		}
		BootfsDatasets = sDataset.Value
		if BootfsDatasets == "-" {
			BootfsDatasets = ""
		}
		if sDataset.Source != "none" {
			sources.BootfsDatasets = sDataset.Source
		}
		// Prefer new tag name
		if BootfsDatasets != "" {
			break
		}
	}

	dp := DatasetProp{
		Mountpoint:       mountpoint,
		CanMount:         canMount,
		Mounted:          mounted,
		BootFS:           bootfs,
		LastUsed:         lastused,
		LastBootedKernel: lastBootedKernel,
		BootfsDatasets:   BootfsDatasets,
		Origin:           origin,
		sources:          sources,
	}

	datasetPropertiesCache[name] = &dp

	return &dp, nil
}

// collectDatasets returns a Dataset tuple of all its properties and children
func collectDatasets(d libzfs.Dataset) []Dataset {
	var results []Dataset
	var collectErr error

	defer func() {
		if collectErr != nil {
			log.Printf(fmt.Sprintf(i18n.G("couldn't load dataset: %s\n"), config.ErrorFormat), collectErr)
		}
	}()

	// Skip non file system or snapshot datasets
	if d.Type == libzfs.DatasetTypeVolume || d.Type == libzfs.DatasetTypeBookmark {
		return nil
	}

	name := d.Properties[libzfs.DatasetPropName].Value

	props, err := getDatasetProp(d)
	if err != nil {
		collectErr = fmt.Errorf(i18n.G("can't get dataset properties for %q: ")+config.ErrorFormat, name, err)
		return nil
	}

	results = append(results,
		Dataset{
			Name:        name,
			IsSnapshot:  d.IsSnapshot(),
			DatasetProp: *props,
		})

	for _, dc := range d.Children {
		results = append(results, collectDatasets(dc)...)
	}

	return results
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
func checkNoClone(d *libzfs.Dataset) error {
	name := d.Properties[libzfs.DatasetPropName].Value

	clones, err := d.Clones()
	if err != nil {
		return fmt.Errorf(i18n.G("couldn't scan %q for clones"), name)
	}
	if len(clones) > 0 {
		return fmt.Errorf(i18n.G("%q has some clones when it shouldn't"), name)
	}

	for _, cd := range d.Children {
		if err := checkNoClone(&cd); err != nil {
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
