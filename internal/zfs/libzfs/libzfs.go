package libzfs

import (
	"math/rand"
	"sync"
	"time"

	golibzfs "github.com/bicomsystems/go-libzfs"
)

// Adapter is an accessor to real system zfs libraries.
type Adapter struct{}

// PoolOpen opens given pool
func (Adapter) PoolOpen(name string) (pool Pool, err error) {
	return golibzfs.PoolOpen(name)
}

// PoolCreate creates a zfs pool
func (Adapter) PoolCreate(name string, vdev VDevTree, features map[string]string, props PoolProperties, fsprops DatasetProperties) (pool Pool, err error) {
	return golibzfs.PoolCreate(name, vdev, features, props, fsprops)
}

// DatasetOpenAll opens all the dataset recursively
func (l Adapter) DatasetOpenAll() (datasets []DZFSInterface, err error) {
	ds, err := golibzfs.DatasetOpenAll()
	if err != nil {
		return nil, err
	}

	for _, d := range ds {
		d := d
		datasets = append(datasets, dZFSAdapter{&d})
	}
	return datasets, nil
}

// DatasetOpen opens a dataset
func (Adapter) DatasetOpen(name string) (DZFSInterface, error) {
	d, err := golibzfs.DatasetOpen(name)
	if err != nil {
		return dZFSAdapter{}, err
	}
	return dZFSAdapter{&d}, nil
}

// DatasetCreate creates a dataset
func (*Adapter) DatasetCreate(path string, dtype DatasetType, props map[Prop]Property) (DZFSInterface, error) {
	d, err := golibzfs.DatasetCreate(path, dtype, props)
	if err != nil {
		return dZFSAdapter{}, err
	}
	return dZFSAdapter{&d}, nil
}

// DatasetSnapshot creates a snapshot
func (*Adapter) DatasetSnapshot(path string, recur bool, props map[Prop]Property) (DZFSInterface, error) {
	d, err := golibzfs.DatasetSnapshot(path, recur, props)
	if err != nil {
		return dZFSAdapter{}, err
	}
	return dZFSAdapter{&d}, nil
}

var seedOnce = sync.Once{}

// GenerateID with n ascii or digits, lowercase, characters
func (*Adapter) GenerateID(length int) string {
	seedOnce.Do(func() { rand.Seed(time.Now().UnixNano()) })

	var allowedRunes = []rune("abcdefghijklmnopqrstuvwxyz0123456789")

	b := make([]rune, length)
	for i := range b {
		b[i] = allowedRunes[rand.Intn(len(allowedRunes))]
	}
	return string(b)
}

type dZFSAdapter struct {
	*golibzfs.Dataset
}

func (d dZFSAdapter) Children() (children []DZFSInterface) {
	for _, c := range d.Dataset.Children {
		c := c
		children = append(children, dZFSAdapter{&c})
	}
	return children
}

func (d dZFSAdapter) DZFSChildren() *[]Dataset {
	return &d.Dataset.Children
}

func (d dZFSAdapter) Properties() *map[Prop]Property {
	return &d.Dataset.Properties
}

func (d dZFSAdapter) Type() DatasetType {
	return d.Dataset.Type
}

func (d dZFSAdapter) Clone(target string, props map[Prop]Property) (DZFSInterface, error) {
	c, err := d.Dataset.Clone(target, props)
	if err != nil {
		return dZFSAdapter{}, err
	}
	return dZFSAdapter{&c}, nil
}
