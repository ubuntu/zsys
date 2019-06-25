package zfs

import (
	"log"
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

// collectDatasets returns a Dataset tuple of all its properties and children
func collectDatasets(d libzfs.Dataset) []Dataset {
	var results []Dataset
	var err error

	defer func() {
		if err != nil {
			log.Printf("couldn't load dataset: "+config.ErrorFormat+"\n", err)
		}
	}()

	// Skip non file system or snapshot datasets
	if d.Type == libzfs.DatasetTypeVolume || d.Type == libzfs.DatasetTypeBookmark {
		return nil
	}

	name, err := d.Path()
	if err != nil {
		err = xerrors.Errorf("can't get dataset path: "+config.ErrorFormat, err)
		return nil
	}

	var mountpoint, canMount string
	if !d.IsSnapshot() {
		mp, err := d.GetProperty(libzfs.DatasetPropMountpoint)
		if err != nil {
			err = xerrors.Errorf("can't get mountpoint for %q: "+config.ErrorFormat, name, err)
			return nil
		}

		p, err := d.Pool()
		if err != nil {
			err = xerrors.Errorf("can't get pool corresponding to %q: "+config.ErrorFormat, name, err)
			return nil
		}
		poolRoot, err := p.GetProperty(libzfs.PoolPropAltroot)
		if err != nil {
			err = xerrors.Errorf("can't get altroot for pool corresponding to %q: "+config.ErrorFormat, name, err)
			return nil
		}
		mountpoint = strings.TrimPrefix(mp.Value, poolRoot.Value)
		if mountpoint == "" {
			mountpoint = "/"
		}

		cm, err := d.GetProperty(libzfs.DatasetPropCanmount)
		if err != nil {
			err = xerrors.Errorf("can't get canmount property for %q: "+config.ErrorFormat, name, err)
			return nil
		}
		canMount = cm.Value
	}

	bfs, err := d.GetUserProperty(bootfsProp)
	if err != nil {
		err = xerrors.Errorf("can't get bootfs property for %q: "+config.ErrorFormat, name, err)
		return nil
	}
	bootfs := bfs.Value
	if bootfs == "-" {
		bootfs = ""
	}

	var lu libzfs.Property
	if !d.IsSnapshot() {
		lu, err = d.GetUserProperty(lastUsedProp)
		if err != nil {
			err = xerrors.Errorf("can't get %q property for %q: "+config.ErrorFormat, lastUsedProp, name, err)
			return nil
		}
	} else {
		lu, err = d.GetProperty(libzfs.DatasetPropCreation)
		if err != nil {
			err = xerrors.Errorf("can't get creation property for %q: "+config.ErrorFormat, name, err)
			return nil
		}
	}
	if lu.Value == "-" {
		lu.Value = "0"
	}
	lastused, err := strconv.Atoi(lu.Value)
	if err != nil {
		err = xerrors.Errorf("%q property for %q isn't an int: "+config.ErrorFormat, lastUsedProp, name, err)
		return nil
	}

	sDataset, err := d.GetUserProperty(systemDataProp)
	if err != nil {
		err = xerrors.Errorf("can't get %q property for %q: "+config.ErrorFormat, systemDataProp, name, err)
		return nil
	}
	systemDataset := sDataset.Value
	if systemDataset == "-" {
		systemDataset = ""
	}

	results = append(results,
		Dataset{
			Name:          name,
			IsSnapshot:    d.IsSnapshot(),
			Mountpoint:    mountpoint,
			CanMount:      canMount,
			BootFS:        bootfs,
			LastUsed:      lastused,
			SystemDataset: systemDataset,
		})

	for _, dc := range d.Children {
		results = append(results, collectDatasets(dc)...)
	}

	return results
}

// Snapshot creates a new snapshot for dataset (and children if recursive is true) with the given name.
func (Zfs) Snapshot(name, datasetName string, recursive bool) error {
	d, err := libzfs.DatasetOpen(datasetName)
	if err != nil {
		return xerrors.Errorf("couldn't open %q: %v", datasetName, err)
	}
	defer d.Close()

	// List all subdataset to ensure there is no snapshot named "name".
	// libzfs.DatasetSnapshot already does this check. However, the error message doesn't tell which dataset
	// has already a snapshot with the same name.
	if recursive {
		for _, dc := range d.Children {
			n, err := dc.Path()
			if err != nil {
				return xerrors.Errorf("couldn't check children dataset %q: %v", n, err)
			}
			snapDatasetName := n + "@" + name
			snapDataset, err := libzfs.DatasetOpen(snapDatasetName)
			if err == nil {
				snapDataset.Close()
				return xerrors.Errorf("%q already exists", snapDatasetName)
			}
		}
	}

	props := make(map[libzfs.Prop]libzfs.Property)
	ds, err := libzfs.DatasetSnapshot(datasetName+"@"+name, recursive, props)
	if err != nil {
		return xerrors.Errorf("couldn't snapshot %q: %v", datasetName, err)
	}
	defer ds.Close()

	return nil
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
