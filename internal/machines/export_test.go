package machines

import (
	"encoding/json"
	"sort"
	"strings"

	"github.com/ubuntu/zsys/internal/zfs"
)

const (
	RevertUserDataTag = zfsRevertUserDataTag
)

type SortedDatasets []*zfs.Dataset

func (s SortedDatasets) Len() int { return len(s) }
func (s SortedDatasets) Less(i, j int) bool {
	return strings.ReplaceAll(s[i].Name, "@", "#") < strings.ReplaceAll(s[j].Name, "@", "#")
}
func (s SortedDatasets) Swap(i, j int) { s[i], s[j] = s[j], s[i] }

// Import from json to export the private fields
func (m *Machines) UnmarshalJSON(b []byte) error {
	mt := Machinesdump{}

	if err := json.Unmarshal(b, &mt); err != nil {
		return err
	}

	m.all = mt.All
	m.cmdline = mt.Cmdline
	m.current = mt.Current
	m.nextState = mt.NextState
	m.allSystemDatasets = mt.AllSystemDatasets
	m.allUsersDatasets = mt.AllUsersDatasets
	m.allPersistentDatasets = mt.AllPersistentDatasets
	m.unmanagedDatasets = mt.UnmanagedDatasets

	if m.current != nil {
		for k, machine := range mt.All {
			if machine.ID != m.current.ID {
				continue
			}
			// restore current machine pointing to the same element than the hashmap
			m.current = mt.All[k]
		}
	}

	return nil
}

// MakeComparable prepares Machines by resetting private fields that change at each invocation
func (m *Machines) MakeComparable() {
	ds := SortedDatasets(m.allSystemDatasets)
	sort.Sort(ds)
	m.allSystemDatasets = ds

	m.z = nil
}

// SplitSnapshotName calls internal splitSnapshotName to split a snapshot name in base and id of a snapshot
func SplitSnapshotName(s string) (string, string) { return splitSnapshotName(s) }

// AllMachines exports machines lists for tests
func (m *Machines) AllMachines() map[string]*Machine { return m.all }
