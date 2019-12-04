package zfs

import (
	"context"
	"fmt"
	"path/filepath"
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
	// SnapshotCanmountProp is the equivalent to CanmountProp, but as a user property to store on zsys snapshot
	SnapshotCanmountProp = zsysPrefix + CanmountProp
	// MountPointProp string value
	MountPointProp = "mountpoint"
	// SnapshotMountpointProp is the equivalent to MountPointProp, but as a user property to store on zsys snapshot
	SnapshotMountpointProp = zsysPrefix + MountPointProp
)

// Dataset is the abstraction of a physical dataset and exposes only properties that must are accessible by the user.
type Dataset struct {
	// Name of the dataset.
	Name       string
	IsSnapshot bool `json:",omitempty"`
	DatasetProp

	children []*Dataset
	dZFS     *libzfs.Dataset
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
	CanMount         string `json:",omitempty"`
	BootFS           string `json:",omitempty"`
	LastUsed         string `json:",omitempty"`
	LastBootedKernel string `json:",omitempty"`
	BootfsDatasets   string `json:",omitempty"`
}

// Zfs is a system handler talking to zfs linux module.
// It contains a local cache and dataset structures of underlying system.
type Zfs struct {
	// root is a virtual dataset to which all top dataset of all pools are attached
	root        *Dataset
	allDatasets map[string]*Dataset
}

// New returns a new zfs system handler.
func New(ctx context.Context, options ...func(*Zfs)) (*Zfs, error) {
	log.Debug(ctx, i18n.G("ZFS: new scan"))

	z := Zfs{
		root:        &Dataset{Name: "/"},
		allDatasets: make(map[string]*Dataset),
	}
	for _, options := range options {
		options(&z)
	}

	// scan all datasets that are currently imported on the system
	ds, err := libzfs.DatasetOpenAll()
	if err != nil {
		return nil, fmt.Errorf(i18n.G("can't list datasets: %v"), err)
	}

	var children []*Dataset
	for i := range ds {
		c, err := newDatasetTree(ctx, &ds[i], &z.allDatasets)
		if err != nil {
			return nil, fmt.Errorf("couldn't scan all datasets: %v", err)
		}
		if c == nil {
			continue
		}
		children = append(children, c)
	}
	z.root.children = children

	return &z, nil
}

// Datasets returns all datasets on the system, where parent will always be before children.
func (z Zfs) Datasets() []Dataset {
	ds := make(chan *Dataset)

	var collectChildren func(d *Dataset)
	collectChildren = func(d *Dataset) {
		if d != z.root {
			ds <- d
		}
		for _, n := range d.children {
			collectChildren(n)
		}
		if d == z.root {
			close(ds)
		}
	}
	go collectChildren(z.root)

	r := make([]Dataset, 0, len(z.allDatasets))
	for d := range ds {
		r = append(r, *d)
	}
	return r
}

// Transaction is a particular transaction on a Zfs state
type Transaction struct {
	*Zfs
	ctx    context.Context
	cancel context.CancelFunc
	done   chan struct{}

	reverts []func() error

	// lastNestedTransaction will help ensuring that it's fully done before reverting this parent one.
	lastNestedTransaction *Transaction
}

// nestedTransaction is an internal transaction on a Zfs state which has a parent.
// It can autorevert on Done(&err) what the nested transaction has processed.
type nestedTransaction struct {
	*Transaction

	parent *Transaction
}

// NoTransaction is a dummy container for no transaction on a ZFS system
type NoTransaction struct {
	*Zfs
	ctx context.Context
}

// NewNoTransaction creates a new Zfs handler where operations can't be cancelled, based on a ZFS object.
func (z *Zfs) NewNoTransaction(ctx context.Context) *NoTransaction {
	return &NoTransaction{Zfs: z, ctx: ctx}
}

// NewTransaction creates a new Zfs handler for a transaction, based on a Zfs object.
// It returns a cancelFunc that is automatically purged on <Transaction>.Done().
// If ctx is a cancellable or if CancelFunc is called before <Transaction>.Done(),
// the transaction will revert any in progress zfs changes.
func (z *Zfs) NewTransaction(ctx context.Context) (*Transaction, context.CancelFunc) {
	ctx, cancel := context.WithCancel(ctx)

	t := Transaction{
		Zfs:    z,
		ctx:    ctx,
		cancel: cancel,
		done:   make(chan struct{}),
	}

	go func() {
		<-ctx.Done()

		// check that any potential lastNestedTransaction has fully processed its reverted if it wasn't ended
		if t.lastNestedTransaction != nil {
			t.lastNestedTransaction.Done()
		}

		if len(t.reverts) > 0 {
			log.Debugf(t.ctx, i18n.G("ZFS: reverting all in progress zfs transactions"))
		}
		for i := len(t.reverts) - 1; i >= 0; i-- {
			if err := t.reverts[i](); err != nil {
				log.Warningf(t.ctx, i18n.G("An error occurred when reverting a Zfs transaction: ")+config.ErrorFormat, err)
			}
		}
		t.reverts = nil
		close(t.done)
	}()

	return &t, cancel
}

// findDatasetByName returns given dataset from path, handling special case of dataset
// not found in hashmap and virtual root dataset.
func (z *Zfs) findDatasetByName(path string) (*Dataset, error) {
	d, exists := z.allDatasets[path]
	if !exists {
		if path == "." {
			d = z.root
		} else {
			return nil, fmt.Errorf(i18n.G("couldn't find dataset %q in cache"), path)
		}
	}
	return d, nil
}

// Done signal that the transaction has ended and the object can't be reused.
// This should be called to release underlying resources.
func (t *Transaction) Done() {
	log.Debugf(t.ctx, i18n.G("ZFS: ending transaction"))
	defer func() { log.Debugf(t.ctx, i18n.G("ZFS: transaction done")) }()

	// If cancel() was called before Done(), ensure we have proceeded the revert functions.
	select {
	case <-t.ctx.Done():
		<-t.done
		return
	default:
	}

	t.reverts = nil
	t.cancel() // Purge ctx goroutine
	<-t.done
}

// registerRevert is a helper for defer() setting error value
func (t *Transaction) registerRevert(f func() error) {
	t.reverts = append(t.reverts, f)
}

// checkValid verifies if the transaction object is still valid and panics if not.
func (t *Transaction) checkValid() {
	select {
	case <-t.done:
		panic(i18n.G("The ZFS transaction object has already been used and Done() was called. It can't be reused"))
	default:
	}
}

// newNestedTransaction creates a sub transaction from an in progress transaction, reusing the parent transaction
// context.
// You should call Done(&err). If the given error it points at is not nil, it will cancel the nested transaction
// automatically
func (t *Transaction) newNestedTransaction() *nestedTransaction {
	nested, _ := t.Zfs.NewTransaction(t.ctx)
	t.lastNestedTransaction = nested
	return &nestedTransaction{
		Transaction: nested,
		parent:      t,
	}
}

// Done either commit a nested transaction or cancel it if an error occured
func (t *nestedTransaction) Done(err *error) {
	defer t.Transaction.Done()
	if *err != nil {
		// revert all in progress transactions
		log.Debugf(t.ctx, i18n.G("ZFS: an error occured: %v"), *err)
		log.Debugf(t.ctx, i18n.G("ZFS: Cancelling nested transaction"))
		t.cancel()
		return
	}
	// append to parents current in progress transactions
	t.parent.reverts = append(t.parent.reverts, t.reverts...)
}

// Create creates a dataset for that path.
func (t *Transaction) Create(path, mountpoint, canmount string) error {
	t.checkValid()

	log.Debugf(t.ctx, i18n.G("ZFS: trying to Create %q with mountpoint %q"), path, mountpoint)

	props := make(map[libzfs.Prop]libzfs.Property)
	if mountpoint != "" {
		props[libzfs.DatasetPropMountpoint] = libzfs.Property{Value: mountpoint}
	}
	props[libzfs.DatasetPropCanmount] = libzfs.Property{Value: canmount}

	dZFS, err := libzfs.DatasetCreate(path, libzfs.DatasetTypeFilesystem, props)
	if err != nil {
		return fmt.Errorf(i18n.G("can't create %q: %v"), path, err)
	}

	d := Dataset{
		Name:       path,
		IsSnapshot: false,
		dZFS:       &dZFS,
	}
	t.registerRevert(func() error {
		nt := t.Zfs.NewNoTransaction(t.ctx)
		if err := nt.Destroy(d.Name); err != nil {
			return fmt.Errorf(i18n.G("couldn't destroy %q for cleanup: %v"), d.Name, err)
		}
		return nil
	})
	if err := d.RefreshProperties(t.ctx, &dZFS); err != nil {
		return fmt.Errorf(i18n.G("couldn't fetch property of newly created dataset: %v"), err)
	}
	t.Zfs.allDatasets[d.Name] = &d

	parent, err := t.Zfs.findDatasetByName(filepath.Dir(d.Name))
	if err != nil {
		return fmt.Errorf(i18n.G("cannot find parent for %q: %v"), d.Name, err)
	}
	parent.children = append(parent.children, &d)

	return nil
}

// Snapshot creates a new snapshot for dataset (and children if recursive is true) with the given name.
func (t *Transaction) Snapshot(snapName, datasetName string, recursive bool) (errSnapshot error) {
	t.checkValid()

	log.Debugf(t.ctx, i18n.G("ZFS: trying to snapshot %q, recursive: %v"), datasetName, recursive)

	d, err := t.Zfs.findDatasetByName(datasetName)
	if err != nil {
		return fmt.Errorf(i18n.G("cannot find %q: %v"), datasetName, err)
	}

	nestedT := t.newNestedTransaction()
	defer nestedT.Done(&errSnapshot)

	// We can't use the recursive version of snapshotting, as we want to track user properties and
	// set them explicitly as needed
	return nestedT.snapshotRecursive(d, snapName, recursive)
}

// snapshotRecursive recursively try snapshotting all children and store "revert" operations by cleaning newly
// created datasets.
func (t *nestedTransaction) snapshotRecursive(parent *Dataset, snapName string, recursive bool) error {
	// Get properties from parent of snapshot.
	srcProps := parent.DatasetProp

	props := make(map[libzfs.Prop]libzfs.Property)

	dZFS, err := libzfs.DatasetSnapshot(parent.Name+"@"+snapName, false, props)
	if err != nil {
		return fmt.Errorf(i18n.G("couldn't snapshot %q: %v"), parent.Name, err)
	}

	d := Dataset{
		Name:       parent.Name + "@" + snapName,
		IsSnapshot: true,
		dZFS:       &dZFS,
	}
	t.registerRevert(func() error {
		nt := t.Zfs.NewNoTransaction(t.ctx)
		if err := nt.destroyOne(&d); err != nil {
			return fmt.Errorf(i18n.G("couldn't destroy %q for cleanup: %v"), d.Name, err)
		}
		return nil
	})

	// Set user properties that we couldn't set before creating the snapshot dataset.
	// We don't set LastUsed here as Creation time will be used.
	bootFS := "no"
	if srcProps.BootFS {
		bootFS = "yes"
	}
	userPropertiesToSet := map[string]string{
		SnapshotMountpointProp: srcProps.Mountpoint + ":" + srcProps.sources.Mountpoint,
		SnapshotCanmountProp:   srcProps.CanMount + ":" + srcProps.sources.CanMount,
		BootfsProp:             bootFS + ":" + srcProps.sources.BootFS,
		LastBootedKernelProp:   srcProps.LastBootedKernel + ":" + srcProps.sources.LastBootedKernel,
		BootfsDatasetsProp:     srcProps.BootfsDatasets + ":" + srcProps.sources.BootfsDatasets,
	}
	for prop, value := range userPropertiesToSet {
		// Only set values that are not empty on parent
		if strings.HasPrefix(value, ":") {
			continue
		}
		if err := dZFS.SetUserProperty(prop, value); err != nil {
			return fmt.Errorf(i18n.G("couldn't set user property %q to %q for %v: ")+config.ErrorFormat, prop, value, d.Name, err)
		}
	}

	if err := d.RefreshProperties(t.ctx, &dZFS); err != nil {
		return fmt.Errorf(i18n.G("couldn't fetch property of newly created snapshot: %v"), err)
	}
	t.Zfs.allDatasets[d.Name] = &d
	parent.children = append(parent.children, &d)

	if !recursive {
		return nil
	}

	for _, dc := range parent.children {
		if dc.IsSnapshot {
			continue
		}
		if err := t.snapshotRecursive(dc, snapName, recursive); err != nil {
			return fmt.Errorf(i18n.G("stop snapshotting dataset for %q: %v"), parent.Name, err)
		}
	}
	return nil
}

/*
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

	/* FIXME: this is taken from getDatasetProp().
	 * We will access directly the parent properties we are interested in here.
	 *

			parentName = name[:strings.LastIndex(name, "@")]
			p, ok := datasetPropertiesCache[parentName]
			if ok != true {
				return nil, fmt.Errorf(i18n.G("couldn't find %q in cache for getting properties of snapshot %q"), parentName, name)
			}
			mountpoint = p.Mountpoint
			sources.Mountpoint = p.sources.Mountpoint
			canMount = p.CanMount

*/
/*
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
}*/

// Destroy recursively all children, including dataset named "name".
// If the dataset is a snapshot, navigate through the hierarchy to delete all dataset with the same snapshot name.
// Note that destruction can't be rollbacked as filesystem content can't be recreated, so we don't accept them
// in a transactional Zfs element.
func (nt *NoTransaction) Destroy(name string) error {
	log.Debugf(nt.ctx, i18n.G("ZFS: request destruction of %q"), name)
	d, err := nt.Zfs.findDatasetByName(name)
	if err != nil {
		return fmt.Errorf(i18n.G("can't get dataset to destroy %q: ")+config.ErrorFormat, name, err)
	}

	if err := d.checkNoClone(); err != nil {
		return fmt.Errorf(i18n.G("couldn't destroy %q due to clones: %v"), name, err)
	}

	var parentName, snapName string
	target := d
	if d.IsSnapshot {
		parentName, snapName = splitSnapshotName(d.Name)
		target, err = nt.Zfs.findDatasetByName(parentName)
		if err != nil {
			return fmt.Errorf(i18n.G("cannot find parent for %q: %v"), d.Name, err)
		}
	}
	if err := nt.destroyRecursive(target, snapName); err != nil {
		return fmt.Errorf(i18n.G("couldn't destroy %q and its children: %v"), name, err)
	}

	return nil
}

// destroyRecursive destroys and unreference dataset objects, starting with children.
func (nt *NoTransaction) destroyRecursive(d *Dataset, snapName string) error {
	for _, dc := range d.children {
		if err := nt.destroyRecursive(dc, snapName); err != nil {
			return fmt.Errorf(i18n.G("stop destroying dataset on %q, cannot destroy child: %v"), d.Name, err)
		}
	}

	target := d
	var err error
	if snapName != "" {
		if target, err = nt.Zfs.findDatasetByName(d.Name + "@" + snapName); err != nil {
			log.Debugf(nt.ctx, i18n.G("no existing snapshot %q: ")+config.ErrorFormat, d.Name+"@"+snapName, err)
			return nil
		}
	}

	return nt.destroyOne(target)
}

// destroyOne destroys only given dataset. If it has children, those should be cleaned up first.
func (nt *NoTransaction) destroyOne(d *Dataset) error {
	log.Debugf(nt.ctx, i18n.G("ZFS: trying to destroy %q"), d.Name)

	// Destroy myself
	if err := d.dZFS.Destroy(false); err != nil {
		return fmt.Errorf(i18n.G("cannot destroy dataset %q: %v"), d.Name, err)
	}
	defer d.dZFS.Close()

	// Unattach from parent children
	parentName := filepath.Dir(d.Name)
	if d.IsSnapshot {
		parentName, _ = splitSnapshotName(d.Name)
	}
	parent, err := nt.Zfs.findDatasetByName(parentName)
	if err != nil {
		return fmt.Errorf(i18n.G("cannot find parent for %s: %v"), d.Name, err)
	}

	i := -1
	for idx, dc := range parent.children {
		if dc.Name == d.Name {
			i = idx
			break
		}
	}
	if i != -1 {
		parent.children = append(parent.children[:i], parent.children[i+1:]...)
	}

	// Delete from main list of dataset
	delete(nt.Zfs.allDatasets, d.Name)

	return nil
}

/*
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
*/
