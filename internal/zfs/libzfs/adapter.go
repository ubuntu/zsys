package libzfs

import (
	golibzfs "github.com/bicomsystems/go-libzfs"
)

type (
	// Prop type to enumerate all different properties supported by ZFS
	Prop = golibzfs.Prop
	// Property ZFS pool or dataset property value
	Property = golibzfs.Property
	// Pool object represents handler to single ZFS pool Pool.Properties map[string]Property Map of all ZFS pool properties,
	// changing any of this will not affect ZFS pool, for that use SetProperty( name, value string) method of the pool object.
	Pool = golibzfs.Pool
	// PoolProperties type is map of pool properties name -> value
	PoolProperties = golibzfs.PoolProperties
	// VDevTree ZFS virtual device tree
	VDevTree = golibzfs.VDevTree
	// Dataset - ZFS dataset object
	Dataset = golibzfs.Dataset
	// DatasetType defines enum of dataset types
	DatasetType = golibzfs.DatasetType
	// DatasetProperties type is map of dataset or volume properties prop -> value
	DatasetProperties = golibzfs.DatasetProperties
)

const (
	// PoolPropAltroot ZFS Pool property
	PoolPropAltroot = golibzfs.PoolPropAltroot
	// PoolNumProps is the end pool number property
	PoolNumProps = golibzfs.PoolNumProps
	// VDevTypeFile is the vdevtype on file
	VDevTypeFile = golibzfs.VDevTypeFile
	// DatasetTypeFilesystem - file system dataset
	DatasetTypeFilesystem = golibzfs.DatasetTypeFilesystem
	// DatasetTypeSnapshot - snapshot of dataset
	DatasetTypeSnapshot = golibzfs.DatasetTypeSnapshot
	// DatasetTypeVolume - volume (virtual block device) dataset
	DatasetTypeVolume = golibzfs.DatasetTypeVolume
	// DatasetTypeBookmark - bookmark dataset
	DatasetTypeBookmark = golibzfs.DatasetTypeBookmark
	// DatasetPropName is the name of the dataset
	DatasetPropName = golibzfs.DatasetPropName
	// DatasetPropCanmount is the canmount property of the dataset
	DatasetPropCanmount = golibzfs.DatasetPropCanmount
	// DatasetPropMountpoint is the mountpoint of the dataset
	DatasetPropMountpoint = golibzfs.DatasetPropMountpoint
	// DatasetPropOrigin is the origin of the dataset
	DatasetPropOrigin = golibzfs.DatasetPropOrigin
	// DatasetPropMounted is the mounted property for the dataset
	DatasetPropMounted = golibzfs.DatasetPropMounted
	// DatasetPropCreation is the creation time property for the dataset
	DatasetPropCreation = golibzfs.DatasetPropCreation
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

// Interface is the interface to use real libzfs or our in memory mock.
type Interface interface {
	DatasetOpenAll() (datasets []DZFSInterface, err error)
	DatasetOpen(name string) (d DZFSInterface, err error)
	DatasetCreate(path string, dtype DatasetType, props map[Prop]Property) (d DZFSInterface, err error)
	DatasetSnapshot(path string, recur bool, props map[Prop]Property) (rd DZFSInterface, err error)
	GenerateID(length int) string
}

// DZFSInterface is the interface to use real libzfs Dataset object or in memory mock.
type DZFSInterface interface {
	DZFSChildren() *[]Dataset
	Children() []DZFSInterface
	Clone(target string, props map[Prop]Property) (rd DZFSInterface, err error)
	Clones() (clones []string, err error)
	Close()
	Destroy(Defer bool) (err error)
	GetUserProperty(p string) (prop Property, err error)
	IsSnapshot() (ok bool)
	Pool() (p Pool, err error)
	Promote() (err error)
	Properties() *map[Prop]Property
	ReloadProperties() (err error)
	SetUserProperty(prop, value string) error
	SetProperty(p Prop, value string) error
	Type() DatasetType
}
