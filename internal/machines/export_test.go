package machines

import (
	"encoding/json"
	"sort"
	"strings"

	"github.com/ubuntu/zsys/internal/config"
	"github.com/ubuntu/zsys/internal/zfs"
)

const (
	RevertUserDataTag       = zfsRevertUserDataTag
	AutomatedSnapshotPrefix = automatedSnapshotPrefix
)

type SortedDatasets []*zfs.Dataset

func (s SortedDatasets) Len() int { return len(s) }
func (s SortedDatasets) Less(i, j int) bool {
	return strings.ReplaceAll(s[i].Name, "@", "#") < strings.ReplaceAll(s[j].Name, "@", "#")
}
func (s SortedDatasets) Swap(i, j int) { s[i], s[j] = s[j], s[i] }

// Import from json to export the private fields
func (ms *Machines) UnmarshalJSON(b []byte) error {
	mt := Machinesdump{}

	if err := json.Unmarshal(b, &mt); err != nil {
		return err
	}

	ms.all = mt.All
	ms.cmdline = mt.Cmdline
	ms.current = mt.Current
	ms.nextState = mt.NextState
	ms.allSystemDatasets = mt.AllSystemDatasets
	ms.allUsersDatasets = mt.AllUsersDatasets
	ms.allPersistentDatasets = mt.AllPersistentDatasets
	ms.unmanagedDatasets = mt.UnmanagedDatasets

	if ms.current != nil {
		for k, machine := range mt.All {
			if machine.ID != ms.current.ID {
				continue
			}
			// restore current machine pointing to the same element than the hashmap
			ms.current = mt.All[k]
		}
	}

	return nil
}

// MakeComparable prepares Machines by resetting private fields that change at each invocation
func (ms *Machines) MakeComparable() {
	ds := SortedDatasets(ms.allSystemDatasets)
	sort.Sort(ds)
	ms.allSystemDatasets = ds

	ms.z = nil
	ms.conf = config.ZConfig{}
}

// SplitSnapshotName calls internal splitSnapshotName to split a snapshot name in base and id of a snapshot
func SplitSnapshotName(s string) (string, string) { return splitSnapshotName(s) }

// AllMachines exports machines lists for tests
func (ms *Machines) AllMachines() map[string]*Machine { return ms.all }
