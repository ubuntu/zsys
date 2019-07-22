package machines_test

import (
	"strings"

	"github.com/ubuntu/zsys/internal/zfs"
	"golang.org/x/xerrors"
)

type zfsMock struct {
	d     []*zfs.Dataset
	nextD []*zfs.Dataset

	predictableSuffixFor string

	cloneErr   bool
	scanErr    bool
	setPropErr bool
	promoteErr bool
}

// NewZfsMock creates a new in memory with mock zfs datasets. We can simulate mounted dataset, and store predictableSuffixFor
// to change every new created datasets names.
func NewZfsMock(ds []zfs.Dataset, mountedDataset, predictableSuffixFor string, cloneErr, scanErr, setPropErr, promoteErr bool) (z *zfsMock) {
	var datasets []*zfs.Dataset
	for _, d := range ds {
		// copy value to take address
		d := d
		if d.Name == mountedDataset {
			d.Mounted = true
		}
		datasets = append(datasets, &d)
	}
	return &zfsMock{
		d:                    datasets,
		predictableSuffixFor: predictableSuffixFor,
		cloneErr:             cloneErr,
		scanErr:              scanErr,
		setPropErr:           setPropErr,
		promoteErr:           promoteErr,
	}
}

// Clone behaves like zfs.Clone, but is a in memory version with mock zfs datasets
func (z *zfsMock) Clone(name, suffix string, skipBootfs, recursive bool) (errClone error) {
	if z.cloneErr {
		return xerrors.New("Mock zfs raised an error on Clone")
	}

	datasets := z.nextD
	if datasets == nil {
		// create a new independent baking array to not modify z.d
		datasets = append([]*zfs.Dataset(nil), z.d...)
	}

	rootName, snapshot := splitSnapshotName(name)
	if snapshot == "" {
		return xerrors.Errorf("should clone a snapshot, got %q", name)
	}

	// Reformat the name with the new uuid and clone now the dataset.
	newRootName := rootName
	suffixIndex := strings.LastIndex(newRootName, "_")
	if suffixIndex != -1 {
		newRootName = newRootName[:suffixIndex]
	}
	newRootName += "_" + suffix

	var nextDatasets []*zfs.Dataset
	for _, d := range datasets {
		nextDatasets = append(nextDatasets, d)

		if !(d.Name == name || (strings.HasPrefix(d.Name, rootName+"/") && strings.HasSuffix(d.Name, "@"+snapshot))) ||
			(skipBootfs && d.BootFS) {
			continue
		}

		cloneName := newRootName + strings.TrimPrefix(d.Name, rootName)
		cloneName = strings.TrimSuffix(cloneName, "@"+snapshot)

		cm := d.CanMount
		if cm == "on" {
			cm = "noauto"
		}
		clone := zfs.Dataset{
			Name: cloneName,
			DatasetProp: zfs.DatasetProp{
				Mountpoint: d.Mountpoint,
				CanMount:   cm,
				BootFS:     d.BootFS,
				Origin:     d.Name,
			},
		}
		nextDatasets = append(nextDatasets, &clone)
	}

	z.nextD = nextDatasets

	return nil
}

// Scan behaves like zfs.Scan, but is a in memory version with mock zfs datasets
func (z zfsMock) Scan() ([]zfs.Dataset, error) {
	if z.scanErr {
		return nil, xerrors.New("Mock zfs raised an error on Scan")
	}

	// "Scan" zfs datasets by switch to the next prepared state
	if z.nextD != nil {
		z.d = z.nextD
		z.nextD = nil
	}

	var datasets []zfs.Dataset
	for _, d := range z.d {
		// Make predictable generated suffix for storing in golden files
		// We base on the given name + never used dataset (freshly created), as all clones and snapshots
		// will share the same prefix
		if strings.HasPrefix(d.Name, z.predictableSuffixFor+"_") && d.LastUsed == 0 {
			suffix := strings.TrimPrefix(d.Name, z.predictableSuffixFor+"_")
			if i := strings.Index(suffix, "/"); i != -1 {
				suffix = suffix[i:]
			} else if i := strings.Index(suffix, "@"); i != -1 {
				suffix = suffix[i:]
			} else {
				suffix = ""
			}
			d.Name = z.predictableSuffixFor + "_xxxxxx" + suffix
		}

		datasets = append(datasets, *d)
	}
	return datasets, nil
}

// SetProperty behaves like zfs.SetProperty, but is a in memory version with mock zfs datasets
func (z *zfsMock) SetProperty(name, value, datasetName string, force bool) error {
	if z.setPropErr {
		return xerrors.New("Mock zfs raised an error on SetProperty")
	}

	datasets := z.nextD
	if datasets == nil {
		// create a new independent baking array to not modify z.d
		datasets = append([]*zfs.Dataset(nil), z.d...)
	}
	for _, d := range datasets {
		if d.Name != datasetName {
			continue
		}
		switch name {
		case zfs.CanmountProp:
			if (d.CanMount == "on" && value == "noauto") || (d.CanMount == "noauto" && value == "on") {
				d.CanMount = value
				// If we have any snapshots for this dataset, applies it to them too
				for _, dSnap := range datasets {
					if strings.HasPrefix(dSnap.Name, d.Name+"@") {
						dSnap.CanMount = value
					}
				}
			}
		case zfs.BootfsDatasetsProp:
			d.BootfsDatasets = value
			// If we have any snapshots for this dataset, applies it to them too
			for _, dSnap := range datasets {
				if strings.HasPrefix(dSnap.Name, d.Name+"@") {
					dSnap.BootfsDatasets = value
				}
			}
			// If we have any children for this dataset, applies it to them too
			for _, dSnap := range datasets {
				if strings.HasPrefix(dSnap.Name, d.Name+"/") {
					dSnap.BootfsDatasets = value
				}
			}
		case zfs.LastUsedProp:
			const currentMagicTime = 2000000000

			d.LastUsed = currentMagicTime
			// If we have any children for this dataset, applies it to them too
			for _, dSnap := range datasets {
				if strings.HasPrefix(dSnap.Name, d.Name+"/") {
					dSnap.LastUsed = currentMagicTime
				}
			}
		}
	}
	z.nextD = datasets

	return nil
}

// Promote behaves like zfs.Promote, but is a in memory version with mock zfs datasets
// This isn't the exact same as promotion which will switch snapshots to opposite tree (if they were done
// before the targeted clone time creation), but we don't really care of it in the mock version.
func (z *zfsMock) Promote(name string) error {
	if z.promoteErr {
		return xerrors.New("Mock zfs raised an error on Promote")
	}

	datasets := z.nextD
	if datasets == nil {
		// Create a new independent baking array to not modify z.d
		datasets = append([]*zfs.Dataset(nil), z.d...)
	}

	ds := make(map[string]*zfs.Dataset)
	for _, d := range datasets {
		ds[d.Name] = d
	}

	for _, d := range datasets {
		// Do this for any datasets to promote (main + childrens)
		if !(d.Name == name || (strings.HasPrefix(d.Name, name+"/") && !strings.Contains(d.Name, "@"))) {
			continue
		}

		recursiveOriginReverse("", d, ds)
	}

	// Prepare nextD
	datasets = nil
	for _, d := range ds {
		datasets = append(datasets, d)
	}
	z.nextD = datasets

	return nil
}

// recursiveOriginReverse reverses order of origin for all datasets depending on each other recursively
func recursiveOriginReverse(newOrig string, d *zfs.Dataset, ds map[string]*zfs.Dataset) {
	// Set the origin of current dataset and prepare next one
	prevOrigin := d.Origin
	d.Origin = newOrig
	if prevOrigin == "" {
		return
	}

	// Rename and move the snapshot on the d dataset
	snapD := ds[prevOrigin]
	prevMasterDataset, snapshot := strings.Split(prevOrigin, "@")[0], strings.Split(prevOrigin, "@")[1]
	snapD.Name = d.Name + "@" + snapshot
	ds[snapD.Name] = snapD
	delete(ds, prevOrigin)

	// Next origin will be this snapshot
	newOrig = snapD.Name

	// Find all elements that were relying on this snapshot and do the same treatment recursively
	for _, d := range ds {
		// Change origin on any datasets pointing to previous origin
		if d.Origin != prevOrigin {
			continue
		}
		// Note: we should normally check for separator and exact match (not only substring), but for our test cases
		// that is fine.
		d.Origin = strings.Replace(d.Origin, prevOrigin, newOrig, -1)
	}

	recursiveOriginReverse(newOrig, ds[prevMasterDataset], ds)
}

// splitSnapshotName return base and trailing names
func splitSnapshotName(name string) (string, string) {
	i := strings.LastIndex(name, "@")
	if i < 0 {
		return name, ""
	}
	return name[:i], name[i+1:]
}
