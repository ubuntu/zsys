package zfs

import (
	"strings"

	"github.com/ubuntu/zsys/internal/config"

	libzfs "github.com/bicomsystems/go-libzfs"
	"golang.org/x/xerrors"
)

const (
	zsysPrefix = "org.zsys:"
	// BootfsProp string value
	BootfsProp = zsysPrefix + "bootfs"
	// LastUsedProp string value
	LastUsedProp = zsysPrefix + "last-used"
	// SystemDataProp string value
	SystemDataProp = zsysPrefix + "system-dataset"
	// CanmountProp string value
	CanmountProp = "canmount"
)

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
	// Origin points to the dataset snapshot this one was clone from.
	Origin string

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
// It can handle a single transaction if "WithTransaction()"" is passed to the New constructor.
// An error won't then try to rollback the changes and Cancel() should be called.
// If no error happened and we want to finish the transaction before starting a new one, call "Done()".
// If no transaction support is used, any error in a method call will try to rollback changes automatically.
type Zfs struct {
	transactional  bool
	reverts        []func() error
	transactionErr bool
}

// New return a new zfs system handler.
func New(options ...func(*Zfs)) *Zfs {
	z := Zfs{}
	for _, options := range options {
		options(&z)
	}

	return &z
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

// Snapshot creates a new snapshot for dataset (and children if recursive is true) with the given name.
func (z *Zfs) Snapshot(snapName, datasetName string, recursive bool) (errSnapshot error) {
	d, err := libzfs.DatasetOpen(datasetName)
	if err != nil {
		return xerrors.Errorf("couldn't open %q: %v", datasetName, err)
	}
	defer d.Close()
	defer func() { z.saveOrRevert(errSnapshot) }()

	// We can't use the recursive version of snapshotting, as we want to track user properties and
	// set them explicitely as needed
	return z.snapshotRecursive(d, snapName, recursive)
}

// snapshotRecursive recursively try snapshotting all children and store "revert" operations by cleaning newly
// created datasets.
func (z *Zfs) snapshotRecursive(d libzfs.Dataset, snapName string, recursive bool) error {
	datasetName := d.Properties[libzfs.DatasetPropName].Value

	// Get properties from parent snapshot.
	srcProps, err := getDatasetProp(d)
	if err != nil {
		return xerrors.Errorf("can't get dataset properties for %q: "+config.ErrorFormat, datasetName, err)
	}

	props := make(map[libzfs.Prop]libzfs.Property)
	n := datasetName + "@" + snapName
	ds, err := libzfs.DatasetSnapshot(n, false, props)
	if err != nil {
		return xerrors.Errorf("couldn't snapshot %q: %v", datasetName, err)
	}
	defer ds.Close()
	z.registerRevert(func() error {
		d, err := libzfs.DatasetOpen(n)
		if err != nil {
			return xerrors.Errorf("couldn't open %q for cleanup: %v", n, err)
		}
		defer d.Close()
		if err := d.Destroy(false); err != nil {
			return xerrors.Errorf("couldn't destroy %q for cleanup: %v", n, err)
		}
		return nil
	})

	// Set user properties that we couldn't set before creating the snapshot dataset.
	// We don't set LastUsed here as Creation time will be used.
	if srcProps.sources.BootFS == "local" {
		if err := ds.SetUserProperty(BootfsProp, srcProps.BootFS); err != nil {
			return xerrors.Errorf("couldn't set user property %q to %q: "+config.ErrorFormat, BootfsProp, n, err)
		}
	}
	if srcProps.sources.SystemDataset == "local" {
		if err := ds.SetUserProperty(SystemDataProp, srcProps.SystemDataset); err != nil {
			return xerrors.Errorf("couldn't set user property %q to %q: "+config.ErrorFormat, SystemDataProp, n, err)
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
		if err := z.snapshotRecursive(cd, snapName, recursive); err != nil {
			return err
		}
	}
	return nil
}

// Clone creates a new dataset from a snapshot (and children if recursive is true) with a given suffix,
// stripping older _<suffix> if any.
func (z *Zfs) Clone(name, suffix string, recursive bool) (errClone error) {
	if suffix == "" {
		return xerrors.Errorf("no suffix was provided for cloning")
	}

	d, err := libzfs.DatasetOpen(name)
	if err != nil {
		return xerrors.Errorf("%q doesn't exist: %v", name, err)
	}
	defer d.Close()

	if !d.IsSnapshot() {
		return xerrors.Errorf("%q isn't a snapshot", name)
	}
	defer func() { z.saveOrRevert(errClone) }()

	rootName, snaphotName := separateSnaphotName(name)

	// Reformat the name with the new uuid and clone now the dataset.
	newRootName := rootName
	suffixIndex := strings.LastIndex(newRootName, "_")
	if suffixIndex != -1 {
		newRootName = newRootName[:suffixIndex]
	}
	newRootName += "_" + suffix

	parent, err := libzfs.DatasetOpen(d.Properties[libzfs.DatasetPropName].Value[:strings.LastIndex(name, "@")])
	if err != nil {
		return xerrors.Errorf("can't get parent dataset of %q: "+config.ErrorFormat, name, err)
	}
	defer parent.Close()
	if recursive {
		if err := checkSnapshotHierarchyIntegrity(parent, snaphotName, true); err != nil {
			return xerrors.Errorf("integrity check failed: %v", err)
		}
	}

	return z.cloneRecursive(d, snaphotName, rootName, newRootName, recursive)
}

// snapshotRecursive recursively clones all children and store "revert" operations by cleaning newly
// created datasets.
func (z *Zfs) cloneRecursive(d libzfs.Dataset, snaphotName, rootName, newRootName string, recursive bool) error {
	name := d.Properties[libzfs.DatasetPropName].Value

	// Get properties from snapshot and parents.
	srcProps, err := getDatasetProp(d)
	if err != nil {
		return xerrors.Errorf("can't get dataset properties for %q: "+config.ErrorFormat, name, err)
	}

	props := make(map[libzfs.Prop]libzfs.Property)
	if srcProps.sources.Mountpoint == "local" {
		props[libzfs.DatasetPropMountpoint] = libzfs.Property{
			Value:  srcProps.Mountpoint,
			Source: srcProps.sources.Mountpoint,
		}
	}
	if srcProps.sources.CanMount == "local" {
		if srcProps.CanMount == "on" {
			// don't mount new cloned dataset on top of parent.
			srcProps.CanMount = "noauto"
		}
		props[libzfs.DatasetPropCanmount] = libzfs.Property{
			Value:  srcProps.CanMount,
			Source: srcProps.sources.CanMount,
		}
	}

	datasetRelPath := strings.TrimPrefix(strings.TrimSuffix(name, "@"+snaphotName), rootName)
	n := newRootName + datasetRelPath
	cd, err := d.Clone(n, props)
	if err != nil {
		return xerrors.Errorf("couldn't clone %q to %q: "+config.ErrorFormat, name, n, err)
	}
	defer cd.Close()
	z.registerRevert(func() error {
		d, err := libzfs.DatasetOpen(n)
		if err != nil {
			return xerrors.Errorf("couldn't open %q for cleanup: %v", n, err)
		}
		defer d.Close()
		if err := d.Destroy(false); err != nil {
			return xerrors.Errorf("couldn't destroy %q for cleanup: %v", n, err)
		}
		return nil
	})

	// Set user properties that we couldn't set before creating the dataset. Based this for local
	// or source == parentName (as it will be local)
	parentName := name[:strings.LastIndex(name, "@")]
	if srcProps.sources.BootFS == "local" || srcProps.sources.BootFS == parentName {
		if err := cd.SetUserProperty(BootfsProp, srcProps.BootFS); err != nil {
			return xerrors.Errorf("couldn't set user property %q to %q: "+config.ErrorFormat, BootfsProp, n, err)
		}
	}
	if srcProps.sources.SystemDataset == "local" || srcProps.sources.SystemDataset == parentName {
		if err := cd.SetUserProperty(SystemDataProp, srcProps.SystemDataset); err != nil {
			return xerrors.Errorf("couldn't set user property %q to %q: "+config.ErrorFormat, SystemDataProp, n, err)
		}
	}
	// We don't set LastUsed in purpose as the dataset isn't used yet

	if !recursive {
		return nil
	}

	// Handle other datasets (children of parent) which may have snapshots
	parent, err := libzfs.DatasetOpen(parentName)
	if err != nil {
		return xerrors.Errorf("can't get parent dataset of %q: "+config.ErrorFormat, name, err)
	}
	defer parent.Close()

	for _, cd := range parent.Children {
		if cd.IsSnapshot() {
			continue
		}
		// Look for childrens filesystem datasets having a corresponding snapshot
		found, snapD := cd.FindSnapshotName("@" + snaphotName)
		if !found {
			continue
		}

		if err := z.cloneRecursive(snapD, snaphotName, rootName, newRootName, recursive); err != nil {
			return err
		}
	}
	return nil
}

// Promote recursively all children, including dataset named "name".
// If the hierarchy is partially promoted, promote the missing one and be no-op for the rest.
func (z *Zfs) Promote(name string) (errPromote error) {
	d, err := libzfs.DatasetOpen(name)
	if err != nil {
		return xerrors.Errorf("can't get dataset %q: "+config.ErrorFormat, name, err)
	}
	defer d.Close()

	if d.IsSnapshot() {
		return xerrors.Errorf("can't promote %q: it's a snapshot", name)
	}
	defer func() { z.saveOrRevert(errPromote) }()

	originParent, snapshotName := separateSnaphotName(d.Properties[libzfs.DatasetPropOrigin].Value)
	// Only check integrity for non promoted elements
	// Otherwise, promoting is a no-op or will repromote children
	if len(originParent) > 0 {
		parent, err := libzfs.DatasetOpen(originParent)
		if err != nil {
			return xerrors.Errorf("can't get parent dataset of %q: "+config.ErrorFormat, name, err)
		}
		defer parent.Close()
		if err := checkSnapshotHierarchyIntegrity(parent, snapshotName, true); err != nil {
			return xerrors.Errorf("integrity check failed: %v", err)
		}
	}

	return z.promoteRecursive(d)
}

func (z *Zfs) promoteRecursive(d libzfs.Dataset) error {
	name := d.Properties[libzfs.DatasetPropName].Value

	origin, _ := separateSnaphotName(d.Properties[libzfs.DatasetPropOrigin].Value)
	// Only promote if not promoted yet.
	if len(origin) > 0 {
		if err := d.Promote(); err != nil {
			return xerrors.Errorf("couldn't promote %q: "+config.ErrorFormat, name, err)
		}
		z.registerRevert(func() error {
			origD, err := libzfs.DatasetOpen(origin)
			if err != nil {
				return xerrors.Errorf("couldn't open %q for cleanup: %v", origin, err)
			}
			defer origD.Close()
			if err := origD.Promote(); err != nil {
				return xerrors.Errorf("couldn't promote %q for cleanup: %v", origin, err)
			}
			return nil
		})
	}

	for _, cd := range d.Children {
		if cd.IsSnapshot() {
			continue
		}
		if err := z.promoteRecursive(cd); err != nil {
			return err
		}
	}

	return nil
}

// Destroy recursively all children, including dataset named "name".
// If the dataset is a snapshot, navigate through the hierachy to delete all dataset with the same snapshot name.
// Note that destruction can't be rollbacked as filesystem content can't be recreated, so we don't accept them
// in a transactional Zfs element.
func (z *Zfs) Destroy(name string) error {
	if z.transactional {
		return xerrors.Errorf("couldn't call Destroy in a transactional context.")
	}

	d, err := libzfs.DatasetOpen(name)
	if err != nil {
		return xerrors.Errorf("can't get dataset %q: "+config.ErrorFormat, name, err)
	}
	defer d.Close()

	if err := checkNoClone(&d); err != nil {
		return xerrors.Errorf("couldn't destroy %q due to clones: %v", name, err)
	}

	return d.DestroyRecursive()
}

// SetProperty to given dataset if it was a local/none/snapshot directly inheriting from parent value.
// force does it even if the property was inherited.
// For zfs properties, only a fix set is supported. Right now: "canmount"
func (z *Zfs) SetProperty(name, value, datasetName string, force bool) (errSetProperty error) {
	d, err := libzfs.DatasetOpen(datasetName)
	if err != nil {
		return xerrors.Errorf("can't get dataset %q: "+config.ErrorFormat, datasetName, err)
	}
	defer d.Close()
	defer func() { z.saveOrRevert(errSetProperty) }()

	var parentName string
	if d.IsSnapshot() {
		parentName = datasetName[:strings.LastIndex(datasetName, "@")]
	}
	var prop libzfs.Property
	if !strings.Contains(name, ":") {
		var propName libzfs.Prop
		switch name {
		case CanmountProp:
			propName = libzfs.DatasetPropCanmount
		default:
			return xerrors.Errorf("can't set unsupported property %q for %q", name, datasetName)
		}
		prop, err = d.GetProperty(propName)
		if err != nil {
			return xerrors.Errorf("can't get dataset property %q for %q: "+config.ErrorFormat, name, datasetName, err)
		}
		if !force && prop.Source != "local" && prop.Source != "none" && prop.Source != parentName {
			return nil
		}
		if err = d.SetProperty(propName, value); err != nil {
			return xerrors.Errorf("can't set dataset property %q=%q for %q: "+config.ErrorFormat, name, value, datasetName, err)
		}
		z.registerRevert(func() error { return z.SetProperty(name, prop.Value, datasetName, force) })
		return nil
	}

	// User properties
	prop, err = d.GetUserProperty(name)
	if err != nil {
		return xerrors.Errorf("can't get dataset user property %q for %q: "+config.ErrorFormat, name, datasetName, err)
	}
	if !force && prop.Source != "local" && prop.Source != "none" && prop.Source != parentName {
		return nil
	}
	if err = d.SetUserProperty(name, value); err != nil {
		return xerrors.Errorf("can't set dataset user property %q=%q for %q: "+config.ErrorFormat, name, value, datasetName, err)
	}
	z.registerRevert(func() error { return z.SetProperty(name, prop.Value, datasetName, force) })

	return nil
}
