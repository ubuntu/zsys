package zfs

import (
	"log"
	"sort"
	"strconv"
	"strings"

	"github.com/ubuntu/zsys/internal/config"

	libzfs "github.com/bicomsystems/go-libzfs"
	"golang.org/x/xerrors"
)

const (
	zsysPrefix     = "org.zsys:"
	bootfsProp     = zsysPrefix + "bootfs"
	lastUsedProp   = zsysPrefix + "last-used"
	systemDataProp = zsysPrefix + "system-dataset"
)

/*
clone
destroy
rename
promote
properties write:
	- canmount (when changing current dataset)
	- org.zsys:bootfs (when cloning)
	- org.zsys:last-used (every boot)
  - org.zsys:system-dataset

*/

/*// CanMountState represents the different state of CanMount that the dataset can have.
type CanMountState int

const (
	// CanMountOff is canmount=off.
	CanMountOff CanMountState = iota
	// CanMountAuto is canmount=auto.
	CanMountAuto
	// CanMountOn is canmount=on.
	CanMountOn
)*/

// Dataset is the abstraction of a physical dataset and exposes only properties that must are accessible by the user.
type Dataset struct {
	// Name of the dataset.
	Name string
	// IsSnapshot tells if the dataset is a snapshot
	IsSnapshot bool
	DatasetProp
}

// DatasetProp abstracts some properties for a given dataset
type DatasetProp struct {
	// Mountpoint where the dataset will be mounted (without alt-root).
	Mountpoint string
	// CanMount state of the dataset.
	CanMount string
	// BootFS is a user property stating if the dataset should be mounted in the initramfs.
	BootFS string
	// LastUsed is a user property that store the last time a dataset was used.
	LastUsed int
	// SystemDataset is a user proper for user datasets, linking them to relevant system dataset.
	SystemDataset string

	// Here are the sources (not exposed to the public API) for each property
	// Used mostly for tests
	sources datasetSources
}

// datasetSources list sources some properties for a given dataset
type datasetSources struct {
	Mountpoint    string
	CanMount      string
	BootFS        string
	LastUsed      string
	SystemDataset string
}

// Zfs is a system handler talking to zfs linux module.
type Zfs struct{}

// New return a new zfs system handler.
func New() *Zfs {
	return &Zfs{}
}

// Scan returns all datasets that are currently imported on the system.
func (Zfs) Scan() ([]Dataset, error) {
	ds, err := libzfs.DatasetOpenAll()
	if err != nil {
		return nil, xerrors.Errorf("can't list datasets: %v", err)
	}
	defer libzfs.DatasetCloseAll(ds)

	var datasets []Dataset
	for _, d := range ds {
		datasets = append(datasets, collectDatasets(d)...)
	}

	return datasets, nil
}

// getDatasetsProp returns all properties for a given dataset and the source of them.
// for snapshots, we'll take the parent dataset for the mount properties.
func getDatasetProp(d libzfs.Dataset) (*DatasetProp, error) {
	sources := datasetSources{}

	name, err := d.Path()
	if err != nil {
		return nil, xerrors.Errorf("can't get dataset path: "+config.ErrorFormat, err)
	}

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
	sources.CanMount = cm.Source

	bfs, err := d.GetUserProperty(bootfsProp)
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
		lu, err = d.GetUserProperty(lastUsedProp)
		if err != nil {
			return nil, xerrors.Errorf("can't get %q property: "+config.ErrorFormat, lastUsedProp, err)
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
		return nil, xerrors.Errorf("%q property isn't an int: "+config.ErrorFormat, lastUsedProp, err)
	}

	sDataset, err := d.GetUserProperty(systemDataProp)
	if err != nil {
		return nil, xerrors.Errorf("can't get %q property: "+config.ErrorFormat, systemDataProp, err)
	}
	sources.SystemDataset = sDataset.Source
	systemDataset := sDataset.Value
	if systemDataset == "-" {
		systemDataset = ""
	}

	return &DatasetProp{
		Mountpoint:    mountpoint,
		CanMount:      canMount,
		BootFS:        bootfs,
		LastUsed:      lastused,
		SystemDataset: systemDataset,
		sources:       sources,
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

	name, err := d.Path()
	if err != nil {
		collectErr = xerrors.Errorf("can't get dataset path: "+config.ErrorFormat, err)
		return nil
	}

	props, err := getDatasetProp(d)
	if err != nil {
		collectErr = xerrors.Errorf("can't get dataset properties for %q: "+config.ErrorFormat, name, collectErr)
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

// Snapshot creates a new snapshot for dataset (and children if recursive is true) with the given name.
func (Zfs) Snapshot(snapName, datasetName string, recursive bool) (errSnapshot error) {
	d, err := libzfs.DatasetOpen(datasetName)
	if err != nil {
		return xerrors.Errorf("couldn't open %q: %v", datasetName, err)
	}
	defer d.Close()

	// We can't use the recursive version of snapshotting, as we want to track user properties and
	// set them explicitely as needed

	// Cleanup newly created snapshot datasets if we or a children returns an error (like intermediate datasets have a snapshot)
	var snapshotDatasetNames []string
	defer func() {
		if snapshotDatasetNames == nil || errSnapshot == nil {
			return
		}
		// start with leaves to undo clone creation
		sort.Sort(sort.Reverse(sort.StringSlice(snapshotDatasetNames)))
		for _, n := range snapshotDatasetNames {
			d, err := libzfs.DatasetOpen(n)
			if err != nil {
				log.Printf("couldn't open %q for cleanup: %v", n, err)
				continue
			}
			defer d.Close()
			if err := d.Destroy(false); err != nil {
				log.Printf("couldn't destroy %q for cleanup: %v", n, err)
			}
		}
	}()

	// recursively try snapshotting all children. If an error is returned, the closure will clean newly created dataset
	var snapshotInternal func(libzfs.Dataset) error
	snapshotInternal = func(d libzfs.Dataset) error {
		datasetName, err := d.Path()
		if err != nil {
			return xerrors.Errorf("can't get dataset path: "+config.ErrorFormat, err)
		}

		// Get properties from parent snapshot.
		srcProps, err := getDatasetProp(d)
		if err != nil {
			return xerrors.Errorf("can't get dataset properties for %q: "+config.ErrorFormat, datasetName, err)
		}

		props := make(map[libzfs.Prop]libzfs.Property)
		snapshotDatasetName := datasetName + "@" + snapName
		ds, err := libzfs.DatasetSnapshot(snapshotDatasetName, false, props)
		if err != nil {
			return xerrors.Errorf("couldn't snapshot %q: %v", datasetName, err)
		}
		snapshotDatasetNames = append(snapshotDatasetNames, snapshotDatasetName)
		defer ds.Close()

		// Set user properties that we couldn't set before creating the snapshot dataset.
		// We don't set LastUsed here as Creation time will be used.
		if srcProps.sources.BootFS == "local" {
			if err := ds.SetUserProperty(bootfsProp, srcProps.BootFS); err != nil {
				return xerrors.Errorf("couldn't set user property %q to %q: "+config.ErrorFormat, bootfsProp, snapshotDatasetName, err)
			}
		}
		if srcProps.sources.SystemDataset == "local" {
			if err := ds.SetUserProperty(systemDataProp, srcProps.SystemDataset); err != nil {
				return xerrors.Errorf("couldn't set user property %q to %q: "+config.ErrorFormat, systemDataProp, snapshotDatasetName, err)
			}
		}

		if !recursive {
			return nil
		}

		// Take snapshots on non snapshot dataset children
		for _, cd := range d.Children {
			if cd.IsSnapshot() {
				continue
			}
			if err := snapshotInternal(cd); err != nil {
				return err
			}
		}
		return nil
	}

	return snapshotInternal(d)
}

// Clone creates a new dataset from a snapshot (and children if recursive is true) with a given suffix,
// stripping older _<suffix> if any.
func (Zfs) Clone(snapshotName, suffix string, recursive bool) (err error) {
	d, err := libzfs.DatasetOpen(snapshotName)
	if err != nil {
		return xerrors.Errorf("%q doesn't exist: %v", snapshotName, err)
	}
	defer d.Close()

	if !d.IsSnapshot() {
		return xerrors.Errorf("%q isn't a snapshot", snapshotName)
	}

	// Reformat the name with the new uuid
	name := snapshotName[:strings.LastIndex(snapshotName, "@")]
	suffixIndex := strings.LastIndex(snapshotName, "_")
	if suffixIndex != -1 {
		name = name[:suffixIndex]
	}
	name += "_" + suffix

	// Cleanup newly created datasets if we return an error (like intermediate datasets have a snapshot)
	createdDatasets := struct {
		d []libzfs.Dataset
	}{}
	defer func() {
		for _, d := range createdDatasets.d {
			if err != nil {
				d.Destroy(false)
			}
			d.Close()
		}
	}()

	// Get properties from parent
	/*		Mountpoint:    mountpoint.Value,
				CanMount:      canMount.Value,
				BootFS:        bootfs.Value,
				LastUsed:      lastused,
	      SystemDataset: systemDataset.Value,
	*/
	props := make(map[libzfs.Prop]libzfs.Property)
	mountpoint, err := d.GetProperty(libzfs.DatasetPropMountpoint)
	if err != nil {
		return xerrors.Errorf("can't get mountpoint for %q: "+config.ErrorFormat, name, err)
	}
	props[libzfs.DatasetPropMountpoint] = mountpoint

	canMount, err := d.GetProperty(libzfs.DatasetPropCanmount)
	if err != nil {
		return xerrors.Errorf("can't get canmount property for %q: "+config.ErrorFormat, name, err)
	}
	canM := canMount.Value
	if canM == "on" {
		// don't mount new cloned dataset on top of parent
		canM = "noauto"
	}
	props[libzfs.DatasetPropCanmount] = libzfs.Property{Value: canM, Source: canMount.Source}

	bootfs, err := d.GetUserProperty(bootfsProp)
	if err != nil {
		return xerrors.Errorf("can't get bootfs property for %q: "+config.ErrorFormat, name, err)
	}

	creation, err := d.GetProperty(libzfs.DatasetPropCreation)
	if err != nil {
		return xerrors.Errorf("can't get creation property for %q: "+config.ErrorFormat, name, err)
	}

	/*lastused, err := strconv.Atoi(lu.Value)
	if err != nil {
		return xerrors.Errorf("last-used property for %q isn't an int: %v", name, err)
	}*/

	systemDataset, err := d.GetUserProperty(systemDataProp)
	if err != nil {
		return xerrors.Errorf("can't get %q property for %q: "+config.ErrorFormat, systemDataProp, name, err)
	}

	// Clone now the dataset.
	clonedDataset, err := d.Clone(name, props)
	if err != nil {
		return xerrors.Errorf("couldn't clone %q to %q: "+config.ErrorFormat, snapshotName, name, err)
	}
	createdDatasets.d = append([]libzfs.Dataset{clonedDataset}, createdDatasets.d...)

	// Set user properties that we couldn't set before creating the dataset.
	if err := clonedDataset.SetUserProperty(bootfsProp, bootfs.Value); err != nil {
		return xerrors.Errorf("couldn't set user property %q to %q: "+config.ErrorFormat, bootfsProp, name, err)
	}
	if err := clonedDataset.SetUserProperty(lastUsedProp, creation.Value); err != nil {
		return xerrors.Errorf("couldn't set user property %q to %q: "+config.ErrorFormat, lastUsedProp, name, err)
	}
	if err := clonedDataset.SetUserProperty(systemDataProp, systemDataset.Value); err != nil {
		return xerrors.Errorf("couldn't set user property %q to %q: "+config.ErrorFormat, systemDataset, name, err)
	}

	/*for _, cd := range d.Children {
		_ = cd
	}*/

	return nil
}
