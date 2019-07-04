package zfs

import (
	"log"
	"strconv"
	"strings"

	libzfs "github.com/bicomsystems/go-libzfs"
	"github.com/ubuntu/zsys/internal/config"
	"golang.org/x/xerrors"
)

// getDatasetsProp returns all properties for a given dataset and the source of them.
// for snapshots, we'll take the parent dataset for the mount properties.
func getDatasetProp(d libzfs.Dataset) (*DatasetProp, error) {
	sources := datasetSources{}
	name := d.Properties[libzfs.DatasetPropName].Value

	var mountPropertiesDataset = &d
	if d.IsSnapshot() {
		parentName := name[:strings.LastIndex(name, "@")]
		pd, err := libzfs.DatasetOpen(parentName)
		if err != nil {
			return nil, xerrors.Errorf("can't get parent dataset: "+config.ErrorFormat, err)
		}
		defer pd.Close()
		mountPropertiesDataset = &pd
	}

	var mountpoint, canMount string
	mp, err := mountPropertiesDataset.GetProperty(libzfs.DatasetPropMountpoint)
	if err != nil {
		return nil, xerrors.Errorf("can't get mountpoint: "+config.ErrorFormat, err)
	}
	sources.Mountpoint = mp.Source

	p, err := mountPropertiesDataset.Pool()
	if err != nil {
		return nil, xerrors.Errorf("can't get associated pool: "+config.ErrorFormat, err)
	}
	poolRoot, err := p.GetProperty(libzfs.PoolPropAltroot)
	if err != nil {
		return nil, xerrors.Errorf("can't get altroot for associated pool: "+config.ErrorFormat, err)
	}
	mountpoint = strings.TrimPrefix(mp.Value, poolRoot.Value)
	if mountpoint == "" {
		mountpoint = "/"
	}

	cm, err := mountPropertiesDataset.GetProperty(libzfs.DatasetPropCanmount)
	if err != nil {
		return nil, xerrors.Errorf("can't get canmount property: "+config.ErrorFormat, err)
	}
	canMount = cm.Value
	sources.CanMount = cm.Source

	bfs, err := d.GetUserProperty(BootfsProp)
	if err != nil {
		return nil, xerrors.Errorf("can't get bootfs property: "+config.ErrorFormat, err)
	}
	bootfs := bfs.Value
	if bootfs == "-" {
		bootfs = ""
	}
	sources.BootFS = bfs.Source

	var lu libzfs.Property
	if !d.IsSnapshot() {
		lu, err = d.GetUserProperty(LastUsedProp)
		if err != nil {
			return nil, xerrors.Errorf("can't get %q property: "+config.ErrorFormat, LastUsedProp, err)
		}
	} else {
		lu, err = d.GetProperty(libzfs.DatasetPropCreation)
		if err != nil {
			return nil, xerrors.Errorf("can't get creation property: "+config.ErrorFormat, err)
		}
	}
	sources.LastUsed = lu.Source
	if lu.Value == "-" {
		lu.Value = "0"
	}
	lastused, err := strconv.Atoi(lu.Value)
	if err != nil {
		return nil, xerrors.Errorf("%q property isn't an int: "+config.ErrorFormat, LastUsedProp, err)
	}

	sDataset, err := d.GetUserProperty(SystemDataProp)
	if err != nil {
		return nil, xerrors.Errorf("can't get %q property: "+config.ErrorFormat, SystemDataProp, err)
	}
	systemDataset := sDataset.Value
	if systemDataset == "-" {
		systemDataset = ""
	}
	sources.SystemDataset = sDataset.Source

	return &DatasetProp{
		Mountpoint:    mountpoint,
		CanMount:      canMount,
		BootFS:        bootfs,
		LastUsed:      lastused,
		SystemDataset: systemDataset,
		sources:       sources,
	}, nil
}

// collectDatasets returns a Dataset tuple of all its properties and children
func collectDatasets(d libzfs.Dataset) []Dataset {
	var results []Dataset
	var collectErr error

	defer func() {
		if collectErr != nil {
			log.Printf("couldn't load dataset: "+config.ErrorFormat+"\n", collectErr)
		}
	}()

	// Skip non file system or snapshot datasets
	if d.Type == libzfs.DatasetTypeVolume || d.Type == libzfs.DatasetTypeBookmark {
		return nil
	}

	name := d.Properties[libzfs.DatasetPropName].Value

	props, err := getDatasetProp(d)
	if err != nil {
		collectErr = xerrors.Errorf("can't get dataset properties for %q: "+config.ErrorFormat, name, collectErr)
		return nil
	}

	results = append(results,
		Dataset{
			Name:        name,
			IsSnapshot:  d.IsSnapshot(),
			DatasetProp: *props,
		})

	for _, dc := range d.Children {
		results = append(results, collectDatasets(dc)...)
	}

	return results
}

// separateSnaphotName return base and trailing names
func separateSnaphotName(name string) (string, string) {
	i := strings.LastIndex(name, "@")
	if i < 0 {
		return name, ""
	}
	return name[:i], name[i+1:]
}
