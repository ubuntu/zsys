package machines

import (
	"encoding/json"
	"sort"
	"strings"

	"github.com/ubuntu/zsys/internal/zfs"
)

// Machinesdump represents the structure of a machine to be exported
type Machinesdump struct {
	All                   map[string]*Machine `json:",omitempty"`
	Cmdline               string              `json:",omitempty"`
	Current               *Machine            `json:",omitempty"`
	NextState             *State              `json:",omitempty"`
	AllSystemDatasets     []*zfs.Dataset      `json:",omitempty"`
	AllUsersDatasets      []*zfs.Dataset      `json:",omitempty"`
	AllPersistentDatasets []*zfs.Dataset      `json:",omitempty"`
	UnmanagedDatasets     []*zfs.Dataset      `json:",omitempty"`
}

type sortedDatasets []*zfs.Dataset

func (s sortedDatasets) Len() int { return len(s) }
func (s sortedDatasets) Less(i, j int) bool {
	return strings.ReplaceAll(s[i].Name, "@", "#") < strings.ReplaceAll(s[j].Name, "@", "#")
}
func (s sortedDatasets) Swap(i, j int) { s[i], s[j] = s[j], s[i] }

// MarshalJSON exports for json Marshmalling all private fields
func (ms Machines) MarshalJSON() ([]byte, error) {
	mt := Machinesdump{}

	mt.All = ms.all
	mt.Cmdline = ms.cmdline
	mt.Current = ms.current
	mt.NextState = ms.nextState

	ds := sortedDatasets(ms.allSystemDatasets)
	sort.Sort(ds)
	mt.AllSystemDatasets = ds

	ds = sortedDatasets(ms.allUsersDatasets)
	sort.Sort(ds)
	mt.AllUsersDatasets = ds

	ds = sortedDatasets(ms.allPersistentDatasets)
	sort.Sort(ds)
	mt.AllPersistentDatasets = ds

	ds = sortedDatasets(ms.unmanagedDatasets)
	sort.Sort(ds)
	mt.UnmanagedDatasets = ds

	return json.Marshal(mt)
}
