package zfs_test

import (
	"flag"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	libzfs "github.com/bicomsystems/go-libzfs"
	"github.com/ubuntu/zsys/internal/zfs"
	"gopkg.in/yaml.v2"
)

const mB = 1024 * 1024

type fakePools struct {
	Pools          []fakePool
	t              *testing.T
	tempPools      []string
	tempMountpaths []string
	tempFiles      []string
}

type fakePool struct {
	Name     string
	Datasets []struct {
		Name           string
		Mountpoint     string
		CanMount       string
		ZsysBootfs     string    `yaml:"zsys_bootfs"`
		LastUsed       time.Time `yaml:"last_used"`
		BootfsDatasets string    `yaml:"bootfs_datasets"`
		Snapshots      []struct {
			Name string
		}
	}
}

var (
	keepPool = flag.Bool("keep-pool", false, "don't destroy pool and temporary folders for it. Don't run multiple tests with this flag.")
)

// newFakePools returns a fakePools from a yaml file
func newFakePools(t *testing.T, path string) fakePools {
	pools := fakePools{t: t}
	b, err := ioutil.ReadFile(path)
	if err != nil {
		t.Fatal("couldn't read yaml definition file", err)
	}
	if err = yaml.Unmarshal(b, &pools); err != nil {
		t.Fatal("couldn't unmarshal device list", err)
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
		pool, err := libzfs.PoolOpen(p)
		if err != nil {
			fpools.t.Logf("couldn't delete %q: %v", p, err)
			continue
		}
		pool.Export(true, fmt.Sprintf("Export temporary pool %q", p))
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
			// Could be overriden with the "." dataset
			fsprops[libzfs.DatasetPropMountpoint] = "/"
			fsprops[libzfs.DatasetPropCanmount] = "off"

			pool, err := libzfs.PoolCreate(fpool.Name, vdev, features, props, fsprops)
			if err != nil {
				fpools.Fatalf("couldn't create pool %q: %v", fpool.Name, err)
			}
			fpools.tempPools = append(fpools.tempPools, fpool.Name)
			defer pool.Close()

			for _, dataset := range fpool.Datasets {
				func() {
					datasetName := fpool.Name + "/" + dataset.Name
					var d libzfs.Dataset
					if dataset.Name == "." {
						datasetName = fpool.Name
						d, err = libzfs.DatasetOpen(datasetName)
						if err != nil {
							fpools.Fatalf("couldn't open dataset %q: %v", datasetName, err)
						}
					} else {
						props := make(map[libzfs.Prop]libzfs.Property)
						d, err = libzfs.DatasetCreate(datasetName, libzfs.DatasetTypeFilesystem, props)
						if err != nil {
							fpools.Fatalf("couldn't create dataset %q: %v", datasetName, err)
						}
					}
					defer d.Close()
					if dataset.Mountpoint != "" {
						d.SetProperty(libzfs.DatasetPropMountpoint, dataset.Mountpoint)
					}
					if dataset.CanMount == "" {
						dataset.CanMount = "off"
					}
					d.SetProperty(libzfs.DatasetPropCanmount, dataset.CanMount)

					if dataset.ZsysBootfs != "" {
						d.SetUserProperty(zfs.BootfsProp, dataset.ZsysBootfs)
					}
					if !dataset.LastUsed.IsZero() {
						d.SetUserProperty(zfs.LastUsedProp, strconv.FormatInt(dataset.LastUsed.Unix(), 10))
					}
					if dataset.BootfsDatasets != "" {
						d.SetUserProperty(zfs.BootfsDatasetsProp, dataset.BootfsDatasets)
					}

					for _, s := range dataset.Snapshots {
						props := make(map[libzfs.Prop]libzfs.Property)
						d, err := libzfs.DatasetSnapshot(datasetName+"@"+s.Name, false, props)
						if err != nil {
							fmt.Fprintf(os.Stderr, "Couldn't create snapshot %q: %v\n", datasetName+"@"+s.Name, err)
							os.Exit(1)
						}
						d.Close()
					}
				}()
			}
		}()
	}
	return fpools.cleanup
}
