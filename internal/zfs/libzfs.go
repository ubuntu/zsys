package zfs

import libzfs "github.com/bicomsystems/go-libzfs"

type libZFSAdapter struct{}

func (libZFSAdapter) PoolOpen(name string) (pool libzfs.Pool, err error) {
	return libzfs.PoolOpen(name)
}

func (libZFSAdapter) PoolCreate(name string, vdev libzfs.VDevTree, features map[string]string, props libzfs.PoolProperties, fsprops libzfs.DatasetProperties) (pool libzfs.Pool, err error) {
	return libzfs.PoolCreate(name, vdev, features, props, fsprops)
}

func (l libZFSAdapter) DatasetOpenAll() (datasets []dZFSInterface, err error) {
	ds, err := libzfs.DatasetOpenAll()
	if err != nil {
		return nil, err
	}

	for _, d := range ds {
		d := d
		datasets = append(datasets, dZFSAdapter{&d})
	}
	return datasets, nil
}

func (libZFSAdapter) DatasetOpen(name string) (dZFSInterface, error) {
	d, err := libzfs.DatasetOpen(name)
	if err != nil {
		return dZFSAdapter{}, err
	}
	return dZFSAdapter{&d}, nil
}

func (*libZFSAdapter) DatasetCreate(path string, dtype libzfs.DatasetType, props map[libzfs.Prop]libzfs.Property) (dZFSInterface, error) {
	d, err := libzfs.DatasetCreate(path, dtype, props)
	if err != nil {
		return dZFSAdapter{}, err
	}
	return dZFSAdapter{&d}, nil
}

func (*libZFSAdapter) DatasetSnapshot(path string, recur bool, props map[libzfs.Prop]libzfs.Property) (dZFSInterface, error) {
	d, err := libzfs.DatasetSnapshot(path, recur, props)
	if err != nil {
		return dZFSAdapter{}, err
	}
	return dZFSAdapter{&d}, nil
}

type dZFSAdapter struct {
	*libzfs.Dataset
}

func (d dZFSAdapter) Children() (children []dZFSInterface) {
	for _, c := range d.Dataset.Children {
		c := c
		children = append(children, dZFSAdapter{&c})
	}
	return children
}

func (d dZFSAdapter) dZFSChildren() *[]libzfs.Dataset {
	return &d.Dataset.Children
}

func (d dZFSAdapter) Properties() *map[libzfs.Prop]libzfs.Property {
	return &d.Dataset.Properties
}

func (d dZFSAdapter) Type() libzfs.DatasetType {
	return d.Dataset.Type
}

func (d dZFSAdapter) Clone(target string, props map[libzfs.Prop]libzfs.Property) (dZFSInterface, error) {
	c, err := d.Dataset.Clone(target, props)
	if err != nil {
		return dZFSAdapter{}, err
	}
	return dZFSAdapter{&c}, nil
}
