package mock

import (
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ubuntu/zsys/internal/zfs/libzfs"
)

const currentMagicTime = "2000000000"

// LibZFS is the mock, in memory implementation of libzfs
type LibZFS struct {
	mu       sync.RWMutex
	datasets map[string]*dZFS
	pools    map[string]libzfs.Pool

	errOnCreate       bool
	errOnClone        bool
	errOnDestroy      bool
	errOnPromote      bool
	errOnScan         bool
	errOnSetProperty  bool
	forceLastUsedTime bool
}

// PoolOpen opens given pool
func (l *LibZFS) PoolOpen(name string) (pool libzfs.Pool, err error) {
	pool, ok := l.pools[name]
	if !ok {
		return pool, fmt.Errorf("No pool found %q", name)
	}
	return pool, nil
}

// PoolCreate creates a zfs pool
func (l *LibZFS) PoolCreate(name string, vdev libzfs.VDevTree, features map[string]string, props libzfs.PoolProperties, fsprops libzfs.DatasetProperties) (pool libzfs.Pool, err error) {
	p := libzfs.Pool{
		Properties: make([]libzfs.Property, libzfs.PoolNumProps+1),
	}
	p.Properties[libzfs.PoolPropCapacity] = libzfs.Property{Value: "30"}
	for i, prop := range props {
		p.Properties[i] = libzfs.Property{Value: prop}
	}
	l.mu.Lock()
	l.pools[name] = p
	l.mu.Unlock()

	datasetProps := make(map[libzfs.Prop]libzfs.Property)
	for i, prop := range fsprops {
		datasetProps[i] = libzfs.Property{Value: prop}
	}
	l.DatasetCreate(name, libzfs.DatasetTypeFilesystem, datasetProps)

	return p, nil
}

// DatasetOpenAll opens all the dataset recursively
func (l *LibZFS) DatasetOpenAll() (datasets []libzfs.DZFSInterface, err error) {
	if l.errOnScan {
		return nil, errors.New("Error on DatasetOpenAll requested")
	}

	// This is the only place where we can clean the global datasets from datasets to remove as libzfs doesn't do that right on Promote.
	// zfs.New() is calling DatasetOpenAll to load the whole new state from zfs kernel state.
	for n := range l.pools {
		d, err := l.DatasetOpen(n)
		if err != nil {
			return nil, fmt.Errorf("cannot open datasets for pool %q", n)
		}
		datasets = append(datasets, d)
	}
	return datasets, nil
}

// DatasetOpen opens a dataset
func (l *LibZFS) DatasetOpen(name string) (libzfs.DZFSInterface, error) {
	l.mu.RLock()
	d, ok := l.datasets[name]
	l.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("No dataset found with name %q", name)
	}

	l.openChildrenFor(d)
	return d, nil
}

func (l *LibZFS) openChildrenFor(dm *dZFS) {
	name := dm.Dataset.Properties[libzfs.DatasetPropName].Value
	dm.children = nil
	dm.Dataset.Children = nil
	l.mu.RLock()
	defer l.mu.RUnlock()
	for k := range l.datasets {
		d := l.datasets[k]

		/* Only consider potential children
		   Retrieve direct children name from the dataset name with 2 cases to handle for a dataset and a snapshot
		   eg:
			 dataset name: rpool/ROOT/ubuntu
			 rpool/ROOT/ubuntu/var -> var
			 rpool/ROOT/ubuntu@snap1 -> @snap1

			We skip rpool/ROOT/ubuntu as child of rpool/ROOT/ubuntu
		*/
		isSnapshotDesc := strings.Contains(k, "@") && strings.HasPrefix(k, name+"@")
		isDatasetDesc := !strings.Contains(k, "@") && strings.HasPrefix(k, name+"/")
		if (!isSnapshotDesc && !isDatasetDesc) || dm == d {
			continue
		}

		if !isSnapshotDesc && strings.Contains(strings.TrimPrefix(k, name+"/"), "/") {
			continue
		}
		dm.children = append(dm.children, d)
		dm.Dataset.Children = append(dm.Dataset.Children, *d.Dataset)
		l.openChildrenFor(d)
	}
}

// DatasetCreate creates a dataset
func (l *LibZFS) DatasetCreate(path string, dtype libzfs.DatasetType, props map[libzfs.Prop]libzfs.Property) (libzfs.DZFSInterface, error) {
	if l.errOnCreate {
		return nil, errors.New("Error on Create requested")
	}
	l.mu.Lock()
	if _, ok := l.datasets[path]; ok {
		l.mu.Unlock()
		return nil, fmt.Errorf("dataset %q already exists", path)
	}
	l.mu.Unlock()
	poolName := strings.Split(path, "/")[0]
	// snapshot on root dataset
	if !strings.Contains(path, "/") && strings.Contains(path, "@") {
		poolName = strings.Split(path, "@")[0]
	}
	l.mu.RLock()
	if _, ok := l.pools[poolName]; !ok {
		return nil, fmt.Errorf("pool %q doesn't exists", poolName)
	}
	l.mu.RUnlock()

	ctime := fmt.Sprintf("%d", time.Now().Unix())
	if t, ok := props[libzfs.DatasetPropCreation]; ok {
		ctime = t.Value
	}
	props[libzfs.DatasetPropCreation] = libzfs.Property{
		Value:  ctime,
		Source: "none",
	}
	props[libzfs.DatasetPropName] = libzfs.Property{Value: path}
	userProperties := make(map[string]libzfs.Property)

	if mp, ok := props[libzfs.DatasetPropMountpoint]; ok {
		mp.Source = "local"
		props[libzfs.DatasetPropMountpoint] = mp
	}

	cm, ok := props[libzfs.DatasetPropCanmount]
	if ok {
		cm.Source = "local"
	} else {
		var v, s string
		if dtype == libzfs.DatasetTypeFilesystem {
			v = "on"
			s = "default"
		}

		cm = libzfs.Property{
			Value:  v,
			Source: s,
		}
	}
	props[libzfs.DatasetPropCanmount] = cm

	parentName := filepath.Dir(path)
	if dtype == libzfs.DatasetTypeSnapshot {
		parentName = strings.Split(path, "@")[0]
	}

	// Copy all the parent properties to the dataset and set them to local to override the parent
	l.mu.RLock()
	parent, hasParent := l.datasets[parentName]
	l.mu.RUnlock()
	if hasParent {
		// ZFS Properties
		pprops := parent.Dataset.Properties
		for k, pp := range pprops {
			// Overridden local property
			if _, ok := props[k]; ok {
				p := props[k]
				if p.Source == "" {
					p.Source = "local"
				}
				props[k] = p
				continue
			}
			// Read only properties are not inherited
			if pp.Source == "-" {
				continue
			}
			if pp.Source == "local" {
				pp.Source = "inherited"
			}
			// Transform mountpoint
			if k == libzfs.DatasetPropMountpoint {
				pp.Value = filepath.Join(pprops[k].Value, filepath.Base(path))
			}
			props[k] = pp
		}

		// User properties (can only be from parent at creation time)
		for _, k := range []string{libzfs.BootfsProp, libzfs.LastUsedProp, libzfs.BootfsDatasetsProp, libzfs.LastBootedKernelProp,
			libzfs.CanmountProp, libzfs.SnapshotCanmountProp, libzfs.MountPointProp, libzfs.SnapshotMountpointProp} {
			if _, ok := parent.userProperties[k]; ok {
				p := parent.userProperties[k]
				if p.Source == "local" {
					p.Source = "inherited"
				}
				userProperties[k] = p
			}
		}
	} else {
		if _, ok := props[libzfs.DatasetPropMountpoint]; !ok {
			props[libzfs.DatasetPropMountpoint] = libzfs.Property{
				Value:  "/" + path,
				Source: "default",
			}
		}
	}

	d := dZFS{
		Dataset: &libzfs.Dataset{
			Type:       dtype,
			Properties: props,
		},
		libZFSMock:     l,
		userProperties: userProperties,
	}
	if hasParent {
		var found bool
		l.mu.Lock()
		pc := make([]*dZFS, len(parent.children))
		copy(pc, parent.children)
		for _, c := range pc {
			if c == &d {
				found = true
				break
			}
		}
		if !found {
			parent.children = append(parent.children, &d)
		}
		l.mu.Unlock()
	}

	l.mu.Lock()
	l.datasets[path] = &d
	l.mu.Unlock()

	return &d, nil
}

// DatasetSnapshot creates a snapshot
func (l *LibZFS) DatasetSnapshot(path string, recur bool, props map[libzfs.Prop]libzfs.Property, userProps map[string]string) (libzfs.DZFSInterface, error) {
	if len(strings.Split(path, "@")) != 2 || strings.Split(path, "@")[1] == "" {
		return nil, fmt.Errorf("%q is not a valid snapshot name", path)
	}
	return l.createSnapshot(path, recur, props, userProps)
}

func (l *LibZFS) createSnapshot(path string, recur bool, props map[libzfs.Prop]libzfs.Property, userProps map[string]string) (libzfs.DZFSInterface, error) {
	if l.forceLastUsedTime {
		props[libzfs.DatasetPropCreation] = libzfs.Property{Value: currentMagicTime}
	}

	dinterface, err := l.DatasetCreate(path, libzfs.DatasetTypeSnapshot, props)
	if err != nil {
		return nil, err
	}

	d := dinterface.(*dZFS)
	for k, v := range userProps {
		if err := d.SetUserProperty(k, v); err != nil {
			return nil, err
		}
	}

	if !recur {
		return d, nil
	}
	snapshotName := strings.Split(path, "@")[1]
	for _, c := range d.children {
		if c.IsSnapshot() {
			continue
		}

		childPath := c.Dataset.Properties[libzfs.DatasetPropName].Value + "@" + snapshotName
		_, err := l.createSnapshot(childPath, recur, props, userProps)
		if err != nil {
			return nil, err
		}
	}
	return d, nil
}

// SetDatasetAsMounted is a test-only property allowing forcing one dataset to be mounted
func (l *LibZFS) SetDatasetAsMounted(name string, mounted bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	d := l.datasets[name]
	m := "no"
	if mounted {
		m = "yes"
	}
	d.setPropertyWithSource(libzfs.DatasetPropMounted, m, "")
}

// SetPoolCapacity allows forcing a capabity value on a pool
func (l *LibZFS) SetPoolCapacity(name, cap string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.pools[name].Properties[libzfs.PoolPropCapacity] = libzfs.Property{Value: cap}
}

// ErrOnPromote forces a failure of the mock on clone operation
func (l *LibZFS) ErrOnPromote(shouldErr bool) {
	l.errOnPromote = shouldErr
}

// ErrOnClone forces a failure of the mock on clone operation
func (l *LibZFS) ErrOnClone(shouldErr bool) {
	l.errOnClone = shouldErr
}

// ErrOnScan forces a failure of the mock on scan operation
func (l *LibZFS) ErrOnScan(shouldErr bool) {
	l.errOnScan = shouldErr
}

// ErrOnSetProperty forces a failure of the mock on set property operation
func (l *LibZFS) ErrOnSetProperty(shouldErr bool) {
	l.errOnSetProperty = shouldErr
}

// ErrOnCreate forces a failure of the mock on create operation
func (l *LibZFS) ErrOnCreate(shouldErr bool) {
	l.errOnCreate = shouldErr
}

// ErrOnDestroy forces a failure of the mock on destroy operation
func (l *LibZFS) ErrOnDestroy(shouldErr bool) {
	l.errOnDestroy = shouldErr
}

// ForceLastUsedTime ensures that any LastUsed property is set to the magic time for reproducibility
func (l *LibZFS) ForceLastUsedTime(force bool) {
	l.forceLastUsedTime = force
}

// GenerateID returns from a given length a random string (known in advanced if libzfs mock is used)
func (*LibZFS) GenerateID(length int) string {
	return strings.Repeat("x", length)
}

type dZFS struct {
	*libzfs.Dataset
	children       []*dZFS
	libZFSMock     *LibZFS
	userProperties map[string]libzfs.Property
	isClosed       bool
	tempOrigin     string
}

func (d dZFS) assertDatasetOpened() {
	if d.isClosed {
		panic(fmt.Sprintf("operation on closed dataset %q is prohibited", d.Dataset.Properties[libzfs.DatasetPropName].Value))
	}
}
func (d dZFS) Children() (children []libzfs.DZFSInterface) {
	d.assertDatasetOpened()
	var r []libzfs.DZFSInterface
	for i := range d.children {
		r = append(r, d.children[i])
	}
	return r
}

func (d dZFS) DZFSChildren() *[]libzfs.Dataset {
	return &d.Dataset.Children
}

func (d dZFS) Properties() *map[libzfs.Prop]libzfs.Property {
	d.assertDatasetOpened()
	return &d.Dataset.Properties
}

func (d dZFS) Type() libzfs.DatasetType {
	d.assertDatasetOpened()
	return d.Dataset.Type
}

func (d dZFS) Clone(target string, props map[libzfs.Prop]libzfs.Property) (libzfs.DZFSInterface, error) {
	d.assertDatasetOpened()
	if d.libZFSMock.errOnClone {
		return nil, errors.New("Error on Clone requested")
	}
	props[libzfs.DatasetPropOrigin] = libzfs.Property{
		Value:  d.Dataset.Properties[libzfs.DatasetPropName].Value,
		Source: "-",
	}

	dinterface, err := d.libZFSMock.DatasetCreate(target, libzfs.DatasetTypeFilesystem, props)
	if err != nil {
		return nil, err
	}

	di := dinterface.(*dZFS)
	return di, nil
}

func (d dZFS) Pool() (p libzfs.Pool, err error) {
	d.assertDatasetOpened()
	name := d.Dataset.Properties[libzfs.DatasetPropName].Value
	poolName := strings.Split(name, "/")[0]
	p, ok := d.libZFSMock.pools[poolName]
	if !ok {
		return libzfs.Pool{}, fmt.Errorf("No pool found for dataset %q", name)
	}

	return p, nil
}

func (d dZFS) GetUserProperty(p string) (prop libzfs.Property, err error) {
	d.assertDatasetOpened()
	prop, ok := d.userProperties[p]
	if !ok {
		return libzfs.Property{Value: "-", Source: "-"}, nil
	}
	return prop, nil
}

func (d *dZFS) SetUserProperty(prop, value string) error {
	if d.libZFSMock.errOnSetProperty {
		return errors.New("Error on SetProperty requested")
	}
	d.assertDatasetOpened()

	if d.libZFSMock.forceLastUsedTime && prop == libzfs.LastUsedProp {
		value = currentMagicTime
	}

	return d.setUserPropertyWithSource(prop, value, "local")
}

func (d *dZFS) setUserPropertyWithSource(prop, value, source string) error {
	d.userProperties[prop] = libzfs.Property{Value: value, Source: source}
	// refresh children
	for _, c := range d.children {
		if c.userProperties[prop].Source == "local" {
			continue
		}
		if err := c.setUserPropertyWithSource(prop, value, "inherited"); err != nil {
			return err
		}
	}
	return nil
}

func (d *dZFS) SetProperty(p libzfs.Prop, value string) error {
	if d.libZFSMock.errOnSetProperty {
		return errors.New("Error on SetProperty requested")
	}
	d.assertDatasetOpened()
	return d.setPropertyWithSource(p, value, "local")
}

func (d *dZFS) setPropertyWithSource(p libzfs.Prop, value, source string) error {
	// Those properties don't propagate to children
	if p == libzfs.DatasetPropMounted || p == libzfs.DatasetPropOrigin {
		source = "-"
	}

	d.Dataset.Properties[p] = libzfs.Property{Value: value, Source: source}

	if source == "-" {
		return nil
	}

	// refresh children
	for i := range d.children {
		c := d.children[i]
		src := c.Dataset.Properties[p].Source
		if src == "local" || src == "default" || src == "none" {
			continue
		}

		v := value
		if p == libzfs.DatasetPropMountpoint {
			v = filepath.Join(value, strings.TrimPrefix(strings.Split(c.Dataset.Properties[libzfs.DatasetPropName].Value, "@")[0], d.Dataset.Properties[libzfs.DatasetPropName].Value))
		}

		if err := c.setPropertyWithSource(p, v, "inherited"); err != nil {
			return err
		}
		d.children[i] = c
	}
	return nil
}

func (d *dZFS) Destroy(Defer bool) (err error) {
	d.assertDatasetOpened()
	n := d.Dataset.Properties[libzfs.DatasetPropName].Value

	if d.libZFSMock.errOnDestroy {
		return errors.New("Error on Destroy requested")
	}

	d.libZFSMock.mu.Lock()
	defer d.libZFSMock.mu.Unlock()
	for name, dataset := range d.libZFSMock.datasets {
		if n == name {
			continue
		}
		if strings.HasPrefix(name, n+"/") || strings.HasPrefix(name, n+"@") {
			return fmt.Errorf("can't remove %s: it has at least one child: %s", n, name)
		}
		if dataset.Dataset.Properties[libzfs.DatasetPropOrigin].Value == n {
			return fmt.Errorf("can't remove %s: it has at least one clone: %s", n, name)
		}
	}
	delete(d.libZFSMock.datasets, n)
	return nil
}

func (d *dZFS) Clones() (clones []string, err error) {
	d.assertDatasetOpened()
	d.libZFSMock.mu.Lock()
	defer d.libZFSMock.mu.Unlock()

	for _, c := range d.children {
		if !c.IsSnapshot() {
			continue
		}
		name := c.Dataset.Properties[libzfs.DatasetPropName].Value
		for cloneName, clone := range d.libZFSMock.datasets {
			if clone.Dataset.Properties[libzfs.DatasetPropOrigin].Value != name {
				continue
			}
			clones = append(clones, cloneName)
		}
	}
	return clones, nil
}

func (d *dZFS) Promote() (err error) {
	d.assertDatasetOpened()
	if d.libZFSMock.errOnPromote {
		return errors.New("Error on Promote requested")
	}

	datasetName := d.Dataset.Properties[libzfs.DatasetPropName].Value
	origin := d.Dataset.Properties[libzfs.DatasetPropOrigin].Value
	if origin == "" {
		return nil
	}

	d.libZFSMock.mu.Lock()
	origSnapshot := d.libZFSMock.datasets[origin]
	d.libZFSMock.mu.Unlock()

	origSnapshotCreation, err := strconv.Atoi(origSnapshot.Dataset.Properties[libzfs.DatasetPropCreation].Value)
	if err != nil {
		return fmt.Errorf("cannot convert date to int for %q", origin)
	}
	// Collect snapshots to migrate
	var snapshotsToMigrate []*dZFS
	d.libZFSMock.mu.Lock()
	for name, ds := range d.libZFSMock.datasets {
		if !strings.HasPrefix(name, strings.Split(origin, "@")[0]+"@") {
			continue
		}

		dsCreation, err := strconv.Atoi(ds.Dataset.Properties[libzfs.DatasetPropCreation].Value)
		if err != nil {
			return fmt.Errorf("cannot convert date to int for %q", name)
		}
		if dsCreation > origSnapshotCreation {
			continue
		}
		snapshotsToMigrate = append(snapshotsToMigrate, ds)
	}
	d.libZFSMock.mu.Unlock()

	var newOrig string
	for _, snap := range snapshotsToMigrate {
		// Create new snapshots for every snapshots to migrate
		oldDatasetName := snap.Dataset.Properties[libzfs.DatasetPropName].Value
		newName := datasetName + "@" + strings.Split(oldDatasetName, "@")[1]

		// Pass a copy of properties to not alter - soon deleted - old snapshot on previously promoted dataset
		newDProps := make(map[libzfs.Prop]libzfs.Property)
		for k, v := range snap.Dataset.Properties {
			newDProps[k] = v
		}
		newD, err := d.libZFSMock.DatasetCreate(newName, libzfs.DatasetTypeSnapshot, newDProps)
		if err != nil {
			return err
		}
		newDUserProps := make(map[string]libzfs.Property)
		for k, v := range snap.userProperties {
			newDUserProps[k] = v
		}
		newD.(*dZFS).userProperties = newDUserProps

		// Old promoted dataset should now point to new one
		if snap == origSnapshot {
			newOrig = (*d.libZFSMock.datasets[strings.Split(oldDatasetName, "@")[0]].Properties())[libzfs.DatasetPropOrigin].Value
			d.libZFSMock.datasets[strings.Split(oldDatasetName, "@")[0]].tempOrigin = newName
		}

		// All datasets pointing to those snapshots to Migrate should point to new snapshots
		d.libZFSMock.mu.Lock()
		for dName, ds := range d.libZFSMock.datasets {
			if ds.IsSnapshot() {
				continue
			}
			if ds.Dataset.Properties[libzfs.DatasetPropOrigin].Value != oldDatasetName || strings.HasPrefix(newName, dName+"@") {
				continue
			}
			ds.tempOrigin = newName
		}
		d.libZFSMock.mu.Unlock()

		// Mark this snapshot to be deleted on next DatasetOpenAll. The libzfs lib keep them attached to Children
		d.libZFSMock.mu.Lock()
		delete(d.libZFSMock.datasets, oldDatasetName)
		d.libZFSMock.mu.Unlock()
	}

	// Reset promoted snapshot. This simulates ReloadProperties called only on this dataset in libzfz
	d.Dataset.Properties[libzfs.DatasetPropOrigin] = libzfs.Property{
		Value:  newOrig,
		Source: "-",
	}
	return nil
}

// ReloadProperties: set orig to new thing
// This is to mock libZFS only reloading the orig property at this time
func (d *dZFS) ReloadProperties() (err error) {
	if d.tempOrigin != "" {
		d.Dataset.Properties[libzfs.DatasetPropOrigin] = libzfs.Property{
			Value:  d.tempOrigin,
			Source: "-",
		}
	}
	return nil
}

// New returns a initialized LibZFS mock object
func New() LibZFS {
	return LibZFS{
		datasets: make(map[string]*dZFS),
		pools:    make(map[string]libzfs.Pool),
	}
}
