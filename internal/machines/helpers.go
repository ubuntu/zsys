package machines

import (
	"math/rand"
	"sort"
	"strings"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"
	"github.com/ubuntu/zsys/internal/zfs"
	"golang.org/x/xerrors"
)

// sortDataset enables sorting a slice of Dataset elements.
type sortedDataset []zfs.Dataset

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
		err = xerrors.Errorf("unexpected number of @ in dataset name %q", d.Name)
	}
	return false, err
}

// resolveOrigin iterates over each datasets up to their true origin and replaces them.
// This is only done for / as it's the deduplication we are interested in.
func resolveOrigin(datasets []zfs.Dataset) map[string]*string {
	r := make(map[string]*string)
	for _, curDataset := range datasets {
		if curDataset.Mountpoint != "/" || curDataset.CanMount == "off" {
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
				log.Warningf("didn't find origin %q for %q matching any dataset", *curOrig, curDataset.Name)
				delete(r, curDataset.Name)
				break
			}
		}
	}
	return r
}

// appendDatasetIfNotPresent will check that the dataset wasn't already added and will append it
// excludeCanMountOff restricts (for unlinked datasets) the check on datasets that are canMount noauto or on
func appendIfNotPresent(mainDatasets, newDatasets []zfs.Dataset, excludeCanMountOff bool) []zfs.Dataset {
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

var seedOnce = sync.Once{}

// generateID with n ascii or digits, lowercase, characters
func generateID(n int) string {
	seedOnce.Do(func() { rand.Seed(time.Now().UnixNano()) })

	var allowedRunes = []rune("abcdefghijklmnopqrstuvwxyz0123456789")

	b := make([]rune, n)
	for i := range b {
		b[i] = allowedRunes[rand.Intn(len(allowedRunes))]
	}
	return string(b)
}
