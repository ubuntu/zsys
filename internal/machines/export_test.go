package machines

import (
	"encoding/json"

	"github.com/ubuntu/zsys/internal/zfs"
)

const (
	RevertUserDataTag = zfsRevertUserDataTag
)

type MachinesTest struct {
	All               map[string]*Machine `json:",omitempty"`
	Current           *Machine            `json:",omitempty"`
	NextState         *State              `json:",omitempty"`
	AllSystemDatasets []zfs.Dataset       `json:",omitempty"`
	AllUsersDatasets  []zfs.Dataset       `json:",omitempty"`
}

// Export for json Marshmalling all private fields
func (m Machines) MarshalJSON() ([]byte, error) {
	mt := MachinesTest{}

	mt.All = m.all
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
	m.current = mt.Current
	m.nextState = mt.NextState
	m.allSystemDatasets = mt.AllSystemDatasets
	m.allUsersDatasets = mt.AllUsersDatasets

	return nil
}
