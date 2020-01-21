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

type MachinesTest struct {
	All               map[string]*Machine `json:",omitempty"`
	Cmdline           string              `json:",omitempty"`
	Current           *Machine            `json:",omitempty"`
	NextState         *State              `json:",omitempty"`
	AllSystemDatasets []*zfs.Dataset      `json:",omitempty"`
	AllUsersDatasets  []*zfs.Dataset      `json:",omitempty"`
}

type SortedDatasets []*zfs.Dataset

func (s SortedDatasets) Len() int { return len(s) }
func (s SortedDatasets) Less(i, j int) bool {
	return strings.ReplaceAll(s[i].Name, "@", "#") < strings.ReplaceAll(s[j].Name, "@", "#")
}
func (s SortedDatasets) Swap(i, j int) { s[i], s[j] = s[j], s[i] }

// Export for json Marshmalling all private fields
func (m Machines) MarshalJSON() ([]byte, error) {
	mt := MachinesTest{}

	mt.All = m.all
	mt.Cmdline = m.cmdline
	mt.Current = m.current
	mt.NextState = m.nextState
	mt.AllSystemDatasets = m.allSystemDatasets
	mt.AllUsersDatasets = m.allUsersDatasets

	return json.Marshal(mt)
}

// Import from json to export the private fields
func (m *Machines) UnmarshalJSON(b []byte) error {
	mt := MachinesTest{}

	if err := json.Unmarshal(b, &mt); err != nil {
		return err
	}

	m.all = mt.All
	m.cmdline = mt.Cmdline
	m.current = mt.Current
	m.nextState = mt.NextState
	m.allSystemDatasets = mt.AllSystemDatasets
	m.allUsersDatasets = mt.AllUsersDatasets

	return nil
}

// MakeComparable prepares Machines by resetting private fields that change at each invocation
func (m *Machines) MakeComparable() {
	for k, machine := range m.all {
		ds := SortedDatasets(machine.UserDatasets)
		sort.Sort(ds)
		m.all[k].UserDatasets = ds
		for l, h := range machine.History {
			ds := SortedDatasets(h.UserDatasets)
			sort.Sort(ds)
			m.all[k].History[l].UserDatasets = ds
		}
	}

	m.z = nil
}
