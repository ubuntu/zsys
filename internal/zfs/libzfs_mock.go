package zfs

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	libzfs "github.com/bicomsystems/go-libzfs"
)

type libZFSMock struct {
	mu       sync.RWMutex
	datasets map[string]*dZFSMock
	pools    map[string]libzfs.Pool
}

func (l *libZFSMock) PoolOpen(name string) (pool libzfs.Pool, err error) {
	pool, ok := l.pools[name]
	if !ok {
		return pool, fmt.Errorf("No pool found %q", name)
	}
	return pool, nil
}

func (l *libZFSMock) PoolCreate(name string, vdev libzfs.VDevTree, features map[string]string, props libzfs.PoolProperties, fsprops libzfs.DatasetProperties) (pool libzfs.Pool, err error) {
	p := libzfs.Pool{
		Properties: make([]libzfs.Property, libzfs.PoolNumProps+1),
	}
	for i, prop := range props {
		p.Properties[i] = libzfs.Property{Value: prop}
	}
	l.pools[name] = p

	datasetProps := make(map[libzfs.Prop]libzfs.Property)
	for i, prop := range fsprops {
		datasetProps[i] = libzfs.Property{Value: prop}
	}
	l.DatasetCreate(name, libzfs.DatasetTypeFilesystem, datasetProps)

	return p, nil
}

func (l *libZFSMock) DatasetOpenAll() (datasets []dZFSInterface, err error) {
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

func (l *libZFSMock) DatasetOpen(name string) (dZFSInterface, error) {
	l.mu.RLock()
	d, ok := l.datasets[name]
	l.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("No dataset found with name %q", name)
	}

	l.openChildrenFor(d)
	return d, nil
}

func (l *libZFSMock) openChildrenFor(dm *dZFSMock) {
	name := dm.Dataset.Properties[libzfs.DatasetPropName].Value
	dm.children = nil
	dm.Dataset.Children = nil
	for k := range l.datasets {
		l.mu.RLock()
		d := l.datasets[k]
		l.mu.RUnlock()

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

func (l *libZFSMock) DatasetCreate(path string, dtype libzfs.DatasetType, props map[libzfs.Prop]libzfs.Property) (dZFSInterface, error) {
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
	if _, ok := l.pools[poolName]; !ok {
		return nil, fmt.Errorf("pool %q doesn't exists", poolName)
	}

	props[libzfs.DatasetPropCreation] = libzfs.Property{
		Value:  fmt.Sprintf("%d", time.Now().Unix()),
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
		cm = libzfs.Property{
			Value:  "on",
			Source: "default",
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
			// Overriden local property
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
		for _, k := range []string{BootfsProp, LastUsedProp, BootfsDatasetsProp, LastBootedKernelProp,
			CanmountProp, SnapshotCanmountProp, MountPointProp, SnapshotMountpointProp} {
			if _, ok := parent.userProperties[k]; ok {
				p := parent.userProperties[k]
				if p.Source == "local" {
					p.Source = "inherited"
				}
				userProperties[k] = p
			}
		}
	}

	d := dZFSMock{
		Dataset: &libzfs.Dataset{
			Type:       dtype,
			Properties: props,
		},
		libZFSMock:     l,
		userProperties: userProperties,
	}
	if hasParent {
		var found bool
		for _, c := range parent.children {
			if c == &d {
				found = true
				break
			}
		}
		if !found {
			parent.children = append(parent.children, &d)
		}
	}

	l.mu.Lock()
	l.datasets[path] = &d
	l.mu.Unlock()

	return &d, nil
}

func (l *libZFSMock) DatasetSnapshot(path string, recur bool, props map[libzfs.Prop]libzfs.Property) (dZFSInterface, error) {
	if strings.Split(path, "@")[1] == "" {
		return nil, fmt.Errorf("%q is not a valid snapshot name", path)
	}
	return l.createSnapshot(path, recur, props)
}

func (l *libZFSMock) createSnapshot(path string, recur bool, props map[libzfs.Prop]libzfs.Property) (dZFSInterface, error) {
	dinterface, err := l.DatasetCreate(path, libzfs.DatasetTypeSnapshot, props)
	if err != nil {
		return nil, err
	}

	d := dinterface.(*dZFSMock)

	if !recur {
		return d, nil
	}
	snapshotName := strings.Split(path, "@")[1]
	for _, c := range d.children {
		if c.IsSnapshot() {
			continue
		}

		childPath := c.Dataset.Properties[libzfs.DatasetPropName].Value + "@" + snapshotName
		_, err := l.createSnapshot(childPath, recur, props)
		if err != nil {
			return nil, err
		}
	}
	return d, nil
}

type dZFSMock struct {
	*libzfs.Dataset
	children       []*dZFSMock
	libZFSMock     *libZFSMock
	userProperties map[string]libzfs.Property
	isClosed       bool
	tempOrigin     string
}

func (d dZFSMock) assertDatasetOpened() {
	if d.isClosed {
		panic(fmt.Sprintf("operation on closed dataset %q is prohibited", d.Dataset.Properties[libzfs.DatasetPropName].Value))
	}
}
func (d dZFSMock) Children() (children []dZFSInterface) {
	d.assertDatasetOpened()
	var r []dZFSInterface
	for i := range d.children {
		r = append(r, d.children[i])
	}
	return r
}

func (d dZFSMock) dZFSChildren() *[]libzfs.Dataset {
	return &d.Dataset.Children
}

func (d dZFSMock) Properties() *map[libzfs.Prop]libzfs.Property {
	d.assertDatasetOpened()
	return &d.Dataset.Properties
}

func (d dZFSMock) Type() libzfs.DatasetType {
	d.assertDatasetOpened()
	return d.Dataset.Type
}

func (d dZFSMock) Clone(target string, props map[libzfs.Prop]libzfs.Property) (dZFSInterface, error) {
	d.assertDatasetOpened()
	props[libzfs.DatasetPropOrigin] = libzfs.Property{
		Value:  d.Dataset.Properties[libzfs.DatasetPropName].Value,
		Source: "-",
	}
	dinterface, err := d.libZFSMock.DatasetCreate(target, libzfs.DatasetTypeFilesystem, props)
	if err != nil {
		return nil, err
	}

	di := dinterface.(*dZFSMock)
	return di, nil
}

func (d dZFSMock) Pool() (p libzfs.Pool, err error) {
	d.assertDatasetOpened()
	name := d.Dataset.Properties[libzfs.DatasetPropName].Value
	poolName := strings.Split(name, "/")[0]
	p, ok := d.libZFSMock.pools[poolName]
	if !ok {
		return libzfs.Pool{}, fmt.Errorf("No pool found for dataset %q", name)
	}

	return p, nil
}

func (d dZFSMock) GetUserProperty(p string) (prop libzfs.Property, err error) {
	d.assertDatasetOpened()
	prop, ok := d.userProperties[p]
	if !ok {
		return libzfs.Property{Value: "-", Source: "-"}, nil
	}
	return prop, nil
}

func (d *dZFSMock) SetUserProperty(prop, value string) error {
	d.assertDatasetOpened()
	return d.setUserPropertyWithSource(prop, value, "local")
}

func (d *dZFSMock) setUserPropertyWithSource(prop, value, source string) error {
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

func (d *dZFSMock) SetProperty(p libzfs.Prop, value string) error {
	d.assertDatasetOpened()
	return d.setPropertyWithSource(p, value, "local")
}

func (d *dZFSMock) setPropertyWithSource(p libzfs.Prop, value, source string) error {
	d.Dataset.Properties[p] = libzfs.Property{Value: value, Source: source}

	// This property doesn't propagate to children
	if p == libzfs.DatasetPropMounted {
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

func (d *dZFSMock) Destroy(Defer bool) (err error) {
	d.assertDatasetOpened()

	d.libZFSMock.mu.Lock()
	defer d.libZFSMock.mu.Unlock()
	delete(d.libZFSMock.datasets, d.Dataset.Properties[libzfs.DatasetPropName].Value)
	return nil
}

func (d *dZFSMock) Clones() (clones []string, err error) {
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

func (d *dZFSMock) Promote() (err error) {
	d.assertDatasetOpened()

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
	var snapshotsToMigrate []*dZFSMock
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

	for _, snap := range snapshotsToMigrate {
		// Create new snapshots for every snapshots to migrate
		oldDatasetName := snap.Dataset.Properties[libzfs.DatasetPropName].Value
		newName := datasetName + "@" + strings.Split(oldDatasetName, "@")[1]

		// Pass a copy of properties to not alter - soon deleted - old snapshot on previoulsy promoted dataset
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
		newD.(*dZFSMock).userProperties = newDUserProps

		// Old promoted dataset should now point to new one
		if snap == origSnapshot {
			d.libZFSMock.datasets[strings.Split(oldDatasetName, "@")[0]].tempOrigin = newName
		}

		// All datasets pointing to those snapshots to Migrate should point to new snapshots
		d.libZFSMock.mu.Lock()
		for _, ds := range d.libZFSMock.datasets {
			if ds.IsSnapshot() {
				continue
			}
			if ds.Dataset.Properties[libzfs.DatasetPropOrigin].Value == oldDatasetName {
				ds.tempOrigin = newName
			}
		}
		d.libZFSMock.mu.Unlock()

		// Mark this snapshot to be deleted on next DatasetOpenAll. The libzfs lib keep them attached to Children
		d.libZFSMock.mu.Lock()
		delete(d.libZFSMock.datasets, oldDatasetName)
		d.libZFSMock.mu.Unlock()
	}

	// Reset promoted snapshot (real is calling ReloadProperties right away on d only)
	d.Dataset.Properties[libzfs.DatasetPropOrigin] = libzfs.Property{Source: "-"}

	return nil
}

// ReloadProperties: set orig to new thing
// This is to mock libZFS only reloading the orig property at this time
func (d *dZFSMock) ReloadProperties() (err error) {
	if d.tempOrigin != "" {
		d.Dataset.Properties[libzfs.DatasetPropOrigin] = libzfs.Property{
			Value:  d.tempOrigin,
			Source: "-",
		}
	}
	return nil
}