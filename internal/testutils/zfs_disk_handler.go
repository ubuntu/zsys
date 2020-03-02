package testutils

import (
	"flag"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/ubuntu/zsys/internal/zfs/libzfs"
	"github.com/ubuntu/zsys/internal/zfs/libzfs/mock"
	"gopkg.in/yaml.v2"
)

const mB = 1024 * 1024

// FakePools is the handler for pool options and yaml loading
type FakePools struct {
	Pools                []fakePool
	waitBetweenSnapshots bool
	t                    tester
	tempPools            []string
	tempMountpaths       []string
	tempFiles            []string

	libzfs LibZFSInterface
}

type fakePool struct {
	Name     string
	Datasets []struct {
		Name             string
		Mountpoint       string
		CanMount         string
		ZsysBootfs       string    `yaml:"zsys_bootfs"`
		LastUsed         time.Time `yaml:"last_used"`
		LastBootedKernel string    `yaml:"last_booted_kernel"`
		BootfsDatasets   string    `yaml:"bootfs_datasets"`
		Origin           string    `yaml:"origin"`
		Snapshots        orderedSnapshots
	}
}

type orderedSnapshots []struct {
	Name             string
	Mountpoint       string
	CanMount         string
	ZsysBootfs       string     `yaml:"zsys_bootfs"`
	LastBootedKernel string     `yaml:"last_booted_kernel"`
	BootfsDatasets   string     `yaml:"bootfs_datasets"`
	CreationTime     *time.Time `yaml:"creation_time"` // Snapshot creation time, only work for mock usage.
}

func (s orderedSnapshots) Len() int           { return len(s) }
func (s orderedSnapshots) Less(i, j int) bool { return s[i].Name < s[j].Name }
func (s orderedSnapshots) Swap(i, j int)      { s[i], s[j] = s[j], s[i] }

var (
	keepPool = flag.Bool("keep-pool", false, "don't destroy pool and temporary folders for it. Don't run multiple tests with this flag.")
)

// WithWaitBetweenSnapshots force waiting between 2 snapshots creations on the same parent
func WithWaitBetweenSnapshots() func(*FakePools) {
	return func(f *FakePools) {
		f.waitBetweenSnapshots = true
	}
}

// LibZFSInterface is the interface used to create zfs pools on disk or in memory
type LibZFSInterface interface {
	PoolOpen(name string) (pool libzfs.Pool, err error)
	PoolCreate(name string, vdev libzfs.VDevTree, features map[string]string,
		props libzfs.PoolProperties, fsprops libzfs.DatasetProperties) (pool libzfs.Pool, err error)
	libzfs.Interface
}

// WithLibZFS allows overriding default libzfs implementations with a mock
func WithLibZFS(libzfs LibZFSInterface) func(*FakePools) {
	return func(f *FakePools) {
		f.libzfs = libzfs
	}
}

// NewFakePools returns a FakePools from a yaml file
func NewFakePools(t tester, path string, opts ...func(*FakePools)) FakePools {
	pools := FakePools{
		t:      t,
		libzfs: &libzfs.Adapter{},
	}
	b, err := ioutil.ReadFile(path)
	if err != nil {
		t.Fatal("couldn't read yaml definition file", err)
	}
	if err = yaml.Unmarshal(b, &pools); err != nil {
		t.Fatal("couldn't unmarshal device list", err)
	}

	for _, o := range opts {
		o(&pools)
	}

	return pools
}

// Fatal trigger a Fatal testing error
func (fpools FakePools) Fatal(args ...interface{}) {
	fpools.t.Helper()
	fpools.cleanup()
	fpools.t.Fatal(args...)
}

// Fatalf triggers a fatal testing error with formatting
func (fpools FakePools) Fatalf(format string, args ...interface{}) {
	fpools.t.Helper()
	fpools.cleanup()
	fpools.t.Fatalf(format, args...)
}

func (fpools FakePools) cleanup() {
	if *keepPool {
		fmt.Printf("Keep pool(s) as requesting for debug. Please zpool export them afterwards and remove %q\n", filepath.Dir(fpools.tempFiles[0]))
		return
	}

	for _, p := range fpools.tempPools {
		pool, err := fpools.libzfs.PoolOpen(p)
		if err != nil {
			fpools.t.Logf("couldn't delete %q: %v", p, err)
			continue
		}
		if _, ok := fpools.libzfs.(*mock.LibZFS); !ok {
			pool.Export(true, fmt.Sprintf("Export temporary pool %q", p))
		}

		pool.Destroy(fmt.Sprintf("Cleanup temporary pool %q", p))
		pool.Close()
	}
	for _, p := range fpools.tempMountpaths {
		if err := os.RemoveAll(p); err != nil {
			fpools.t.Logf("couldn't delete mountpoint %q: %v", p, err)
		}
	}
	for _, f := range fpools.tempFiles {
		if err := os.Remove(f); err != nil {
			fpools.t.Logf("couldn't delete %q: %v", f, err)
		}
	}
}

// Create on disk mock pools as files
func (fpools FakePools) Create(path string) func() {
	snapshotWG := sync.WaitGroup{}
	for _, fpool := range fpools.Pools {
		func() {
			// Create device as file on disk
			p := filepath.Join(path, fpool.Name+".disk")
			f, err := os.Create(p)
			if err != nil {
				fpools.Fatal("couldn't create device file on disk", err)
			}
			fpools.tempFiles = append(fpools.tempFiles, p)
			if err = f.Truncate(100 * mB); err != nil {
				f.Close()
				fpools.Fatal("couldn't initializing device size on disk", err)
			}
			f.Close()

			poolMountpath := filepath.Join(path, fpool.Name)
			if err := os.MkdirAll(poolMountpath, 0700); err != nil {
				fpools.Fatal("couldn't create directory for pool", err)
			}
			fpools.tempMountpaths = append(fpools.tempMountpaths, poolMountpath)

			vdev := libzfs.VDevTree{
				Type:    libzfs.VDevTypeFile,
				Path:    p,
				Devices: []libzfs.VDevTree{{Type: libzfs.VDevTypeFile, Path: p}},
			}

			features := make(map[string]string)
			props := make(map[libzfs.Prop]string)
			props[libzfs.PoolPropAltroot] = poolMountpath
			fsprops := make(map[libzfs.Prop]string)
			// Could be overridden with the "." dataset
			fsprops[libzfs.DatasetPropMountpoint] = "/"
			fsprops[libzfs.DatasetPropCanmount] = "off"

			pool, err := fpools.libzfs.PoolCreate(fpool.Name, vdev, features, props, fsprops)
			if err != nil {
				fpools.Fatalf("couldn't create pool %q: %v", fpool.Name, err)
			}
			fpools.tempPools = append(fpools.tempPools, fpool.Name)
			defer pool.Close()

			for _, dataset := range fpool.Datasets {
				datasetName := fpool.Name + "/" + dataset.Name
				var d libzfs.DZFSInterface
				if dataset.Name == "." {
					datasetName = fpool.Name
					d, err = fpools.libzfs.DatasetOpen(datasetName)
					if err != nil {
						fpools.Fatalf("couldn't open dataset %q: %v", datasetName, err)
					}
				} else {
					props := make(map[libzfs.Prop]libzfs.Property)
					d, err = fpools.libzfs.DatasetCreate(datasetName, libzfs.DatasetTypeFilesystem, props)
					if err != nil {
						fpools.Fatalf("couldn't create dataset %q: %v", datasetName, err)
					}
				}
				if dataset.Mountpoint != "" {
					d.SetProperty(libzfs.DatasetPropMountpoint, dataset.Mountpoint)
				}
				if dataset.CanMount == "" {
					dataset.CanMount = "on"
				}
				if dataset.CanMount != "-" {
					d.SetProperty(libzfs.DatasetPropCanmount, dataset.CanMount)
				}

				if dataset.ZsysBootfs != "" {
					d.SetUserProperty(libzfs.BootfsProp, dataset.ZsysBootfs)
				}
				if !dataset.LastUsed.IsZero() {
					d.SetUserProperty(libzfs.LastUsedProp, strconv.FormatInt(dataset.LastUsed.Unix(), 10))
				}
				if dataset.LastBootedKernel != "" {
					d.SetUserProperty(libzfs.LastBootedKernelProp, dataset.LastBootedKernel)
				}
				if dataset.BootfsDatasets != "" {
					d.SetUserProperty(libzfs.BootfsDatasetsProp, dataset.BootfsDatasets)
				}
				if dataset.Origin != "" {
					if _, ok := fpools.libzfs.(*mock.LibZFS); !ok {
						fpools.Fatalf("trying to set origin on clone for %q on real ZFS run. This is not possible", datasetName)
					}
					d.SetProperty(libzfs.DatasetPropOrigin, dataset.Origin)
				}
				d.Close()

				snapshotWG.Add(1)
				go func(snapshots orderedSnapshots) {
					defer snapshotWG.Done()
					sort.Sort(snapshots)
					for i, s := range snapshots {
						// Dataset creation time have second granularity. As we want for some tests reproducible
						// snapshot orders (like promotion), we need then to ensure we create them at an expected rate.
						if fpools.waitBetweenSnapshots && i > 0 {
							time.Sleep(time.Second)
						}
						props := make(map[libzfs.Prop]libzfs.Property)
						if s.CreationTime != nil {
							if _, ok := fpools.libzfs.(*mock.LibZFS); !ok {
								fpools.Fatalf("trying to set snapshot time for %q on real ZFS run. This is not possible", datasetName)
							}
							props[libzfs.DatasetPropCreation] = libzfs.Property{Value: strconv.FormatInt(s.CreationTime.Unix(), 10)}
						}
						userProps := make(map[string]string)
						if s.Mountpoint != "" {
							userProps[libzfs.SnapshotMountpointProp] = s.Mountpoint
						}
						if s.CanMount != "" {
							userProps[libzfs.SnapshotCanmountProp] = s.CanMount
						}
						if s.ZsysBootfs != "" {
							userProps[libzfs.BootfsProp] = s.ZsysBootfs
						}
						if s.LastBootedKernel != "" {
							userProps[libzfs.LastBootedKernelProp] = s.LastBootedKernel
						}
						if s.BootfsDatasets != "" {
							userProps[libzfs.BootfsDatasetsProp] = s.BootfsDatasets
						}
						d, err := fpools.libzfs.DatasetSnapshot(datasetName+"@"+s.Name, false, props, userProps)
						if err != nil {
							fmt.Fprintf(os.Stderr, "Couldn't create snapshot %q: %v\n", datasetName+"@"+s.Name, err)
							os.Exit(1)
						}
						d.Close()
					}
				}(dataset.Snapshots)
			}
		}()
	}
	snapshotWG.Wait()
	return fpools.cleanup
}
