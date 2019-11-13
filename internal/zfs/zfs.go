package zfs

import (
	"context"
	"fmt"
	"strings"

	libzfs "github.com/bicomsystems/go-libzfs"
	"github.com/ubuntu/zsys/internal/config"
	"github.com/ubuntu/zsys/internal/i18n"
	"github.com/ubuntu/zsys/internal/log"
)

const (
	zsysPrefix = "com.ubuntu.zsys:"
	// BootfsProp string value
	BootfsProp = zsysPrefix + "bootfs"
	// LastUsedProp string value
	LastUsedProp = zsysPrefix + "last-used"
	// BootfsDatasetsProp string value
	BootfsDatasetsProp = zsysPrefix + "bootfs-datasets"
	// LastBootedKernelProp string value
	LastBootedKernelProp = zsysPrefix + "last-booted-kernel"
	// CanmountProp string value
	CanmountProp = "canmount"
	// MountPointProp string value
	MountPointProp = "mountpoint"
)

// Dataset is the abstraction of a physical dataset and exposes only properties that must are accessible by the user.
type Dataset struct {
	// Name of the dataset.
	Name string
	// IsSnapshot tells if the dataset is a snapshot
	IsSnapshot bool `json:",omitempty"`
	DatasetProp
}

// DatasetProp abstracts some properties for a given dataset
type DatasetProp struct {
	// Mountpoint where the dataset will be mounted (without alt-root).
	Mountpoint string `json:",omitempty"`
	// CanMount state of the dataset.
	CanMount string `json:",omitempty"`
	// Mounted report if dataset is mounted
	Mounted bool `json:",omitempty"`
	// BootFS is a user property stating if the dataset should be mounted in the initramfs.
	BootFS bool `json:",omitempty"`
	// LastUsed is a user property that store the last time a dataset was used.
	LastUsed int `json:",omitempty"`
	// LastBootedKernel is a user property storing what latest kernel was a root dataset successfully boot with.
	LastBootedKernel string `json:",omitempty"`
	// BootfsDatasets is a user property for user datasets, linking them to relevant system bootfs datasets.
	BootfsDatasets string `json:",omitempty"`
	// Origin points to the dataset snapshot this one was clone from.
	Origin string `json:",omitempty"`

	// Here are the sources (not exposed to the public API) for each property
	// Used mostly for tests
	sources datasetSources
}

// datasetSources list sources some properties for a given dataset
type datasetSources struct {
	Mountpoint       string `json:",omitempty"`
	BootFS           string `json:",omitempty"`
	LastUsed         string `json:",omitempty"`
	LastBootedKernel string `json:",omitempty"`
	BootfsDatasets   string `json:",omitempty"`
}

// Zfs is a system handler talking to zfs linux module.
// It can handle transactions if the context passed to the constructor is cancellable (timeout, deadline or cancel).
// We finally need to call Done() to end a transaction. This object shouldn't be reused then.
// If cancel() on the context is called before Done(), we'll rollback the changes.
// Some multi-zfs-calls functions will create a subtransaction and try to rollback those in-flight changes in case of error.
type Zfs struct {
	ctx context.Context

	cancel context.CancelFunc
	commit chan struct{}
	done   chan struct{}

	reverts []func() error
}

// New returns a new zfs system handler.
func New(ctx context.Context, options ...func(*Zfs)) *Zfs {
	z := Zfs{
		ctx:    ctx,
		commit: make(chan struct{}),
		done:   make(chan struct{}),
	}
	for _, options := range options {
		options(&z)
	}

	ready := make(chan struct{})

	// cancel transactions when context is cancelled before any commit
	go func() {
		close(ready)
		select {
		case <-z.ctx.Done():
			log.Debugf(z.ctx, i18n.G("ZFS: reverting all in progress zfs transactions"))
			for i := len(z.reverts) - 1; i >= 0; i-- {
				if err := z.reverts[i](); err != nil {
					log.Warningf(z.ctx, i18n.G("An error occurred when reverting a Zfs transaction: ")+config.ErrorFormat, err)
				}
			}
			z.reverts = nil
		case <-z.commit:
		}
		z.done <- struct{}{}
	}()
	// wait for teardown goroutine to start
	<-ready

	return &z
}

// NewWithCancel returns a new zfs system handler and its associated cancellable context.
// It returns the context cancel function which should always be called to release ressources.
// Call it before Done() to cancel the transaction or after Done() to be a no-op.
func NewWithCancel(ctx context.Context, options ...func(*Zfs)) (*Zfs, context.CancelFunc) {
	ctx, cancel := context.WithCancel(ctx)
	z := New(ctx, options...)

	return z, cancel
}

// NewWithAutoCancel returns a new zfs system handler which can autocancel in case DoneCheckErr is called.
// If the given error it points at is not nil, it will cancel the transaction.
// A call to Done() is equivalent to DoneCheckErr(nil).
// First Done() or DoneCheckErr is relevant and the following calls are no-op.
// FIXME: we should prevent it being called when there is an autocancel error
func NewWithAutoCancel(ctx context.Context, options ...func(*Zfs)) *Zfs {
	ctx, cancel := context.WithCancel(ctx)
	z := New(ctx, func(z *Zfs) {
		z.cancel = cancel
	})

	return z
}

// newTransaction create a new Zfs handler for a sub transaction. This is useful for recursive calls
// to hide implementations details from outside. If an error is not nil in the function callback, the
// sub transaction will be reverted. If none is found, the in progress reverts will be appended to the
// parent transaction.
func (z *Zfs) newTransaction() (*Zfs, func(err *error)) {
	// Create a subtransaction (for recursive calls)
	subctx, subrevert := context.WithCancel(z.ctx)
	subz := New(subctx)
	return subz,
		func(err *error) {
			if *err != nil {
				subrevert()
			} else {
				// attach reverts to parent context and purge current routine
				z.reverts = append(z.reverts, subz.reverts...)
			}

			// Purge current subtransaction
			subz.Done()
		}
}

// registerRevert is a helper for defer() setting error value
func (z *Zfs) registerRevert(f func() error) {
	z.reverts = append(z.reverts, f)
}

// Context returns underlying context created or associated to the Zfs object.
func (z *Zfs) Context() context.Context {
	return z.ctx
}

// Done commits current changes.
// This should be called to release underlying resources and will block until all is good.
// This is a no-op if associated context was already cancelled or isn't cancellable.
// First Done() or DoneCheckErr is relevant and the following calls are no-op.
func (z *Zfs) Done() {
	z.DoneCheckErr(nil)
}

// DoneCheckErr commits current changes if associated error it's pointing at is nil.
// It will otherwise revert the commit and cancel the associated internal context.
// It should be called with err != nil only on zfs object created with NewWithAutoCancel
// or it will panic otherwise.
// First Done() or DoneCheckErr is relevant and the following calls are no-op.
func (z *Zfs) DoneCheckErr(err *error) {
	// We already committed, other calls are no-op
	if z.commit == nil {
		return
	}

	if err != nil && *err != nil {
		if z.cancel == nil {
			panic("DoneCheckErr with a non nil error called on an zfs object not created with NewWithAutoCancel")
		}
		z.cancel()
	}

	select {
	// try to send a commit to purge goroutine
	case z.commit <- struct{}{}:
	default:
		<-z.done
		z.commit = nil
		return
	}

	log.Debugf(z.ctx, i18n.G("ZFS: committing transaction"))
	z.reverts = nil
	<-z.done
	z.commit = nil
}

// Create creates a dataset for that path.
func (z *Zfs) Create(path, mountpoint, canmount string) error {
	log.Debugf(z.ctx, i18n.G("ZFS: trying to Create %q with mountpoint %q"), path, mountpoint)
	props := make(map[libzfs.Prop]libzfs.Property)
	if mountpoint != "" {
		props[libzfs.DatasetPropMountpoint] = libzfs.Property{Value: mountpoint}
	}
	props[libzfs.DatasetPropCanmount] = libzfs.Property{Value: canmount}

	d, err := libzfs.DatasetCreate(path, libzfs.DatasetTypeFilesystem, props)
	if err != nil {
		return fmt.Errorf(i18n.G("can't create %q: %v"), path, err)
	}
	defer d.Close()

	z.registerRevert(func() error {
		d, err := libzfs.DatasetOpen(path)
		if err != nil {
			return fmt.Errorf(i18n.G("couldn't open %q for cleanup: %v"), path, err)
		}
		defer d.Close()
		if err := d.Destroy(false); err != nil {
			return fmt.Errorf(i18n.G("couldn't destroy %q for cleanup: %v"), path, err)
		}
		return nil
	})

	return nil
}

// Scan returns all datasets that are currently imported on the system.
func (z *Zfs) Scan() ([]Dataset, error) {
	log.Debug(z.ctx, i18n.G("ZFS: scan requested"))
	ds, err := libzfs.DatasetOpenAll()
	if err != nil {
		return nil, fmt.Errorf(i18n.G("can't list datasets: %v"), err)
	}
	defer libzfs.DatasetCloseAll(ds)

	// Refresh cache
	datasetPropertiesCache = make(map[string]*DatasetProp)

	var datasets []Dataset
	for _, d := range ds {
		datasets = append(datasets, collectDatasets(d)...)
	}

	return datasets, nil
}

// Snapshot creates a new snapshot for dataset (and children if recursive is true) with the given name.
func (z *Zfs) Snapshot(snapName, datasetName string, recursive bool) (errSnapshot error) {
	log.Debugf(z.ctx, i18n.G("ZFS: trying to snapshot %q, recursive: %v"), datasetName, recursive)
	d, err := libzfs.DatasetOpen(datasetName)
	if err != nil {
		return fmt.Errorf(i18n.G("couldn't open %q: %v"), datasetName, err)
	}
	defer d.Close()

	subz, done := z.newTransaction()
	defer done(&errSnapshot)

	// We can't use the recursive version of snapshotting, as we want to track user properties and
	// set them explicitly as needed
	return subz.snapshotRecursive(d, snapName, recursive)
}

// snapshotRecursive recursively try snapshotting all children and store "revert" operations by cleaning newly
// created datasets.
func (z *Zfs) snapshotRecursive(d libzfs.Dataset, snapName string, recursive bool) error {
	datasetName := d.Properties[libzfs.DatasetPropName].Value

	// Get properties from parent snapshot.
	srcProps, err := getDatasetProp(d)
	if err != nil {
		return fmt.Errorf(i18n.G("can't get dataset properties for %q: ")+config.ErrorFormat, datasetName, err)
	}

	props := make(map[libzfs.Prop]libzfs.Property)
	n := datasetName + "@" + snapName
	ds, err := libzfs.DatasetSnapshot(n, false, props)
	if err != nil {
		return fmt.Errorf(i18n.G("couldn't snapshot %q: %v"), datasetName, err)
	}
	defer ds.Close()
	z.registerRevert(func() error {
		d, err := libzfs.DatasetOpen(n)
		if err != nil {
			return fmt.Errorf(i18n.G("couldn't open %q for cleanup: %v"), n, err)
		}
		defer d.Close()
		if err := d.Destroy(false); err != nil {
			return fmt.Errorf(i18n.G("couldn't destroy %q for cleanup: %v"), n, err)
		}
		return nil
	})

	// Set user properties that we couldn't set before creating the snapshot dataset.
	// We don't set LastUsed here as Creation time will be used.
	if srcProps.sources.BootFS == "local" {
		bootfsValue := "no"
		if srcProps.BootFS {
			bootfsValue = "yes"
		}
		if err := ds.SetUserProperty(BootfsProp, bootfsValue); err != nil {
			return fmt.Errorf(i18n.G("couldn't set user property %q to %q: ")+config.ErrorFormat, BootfsProp, n, err)
		}
	}
	if srcProps.sources.BootfsDatasets == "local" {
		if err := ds.SetUserProperty(BootfsDatasetsProp, srcProps.BootfsDatasets); err != nil {
			return fmt.Errorf(i18n.G("couldn't set user property %q to %q: ")+config.ErrorFormat, BootfsDatasetsProp, n, err)
		}
	}
	if srcProps.sources.LastBootedKernel == "local" {
		if err := ds.SetUserProperty(LastBootedKernelProp, srcProps.LastBootedKernel); err != nil {
			return fmt.Errorf(i18n.G("couldn't set user property %q to %q: ")+config.ErrorFormat, LastBootedKernelProp, n, err)
		}
	}

	if !recursive {
		return nil
	}

	// Take snapshots on non snapshot dataset children
	return recurseFileSystemDatasets(d,
		func(next libzfs.Dataset) error {
			return z.snapshotRecursive(next, snapName, true)
		})
}

// Clone creates a new dataset from a snapshot (and children if recursive is true) with a given suffix,
// stripping older _<suffix> if any.
func (z *Zfs) Clone(name, suffix string, skipBootfs, recursive bool) (errClone error) {
	log.Debugf(z.ctx, i18n.G("ZFS: trying to clone %q"), name)
	if suffix == "" {
		return fmt.Errorf(i18n.G("no suffix was provided for cloning"))
	}

	d, err := libzfs.DatasetOpen(name)
	if err != nil {
		return fmt.Errorf(i18n.G("%q doesn't exist: %v"), name, err)
	}
	defer d.Close()

	if !d.IsSnapshot() {
		return fmt.Errorf(i18n.G("%q isn't a snapshot"), name)
	}

	subz, done := z.newTransaction()
	defer done(&errClone)

	rootName, snapshotName := splitSnapshotName(name)

	// Reformat the name with the new uuid and clone now the dataset.
	newRootName := rootName
	suffixIndex := strings.LastIndex(newRootName, "_")
	if suffixIndex != -1 {
		newRootName = newRootName[:suffixIndex]
	}
	newRootName += "_" + suffix

	parent, err := libzfs.DatasetOpen(d.Properties[libzfs.DatasetPropName].Value[:strings.LastIndex(name, "@")])
	if err != nil {
		return fmt.Errorf(i18n.G("can't get parent dataset of %q: ")+config.ErrorFormat, name, err)
	}
	defer parent.Close()
	if recursive {
		if err := checkSnapshotHierarchyIntegrity(parent, snapshotName, true); err != nil {
			return fmt.Errorf(i18n.G("integrity check failed: %v"), err)
		}
	}

	return subz.cloneRecursive(d, snapshotName, rootName, newRootName, skipBootfs, recursive)
}

// cloneRecursive recursively clones all children and store "revert" operations by cleaning newly
// created datasets.
func (z *Zfs) cloneRecursive(d libzfs.Dataset, snapshotName, rootName, newRootName string, skipBootfs, recursive bool) error {
	name := d.Properties[libzfs.DatasetPropName].Value
	parentName := name[:strings.LastIndex(name, "@")]

	// Get properties from snapshot and parents.
	srcProps, err := getDatasetProp(d)
	if err != nil {
		return fmt.Errorf(i18n.G("can't get dataset properties for %q: ")+config.ErrorFormat, name, err)
	}

	datasetRelPath := strings.TrimPrefix(strings.TrimSuffix(name, "@"+snapshotName), rootName)
	if (!skipBootfs && srcProps.BootFS) || !srcProps.BootFS {
		if err := z.cloneDataset(d, newRootName+datasetRelPath, *srcProps, parentName); err != nil {
			return err
		}
	}

	if !recursive {
		return nil
	}

	// Handle other datasets (children of parent) which may have snapshots
	parent, err := libzfs.DatasetOpen(parentName)
	if err != nil {
		return fmt.Errorf(i18n.G("can't get parent dataset of %q: ")+config.ErrorFormat, name, err)
	}
	defer parent.Close()

	return recurseFileSystemDatasets(parent,
		func(next libzfs.Dataset) error {
			// Look for childrens filesystem datasets having a corresponding snapshot
			found, snapD := next.FindSnapshotName("@" + snapshotName)
			if !found {
				return nil
			}

			return z.cloneRecursive(snapD, snapshotName, rootName, newRootName, skipBootfs, true)
		})
}

func (z *Zfs) cloneDataset(d libzfs.Dataset, target string, srcProps DatasetProp, parentName string) error {
	props := make(map[libzfs.Prop]libzfs.Property)
	if srcProps.sources.Mountpoint == "local" {
		props[libzfs.DatasetPropMountpoint] = libzfs.Property{
			Value:  srcProps.Mountpoint,
			Source: srcProps.sources.Mountpoint,
		}
	}

	// CanMount is always local
	if srcProps.CanMount == "on" {
		// don't mount new cloned dataset on top of parent.
		srcProps.CanMount = "noauto"
	}
	props[libzfs.DatasetPropCanmount] = libzfs.Property{
		Value:  srcProps.CanMount,
		Source: "local",
	}

	cd, err := d.Clone(target, props)
	if err != nil {
		name := d.Properties[libzfs.DatasetPropName].Value
		return fmt.Errorf(i18n.G("couldn't clone %q to %q: ")+config.ErrorFormat, name, target, err)
	}
	defer cd.Close()
	z.registerRevert(func() error {
		d, err := libzfs.DatasetOpen(target)
		if err != nil {
			return fmt.Errorf(i18n.G("couldn't open %q for cleanup: %v"), target, err)
		}
		defer d.Close()
		if err := d.Destroy(false); err != nil {
			return fmt.Errorf(i18n.G("couldn't destroy %q for cleanup: %v"), target, err)
		}
		return nil
	})

	// Set user properties that we couldn't set before creating the dataset. Based this for local
	// or source == parentName (as it will be local)
	if srcProps.sources.BootFS == "local" || srcProps.sources.BootFS == parentName {
		bootfsValue := "no"
		if srcProps.BootFS {
			bootfsValue = "yes"
		}
		if err := cd.SetUserProperty(BootfsProp, bootfsValue); err != nil {
			return fmt.Errorf(i18n.G("couldn't set user property %q to %q: ")+config.ErrorFormat, BootfsProp, target, err)
		}
	}
	// We don't set BootfsDatasets as this property can't be translated to new datasets
	// We don't set LastUsed on purpose as the dataset isn't used yet

	return nil
}

// Promote recursively all children, including dataset named "name".
// If the hierarchy is partially promoted, promote the missing one and be no-op for the rest.
func (z *Zfs) Promote(name string) (errPromote error) {
	log.Debugf(z.ctx, i18n.G("ZFS: trying to promote %q"), name)
	d, err := libzfs.DatasetOpen(name)
	if err != nil {
		return fmt.Errorf(i18n.G("can't get dataset %q: ")+config.ErrorFormat, name, err)
	}
	defer d.Close()

	if d.IsSnapshot() {
		return fmt.Errorf(i18n.G("can't promote %q: it's a snapshot"), name)
	}

	subz, done := z.newTransaction()
	defer done(&errPromote)

	originParent, snapshotName := splitSnapshotName(d.Properties[libzfs.DatasetPropOrigin].Value)
	// Only check integrity for non promoted elements
	// Otherwise, promoting is a no-op or will repromote children
	if len(originParent) > 0 {
		parent, err := libzfs.DatasetOpen(originParent)
		if err != nil {
			return fmt.Errorf(i18n.G("can't get parent dataset of %q: ")+config.ErrorFormat, name, err)
		}
		defer parent.Close()
		if err := checkSnapshotHierarchyIntegrity(parent, snapshotName, true); err != nil {
			return fmt.Errorf(i18n.G("integrity check failed: %v"), err)
		}
	}

	return subz.promoteRecursive(d)
}

func (z *Zfs) promoteRecursive(d libzfs.Dataset) error {
	name := d.Properties[libzfs.DatasetPropName].Value

	origin, _ := splitSnapshotName(d.Properties[libzfs.DatasetPropOrigin].Value)
	// Only promote if not promoted yet.
	if len(origin) > 0 {
		if err := d.Promote(); err != nil {
			return fmt.Errorf(i18n.G("couldn't promote %q: ")+config.ErrorFormat, name, err)
		}
		z.registerRevert(func() error {
			origD, err := libzfs.DatasetOpen(origin)
			if err != nil {
				return fmt.Errorf(i18n.G("couldn't open %q for cleanup: %v"), origin, err)
			}
			defer origD.Close()
			if err := origD.Promote(); err != nil {
				return fmt.Errorf(i18n.G("couldn't promote %q for cleanup: %v"), origin, err)
			}
			return nil
		})
	}

	return recurseFileSystemDatasets(d,
		func(next libzfs.Dataset) error {
			return z.promoteRecursive(next)
		})
}

// Destroy recursively all children, including dataset named "name".
// If the dataset is a snapshot, navigate through the hierarchy to delete all dataset with the same snapshot name.
// Note that destruction can't be rollbacked as filesystem content can't be recreated, so we don't accept them
// in a transactional Zfs element.
func (z *Zfs) Destroy(name string) error {
	log.Debugf(z.ctx, i18n.G("ZFS: trying to destroy %q"), name)
	if z.ctx.Done() != nil {
		return fmt.Errorf(i18n.G("couldn't call Destroy in a transactional context"))
	}

	d, err := libzfs.DatasetOpen(name)
	if err != nil {
		return fmt.Errorf(i18n.G("can't get dataset %q: ")+config.ErrorFormat, name, err)
	}
	defer d.Close()

	if err := checkNoClone(&d); err != nil {
		return fmt.Errorf(i18n.G("couldn't destroy %q due to clones: %v"), name, err)
	}

	return d.DestroyRecursive()
}

// SetProperty to given dataset if it was a local/none/snapshot directly inheriting from parent value.
// force does it even if the property was inherited.
// For zfs properties, only a fix set is supported. Right now: "canmount"
func (z *Zfs) SetProperty(name, value, datasetName string, force bool) error {
	log.Debugf(z.ctx, i18n.G("ZFS: trying to set %q=%q on %q"), name, value, datasetName)
	d, err := libzfs.DatasetOpen(datasetName)
	if err != nil {
		return fmt.Errorf(i18n.G("can't get dataset %q: ")+config.ErrorFormat, datasetName, err)
	}
	defer d.Close()

	if d.IsSnapshot() {
		return fmt.Errorf(i18n.G("can't set a property %q on %q: the dataset a snapshot"), name, datasetName)
	}

	prop, err := getProperty(d, name)
	if err != nil {
		return fmt.Errorf(i18n.G("can't get dataset property %q for %q: ")+config.ErrorFormat, name, datasetName, err)
	}
	if !force && prop.Source != "local" && prop.Source != "default" && prop.Source != "none" && prop.Source != "" {
		log.Debugf(z.ctx, i18n.G("ZFS: can't set property %q=%q for %q as not a local property (%q)"), name, value, datasetName, prop.Source)
		return nil
	}
	if err = setProperty(d, name, value); err != nil {
		return fmt.Errorf(i18n.G("can't set dataset property %q=%q for %q: ")+config.ErrorFormat, name, value, datasetName, err)
	}
	z.registerRevert(func() error { return z.SetProperty(name, prop.Value, datasetName, force) })

	return nil
}
