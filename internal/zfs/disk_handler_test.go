package zfs_test

import (
	"flag"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"sync"
	"testing"
	"time"

	libzfs "github.com/bicomsystems/go-libzfs"
	"github.com/ubuntu/zsys/internal/zfs"
	"gopkg.in/yaml.v2"
)

const mB = 1024 * 1024

type fakePools struct {
	Pools                []fakePool
	waitBetweenSnapshots bool
	t                    *testing.T
	tempPools            []string
	tempMountpaths       []string
	tempFiles            []string

	libzfs libZFSInterface
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
		Snapshots        orderedSnapshots
	}
}

type orderedSnapshots []struct {
	Name             string
	Mountpoint       string
	CanMount         string
	ZsysBootfs       string `yaml:"zsys_bootfs"`
	LastBootedKernel string `yaml:"last_booted_kernel"`
	BootfsDatasets   string `yaml:"bootfs_datasets"`
	// LastUsed         time.Time `yaml:"last_used"` Last used will be snapshot creation time, so "now" in tests
}

func (s orderedSnapshots) Len() int           { return len(s) }
func (s orderedSnapshots) Less(i, j int) bool { return s[i].Name < s[j].Name }
func (s orderedSnapshots) Swap(i, j int)      { s[i], s[j] = s[j], s[i] }

var (
	keepPool = flag.Bool("keep-pool", false, "don't destroy pool and temporary folders for it. Don't run multiple tests with this flag.")
)

func withWaitBetweenSnapshots() func(*fakePools) {
	return func(f *fakePools) {
		f.waitBetweenSnapshots = true
	}
}

type libZFSInterface interface {
	PoolOpen(name string) (pool libzfs.Pool, err error)
	PoolCreate(name string, vdev libzfs.VDevTree, features map[string]string,
		props libzfs.PoolProperties, fsprops libzfs.DatasetProperties) (pool libzfs.Pool, err error)
	zfs.LibZFSInterface
}

// withLibZFS allows overriding default libzfs implementations with a mock
func withLibZFS(libzfs libZFSInterface) func(*fakePools) {
	return func(f *fakePools) {
		f.libzfs = libzfs
	}
}

// newFakePools returns a fakePools from a yaml file
func newFakePools(t *testing.T, path string, opts ...func(*fakePools)) fakePools {
	pools := fakePools{
		t:      t,
		libzfs: &zfs.LibZFSAdapter{},
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

func (fpools fakePools) Fatal(args ...interface{}) {
	fpools.t.Helper()
	fpools.cleanup()
	fpools.t.Fatal(args...)
}

func (fpools fakePools) Fatalf(format string, args ...interface{}) {
	fpools.t.Helper()
	fpools.cleanup()
	fpools.t.Fatalf(format, args...)
}

func (fpools fakePools) cleanup() {
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
		if !isLibZFSMock(fpools.libzfs) {
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

// create on disk mock pools as files
func (fpools fakePools) create(path string) func() {
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
				var d zfs.DZFSInterface
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
					dataset.CanMount = "off"
				}
				if dataset.CanMount != "-" {
					d.SetProperty(libzfs.DatasetPropCanmount, dataset.CanMount)
				}

				if dataset.ZsysBootfs != "" {
					d.SetUserProperty(zfs.BootfsProp, dataset.ZsysBootfs)
				}
				if !dataset.LastUsed.IsZero() {
					d.SetUserProperty(zfs.LastUsedProp, strconv.FormatInt(dataset.LastUsed.Unix(), 10))
				}
				if dataset.LastBootedKernel != "" {
					d.SetUserProperty(zfs.LastBootedKernelProp, dataset.LastBootedKernel)
				}
				if dataset.BootfsDatasets != "" {
					d.SetUserProperty(zfs.BootfsDatasetsProp, dataset.BootfsDatasets)
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
						d, err := fpools.libzfs.DatasetSnapshot(datasetName+"@"+s.Name, false, props)
						if err != nil {
							fmt.Fprintf(os.Stderr, "Couldn't create snapshot %q: %v\n", datasetName+"@"+s.Name, err)
							os.Exit(1)
						}
						if s.Mountpoint != "" {
							d.SetUserProperty(zfs.SnapshotMountpointProp, s.Mountpoint)
						}
						if s.CanMount != "" {
							d.SetUserProperty(zfs.SnapshotCanmountProp, s.CanMount)
						}
						if s.ZsysBootfs != "" {
							d.SetUserProperty(zfs.BootfsProp, s.ZsysBootfs)
						}
						if s.LastBootedKernel != "" {
							d.SetUserProperty(zfs.LastBootedKernelProp, s.LastBootedKernel)
						}
						if s.BootfsDatasets != "" {
							d.SetUserProperty(zfs.BootfsDatasetsProp, s.BootfsDatasets)
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
