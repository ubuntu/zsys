package zfs_test

import (
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

type FakePools struct {
	Pools          []FakePool
	t              *testing.T
	tempPools      []string
	tempMountpaths []string
	tempFiles      []string
}

type FakePool struct {
	Name     string
	Datasets []struct {
		Name          string
		Mountpoint    string
		CanMount      string
		ZsysBootfs    bool      `yaml:"zsys_bootfs"`
		LastUsed      time.Time `yaml:"last_used"`
		SystemDataset string    `yaml:"system_dataset"`
		Snapshots     []struct {
			Name         string
			CreationDate time.Time `yaml:"creation_date"`
		}
	}
}

// newFakePools returns a FakePools from a yaml file
func newFakePools(t *testing.T, path string) FakePools {
	pools := FakePools{t: t}
	b, err := ioutil.ReadFile(path)
	if err != nil {
		t.Fatal("couldn't read yaml definition file", err)
	}
	if err = yaml.Unmarshal(b, &pools); err != nil {
		t.Fatal("couldn't unmarshal device list", err)
	}

	return pools
}

func (fpools FakePools) Fatal(args ...interface{}) {
	fpools.t.Helper()
	fpools.cleanup()
	fpools.t.Fatal(args...)
}

func (fpools FakePools) Fatalf(format string, args ...interface{}) {
	fpools.t.Helper()
	fpools.cleanup()
	fpools.t.Fatalf(format, args...)
}

func (fpools FakePools) cleanup() {
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
func (fpools FakePools) create(path, testName string) func() {
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

			// WORKAROUND: we need to use dirname of path as creating 2 consecutives pools with similar dataset name
			// will make the second dataset from the second pool returning as parent pool the first one.
			// Of course, the resulting mountpoint will be wrong.
			poolName := testName + "-" + fpool.Name

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

			pool, err := libzfs.PoolCreate(poolName, vdev, features, props, fsprops)
			if err != nil {
				fpools.Fatalf("couldn't create pool %q: %v", fpool.Name, err)
			}
			fpools.tempPools = append(fpools.tempPools, poolName)
			defer pool.Close()

			for _, dataset := range fpool.Datasets {
				func() {
					datasetName := poolName + "/" + dataset.Name
					var d libzfs.Dataset
					if dataset.Name == "." {
						datasetName = poolName
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

					if dataset.ZsysBootfs {
						d.SetUserProperty(zfs.BootfsProp, "yes")
					}
					if !dataset.LastUsed.IsZero() {
						d.SetUserProperty(zfs.LastUsedProp, strconv.FormatInt(dataset.LastUsed.Unix(), 10))
					}
					if dataset.SystemDataset != "" {
						d.SetUserProperty(zfs.SystemDataProp, dataset.SystemDataset)
					}

					for _, s := range dataset.Snapshots {
						func() {
							props := make(map[libzfs.Prop]libzfs.Property)
							d, err := libzfs.DatasetSnapshot(datasetName+"@"+s.Name, false, props)
							if err != nil {
								fmt.Fprintf(os.Stderr, "Couldn't create snapshot %q: %v\n", datasetName+"@"+s.Name, err)
								os.Exit(1)
							}
							defer d.Close()

							// TODO: analyze this (no mock in binding)
							// Convert time in current timezone for mock
							location, err := time.LoadLocation("Local")
							if err != nil {
								fpools.Fatal("couldn't get current timezone", err)
							}
							d.SetUserProperty("org.zsys:creation.test", strconv.FormatInt(s.CreationDate.In(location).Unix(), 10))
						}()
					}
				}()
			}
		}()
	}
	return fpools.cleanup
}
