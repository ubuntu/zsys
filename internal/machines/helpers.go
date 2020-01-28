package machines

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/ubuntu/zsys/internal/i18n"
	"github.com/ubuntu/zsys/internal/log"
	"github.com/ubuntu/zsys/internal/zfs"
)

// sortDataset enables sorting a slice of Dataset elements.
type sortedDataset []*zfs.Dataset

func (s sortedDataset) Len() int { return len(s) }
func (s sortedDataset) Less(i, j int) bool {
	// We need snapshots root datasets before snapshot children, count the number of / and order by this.
	subDatasetsI := strings.Count(s[i].Name, "/")
	subDatasetsJ := strings.Count(s[j].Name, "/")
	if subDatasetsI != subDatasetsJ {
		return subDatasetsI < subDatasetsJ
	}

	return s[i].Name < s[j].Name
}
func (s sortedDataset) Swap(i, j int) { s[i], s[j] = s[j], s[i] }

// isChild returns if a dataset d is a child of name.
// An error will mean that the dataset name isn't what we expected it to be.
func isChild(name string, d zfs.Dataset) (bool, error) {
	names := strings.Split(name, "@")
	var err error
	switch len(names) {
	// direct system or clone child
	case 1:
		if strings.HasPrefix(d.Name, names[0]+"/") && !strings.Contains(d.Name, "@") {
			return true, nil
		}
	// snapshot child
	case 2:
		if strings.HasPrefix(d.Name, names[0]+"/") && strings.HasSuffix(d.Name, "@"+names[1]) {
			return true, nil
		}
	default:
		err = fmt.Errorf(i18n.G("unexpected number of @ in dataset name %q"), d.Name)
	}
	return false, err
}

func getRootDatasets(ctx context.Context, ds []*zfs.Dataset) (rds map[*zfs.Dataset][]*zfs.Dataset) {
	rds = make(map[*zfs.Dataset][]*zfs.Dataset)
nextUserData:
	for _, d := range ds {
		if d.CanMount == "off" {
			continue
		}
		for r := range rds {
			ok, err := isChild(r.Name, *d)
			if err != nil {
				log.Warningf(ctx, "Couldn’t evaluate if %q is a child of %q: %v", d.Name, r.Name, err)
			}
			if ok {
				rds[r] = append(rds[r], d)
				continue nextUserData
			}
		}
		rds[d] = nil
	}

	return rds
}

// resolveOrigin iterates over each datasets up to their true origin and replaces them.
// This is only done for onlyOnMountpoint if not empty to limit the interest of deduplication we are interested in.
func resolveOrigin(ctx context.Context, datasets []*zfs.Dataset, onlyOnMountpoint string) map[string]*string {
	r := make(map[string]*string)
	for _, curDataset := range datasets {
		if (onlyOnMountpoint != "" && curDataset.Mountpoint != onlyOnMountpoint) || curDataset.CanMount == "off" {
			continue
		}

		// copy to a local variable so that they don't all use the same address
		origin := curDataset.Origin
		if curDataset.IsSnapshot {
			origin = curDataset.Name

		}
		r[curDataset.Name] = &origin

		if *r[curDataset.Name] == "" && !curDataset.IsSnapshot {
			continue
		}

		curOrig := r[curDataset.Name]
	nextOrigin:
		for {
			// origin for a clone points to a snapshot, points directly to the originating file system datasets to prevent a hop
			if j := strings.LastIndex(*curOrig, "@"); j > 0 {
				*curOrig = (*curOrig)[:j]
			}

			originStart := *curOrig
			for _, d := range datasets {
				if *curOrig != d.Name {
					continue
				}
				if d.Origin != "" {
					*curOrig = d.Origin
					break
				}
				break nextOrigin
			}
			if originStart == *curOrig {
				log.Warningf(ctx, i18n.G("Didn't find origin %q for %q matching any dataset"), *curOrig, curDataset.Name)
				delete(r, curDataset.Name)
				break
			}
		}
	}
	return r
}

// appendDatasetIfNotPresent will check that the dataset wasn't already added and will append it
// excludeCanMountOff restricts (for unlinked datasets) the check on datasets that are canMount noauto or on
func appendIfNotPresent(mainDatasets, newDatasets []*zfs.Dataset, excludeCanMountOff bool) []*zfs.Dataset {
	for _, d := range newDatasets {
		if excludeCanMountOff && d.CanMount == "off" {
			continue
		}

		found := false
		for _, mainD := range mainDatasets {
			if mainD.Name == d.Name {
				found = true
				break
			}
		}
		if found {
			continue
		}
		mainDatasets = append(mainDatasets, d)
	}
	return mainDatasets
}

func sortedMachineKeys(m map[string]*Machine) []string {
	keys := make([]string, len(m))
	i := 0
	for k := range m {
		keys[i] = k
		i++
	}
	sort.Strings(keys)
	return keys
}

func sortedStateKeys(m map[string]*State) []string {
	keys := make([]string, len(m))
	i := 0
	for k := range m {
		keys[i] = k
		i++
	}
	sort.Strings(keys)
	return keys
}

// splitSnapshotName return base and trailing names
func splitSnapshotName(name string) (string, string) {
	i := strings.LastIndex(name, "@")
	if i < 0 {
		return name, ""
	}
	return name[:i], name[i+1:]
}

// nameInBootfsDatasets returns if name is part of the bootfsdatsets list for d
func nameInBootfsDatasets(name string, d zfs.Dataset) bool {
	for _, bootfsDataset := range strings.Split(d.BootfsDatasets, bootfsdatasetsSeparator) {
		if bootfsDataset == name || strings.HasPrefix(d.BootfsDatasets, name+"/") {
			return true
		}
	}
	return false
}
