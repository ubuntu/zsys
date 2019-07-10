package zfs

import (
	"log"
	"strconv"
	"strings"

	libzfs "github.com/bicomsystems/go-libzfs"
	"github.com/ubuntu/zsys/internal/config"
	"golang.org/x/xerrors"
)

// getDatasetsProp returns all properties for a given dataset and the source of them.
// for snapshots, we'll take the parent dataset for the mount properties.
func getDatasetProp(d libzfs.Dataset) (*DatasetProp, error) {
	sources := datasetSources{}
	name := d.Properties[libzfs.DatasetPropName].Value

	var mountPropertiesDataset = &d
	if d.IsSnapshot() {
		parentName := name[:strings.LastIndex(name, "@")]
		pd, err := libzfs.DatasetOpen(parentName)
		if err != nil {
			return nil, xerrors.Errorf("can't get parent dataset: "+config.ErrorFormat, err)
		}
		defer pd.Close()
		mountPropertiesDataset = &pd
	}

	var mountpoint, canMount string
	mp, err := mountPropertiesDataset.GetProperty(libzfs.DatasetPropMountpoint)
	if err != nil {
		return nil, xerrors.Errorf("can't get mountpoint: "+config.ErrorFormat, err)
	}
	sources.Mountpoint = mp.Source

	p, err := mountPropertiesDataset.Pool()
	if err != nil {
		return nil, xerrors.Errorf("can't get associated pool: "+config.ErrorFormat, err)
	}
	poolRoot, err := p.GetProperty(libzfs.PoolPropAltroot)
	if err != nil {
		return nil, xerrors.Errorf("can't get altroot for associated pool: "+config.ErrorFormat, err)
	}
	mountpoint = strings.TrimPrefix(mp.Value, poolRoot.Value)
	if mountpoint == "" {
		mountpoint = "/"
	}

	cm, err := mountPropertiesDataset.GetProperty(libzfs.DatasetPropCanmount)
	if err != nil {
		return nil, xerrors.Errorf("can't get canmount property: "+config.ErrorFormat, err)
	}
	canMount = cm.Value

	// libzfs is accessing the property itself like this. There are issues when we do the check regularly with "no error"
	// returned, or dataset doesn't exists…
	origin := d.Properties[libzfs.DatasetPropOrigin].Value

	bfs, err := d.GetUserProperty(BootfsProp)
	if err != nil {
		return nil, xerrors.Errorf("can't get bootfs property: "+config.ErrorFormat, err)
	}
	bootfs := bfs.Value
	if bootfs == "-" {
		bootfs = ""
	}
	sources.BootFS = bfs.Source

	var lu libzfs.Property
	if !d.IsSnapshot() {
		lu, err = d.GetUserProperty(LastUsedProp)
		if err != nil {
			return nil, xerrors.Errorf("can't get %q property: "+config.ErrorFormat, LastUsedProp, err)
		}
	} else {
		lu, err = d.GetProperty(libzfs.DatasetPropCreation)
		if err != nil {
			return nil, xerrors.Errorf("can't get creation property: "+config.ErrorFormat, err)
		}
	}
	sources.LastUsed = lu.Source
	if lu.Value == "-" {
		lu.Value = "0"
	}
	lastused, err := strconv.Atoi(lu.Value)
	if err != nil {
		return nil, xerrors.Errorf("%q property isn't an int: "+config.ErrorFormat, LastUsedProp, err)
	}

	sDataset, err := d.GetUserProperty(BootfsDatasetsProp)
	if err != nil {
		return nil, xerrors.Errorf("can't get %q property: "+config.ErrorFormat, BootfsDatasetsProp, err)
	}
	BootfsDatasets := sDataset.Value
	if BootfsDatasets == "-" {
		BootfsDatasets = ""
	}
	sources.BootfsDatasets = sDataset.Source

	return &DatasetProp{
		Mountpoint:     mountpoint,
		CanMount:       canMount,
		BootFS:         bootfs,
		LastUsed:       lastused,
		BootfsDatasets: BootfsDatasets,
		Origin:         origin,
		sources:        sources,
	}, nil
}

// collectDatasets returns a Dataset tuple of all its properties and children
func collectDatasets(d libzfs.Dataset) []Dataset {
	var results []Dataset
	var collectErr error

	defer func() {
		if collectErr != nil {
			log.Printf("couldn't load dataset: "+config.ErrorFormat+"\n", collectErr)
		}
	}()

	// Skip non file system or snapshot datasets
	if d.Type == libzfs.DatasetTypeVolume || d.Type == libzfs.DatasetTypeBookmark {
		return nil
	}

	name := d.Properties[libzfs.DatasetPropName].Value

	props, err := getDatasetProp(d)
	if err != nil {
		collectErr = xerrors.Errorf("can't get dataset properties for %q: "+config.ErrorFormat, name, err)
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

// separateSnaphotName return base and trailing names
func separateSnaphotName(name string) (string, string) {
	i := strings.LastIndex(name, "@")
	if i < 0 {
		return name, ""
	}
	return name[:i], name[i+1:]
}

// checkSnapshotHierarchyIntegrity checks that the hierachy follow the correct rules.
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
		return xerrors.Errorf("parent of %q doesn't have a snapshot named %q. Every of its children shouldn't have a snapshot. However %q exists.",
			name, snapshotName, name+"@"+snapshotName)
	}

	for _, cd := range d.Children {
		if err := checkSnapshotHierarchyIntegrity(cd, snapshotName, found); err != nil {
			return err
		}
	}
	return nil
}

// checkNoClone checks that the hierachy has no clone.
func checkNoClone(d *libzfs.Dataset) error {
	name := d.Properties[libzfs.DatasetPropName].Value

	clones, err := d.Clones()
	if err != nil {
		return xerrors.Errorf("couldn't scan %q for clones", name)
	}
	if len(clones) > 0 {
		return xerrors.Errorf("%q has some clones when it shouldn't", name)
	}

	for _, cd := range d.Children {
		if err := checkNoClone(&cd); err != nil {
			return err
		}
	}
	return nil
}
