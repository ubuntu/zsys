package machines

import (
	"sort"
	"strings"
	"time"

	"github.com/k0kubun/pp"
	log "github.com/sirupsen/logrus"
	"github.com/ubuntu/zsys/internal/config"
	"github.com/ubuntu/zsys/internal/zfs"
)

// Machines hold a zfs system states, with a map of main root system dataset name to a given Machine,
// current machine and nextState if an upgrade has been proceeded.
type Machines struct {
	all               map[string]*Machine
	cmdline           string
	current           *Machine
	nextState         *State
	allSystemDatasets []zfs.Dataset
	allUsersDatasets  []zfs.Dataset
}

// Machine is a group of Main and its History children statees
type Machine struct {
	// Main machine State
	State
	// History is a map or root system datasets to all its possible State
	History map[string]*State `json:",omitempty"`
}

// State is a finite regroupement of multiple ID and elements corresponding to a bootable machine instance.
type State struct {
	// ID is the path to the root system dataset for this State.
	ID string
	// IsZsys states if we have a zsys system. The other datasets type will be empty otherwise.
	IsZsys bool `json:",omitempty"`
	// LastUsed is the last time this state was used
	LastUsed *time.Time `json:",omitempty"`
	// SystemDatasets are all datasets that constitutes this State (in <pool>/ROOT/ + <pool>/BOOT/)
	SystemDatasets []zfs.Dataset `json:",omitempty"`
	// UserDatasets are all datasets that are attached to the given State (in <pool>/USERDATA/)
	UserDatasets []zfs.Dataset `json:",omitempty"`
	// PersistentDatasets are all datasets that are canmount=on and and not in ROOT, USERDATA or BOOT dataset containers.
	// Those are common between all machines, as persistent (and detected without snapshot information)
	PersistentDatasets []zfs.Dataset `json:",omitempty"`
}

const (
	userdatasetsContainerName = "/userdata/"
)

// New detects and generate machines elems
func New(ds []zfs.Dataset, cmdline string) Machines {
	log.Infoln("building new machines list")
	machines := Machines{
		all:     make(map[string]*Machine),
		cmdline: cmdline,
	}

	// We are going to transform the origin of datasets, get a copy first
	datasets := make([]zfs.Dataset, len(ds))
	copy(datasets, ds)

	// Sort datasets so that children datasets are after their parents.
	sortedDataset := sortedDataset(datasets)
	sort.Sort(sortedDataset)

	// Resolve out to its root origin for /, /boot* and user datasets
	origins := resolveOrigin([]zfs.Dataset(sortedDataset))

	// First, set main datasets, then set clones
	mainDatasets := make([]zfs.Dataset, 0, len(sortedDataset))
	cloneDatasets := make([]zfs.Dataset, 0, len(sortedDataset))
	otherDatasets := make([]zfs.Dataset, 0, len(sortedDataset))
	for _, d := range sortedDataset {
		if origins[d.Name] == nil {
			otherDatasets = append(otherDatasets, d)
			continue
		}
		if *origins[d.Name] == "" {
			mainDatasets = append(mainDatasets, d)
		} else {
			cloneDatasets = append(cloneDatasets, d)
		}
	}

	// First, handle system datasets (active for each machine and history) and return remaining ones.
	boots, userdatas, persistents := machines.triageDatasets(append(append(mainDatasets, cloneDatasets...), otherDatasets...), origins)

	// Attach to machine zsys boots and userdata non persisent datasets per machines before attaching persistents.
	// Same with children and history datasets.
	// We want reproducibility, so iterate to attach datasets in a given order.
	for _, k := range sortedMachineKeys(machines.all) {
		m := machines.all[k]
		m.attachRemainingDatasets(boots, userdatas, persistents)

		// attach to global list all system datasets of this machine
		machines.allSystemDatasets = append(machines.allSystemDatasets, m.SystemDatasets...)
		for _, k := range sortedStateKeys(m.History) {
			h := m.History[k]
			machines.allSystemDatasets = append(machines.allSystemDatasets, h.SystemDatasets...)
		}
	}

	for _, d := range userdatas {
		if d.CanMount == "off" {
			continue
		}
		machines.allUsersDatasets = append(machines.allUsersDatasets, d)
	}

	// Append unlinked boot datasets to ensure we will switch to noauto everything
	machines.allSystemDatasets = appendIfNotPresent(machines.allSystemDatasets, boots, true)

	root, _ := bootParametersFromCmdline(cmdline)
	m, _ := machines.findFromRoot(root)
	machines.current = m

	if log.GetLevel() == log.DebugLevel {
		log.Debugln("current machines scanning layout:")
		pp.Println(machines)
	}

	return machines
}

// triageDatasets attach main system datasets to machines and returns other types of datasets for later triage/attachment.
func (machines *Machines) triageDatasets(allDatasets []zfs.Dataset, origins map[string]*string) (boots, userdatas, persistents []zfs.Dataset) {
	for _, d := range allDatasets {
		// Main active system dataset building up a machine
		m := newMachineFromDataset(d, origins[d.Name])
		if m != nil {
			machines.all[d.Name] = m
			continue
		}

		// Check for children, clones and snapshots
		if machines.attachSystemAndHistory(d, origins[d.Name]) {
			continue
		}

		// Starting from now, there is no children of system datasets

		// Extract boot datasets if any. We can't attach them directly with machines as if they are on another pool,
		// the machine is not necessiraly loaded yet.
		if strings.HasPrefix(d.Mountpoint, "/boot") {
			boots = append(boots, d)
			continue
		}

		// Extract zsys user datasets if any. We can't attach them directly with machines as if they are on another pool,
		// the machine is not necessiraly loaded yet.
		if strings.Contains(strings.ToLower(d.Name), userdatasetsContainerName) {
			userdatas = append(userdatas, d)
			continue
		}

		// At this point, it's either non zfs system or persistent dataset. Filters out canmount != "on" as nothing
		// will mount them.
		if d.CanMount != "on" {
			log.Debugf("ignoring %q: either an orphan clone or not a boot, user or system datasets and canmount isn't on", d.Name)
			continue
		}

		// should be persistent datasets
		persistents = append(persistents, d)
	}

	return boots, userdatas, persistents
}

// newMachineFromDataset returns a new machine if the given dataset is a main system one.
func newMachineFromDataset(d zfs.Dataset, origin *string) *Machine {
	// Register all zsys non cloned mountable / to a new machine
	if d.Mountpoint == "/" && d.CanMount != "off" && origin != nil && *origin == "" {
		m := Machine{
			State: State{
				ID:             d.Name,
				IsZsys:         d.BootFS,
				SystemDatasets: []zfs.Dataset{d},
			},
			History: make(map[string]*State),
		}
		// We don't want lastused to be 1970 in our golden files
		if d.LastUsed != 0 {
			lu := time.Unix(int64(d.LastUsed), 0)
			m.State.LastUsed = &lu
		}
		return &m
	}
	return nil
}

// attachSystemAndHistory identified if the given dataset is a system dataset (children of root one) or a history
// one. It creates and attach the states as needed.
// It returns ok if the dataset matches any machine and is attached.
func (machines *Machines) attachSystemAndHistory(d zfs.Dataset, origin *string) (ok bool) {
	for _, m := range machines.all {

		// Direct main machine state children
		if ok, err := isChild(m.ID, d); err != nil {
			log.Warningf("ignoring %q as couldn't assert if it's a child: "+config.ErrorFormat, d.Name, err)
		} else if ok {
			m.SystemDatasets = append(m.SystemDatasets, d)
			return true
		}

		// Clones or snapshot root dataset (origins points to origin dataset)
		if d.Mountpoint == "/" && d.CanMount != "off" && origin != nil && *origin == m.ID {
			m.History[d.Name] = &State{
				ID:             d.Name,
				IsZsys:         d.BootFS,
				SystemDatasets: []zfs.Dataset{d},
			}
			// We don't want lastused to be 1970 in our golden files
			if d.LastUsed != 0 {
				lu := time.Unix(int64(d.LastUsed), 0)
				m.History[d.Name].LastUsed = &lu
			}
			return true
		}

		// Clones or snapshot children
		for _, h := range m.History {
			if ok, err := isChild(h.ID, d); err != nil {
				log.Warningf("ignoring %q as couldn't assert if it's a child: "+config.ErrorFormat, d.Name, err)
			} else if ok {
				h.SystemDatasets = append(h.SystemDatasets, d)
				return true
			}
		}
	}

	return false
}

// attachRemainingDatasets attaches to machine boot, userdata and persistent datasets if they fit current machine.
func (m *Machine) attachRemainingDatasets(boots, userdatas, persistents []zfs.Dataset) {
	e := strings.Split(m.ID, "/")
	// machineDatasetID is the main State dataset ID.
	machineDatasetID := e[len(e)-1]

	// Boot datasets
	var bootsDataset []zfs.Dataset
	for _, d := range boots {
		if d.IsSnapshot {
			continue
		}
		// Matching base dataset name or subdataset of it.
		if strings.HasSuffix(d.Name, "/"+machineDatasetID) || strings.Contains(d.Name, "/"+machineDatasetID+"/") {
			bootsDataset = append(bootsDataset, d)
		}
	}
	m.SystemDatasets = append(m.SystemDatasets, bootsDataset...)

	// Userdata datasets. Don't base on machineID name as it's a tag on the dataset (the same userdataset can be
	// linked to multiple clones and systems).
	var userDatasets []zfs.Dataset
	for _, d := range userdatas {
		if d.IsSnapshot {
			continue
		}
		// Only match datasets corresponding to the linked bootfs datasets (string slice separated by :)
		for _, bootfsDataset := range strings.Split(d.BootfsDatasets, ":") {
			if bootfsDataset == m.ID || strings.HasPrefix(d.BootfsDatasets, m.ID+"/") {
				userDatasets = append(userDatasets, d)
			}
		}
	}
	m.UserDatasets = append(m.UserDatasets, userDatasets...)

	// Persistent datasets
	m.PersistentDatasets = persistents

	// Handle history now
	// We want reproducibility, so iterate to attach datasets in a given order.
	for _, k := range sortedStateKeys(m.History) {
		h := m.History[k]
		h.attachRemainingDatasetsForHistory(boots, userdatas, persistents)
	}
}

// attachRemainingDatasetsForHistory attaches to a given history state boot, userdata and persistent datasets if they fit.
// It's similar to attachRemainingDatasets with some particular rules on snapshots.
func (h *State) attachRemainingDatasetsForHistory(boots, userdatas, persistents []zfs.Dataset) {
	e := strings.Split(h.ID, "/")
	// stateDatasetID may contain @snapshot, which we need to strip to test the suffix
	stateDatasetID := e[len(e)-1]
	var snapshot string
	if j := strings.LastIndex(stateDatasetID, "@"); j > 0 {
		snapshot = stateDatasetID[j+1:]
	}

	// Boot datasets
	var bootsDataset []zfs.Dataset
	for _, d := range boots {
		if snapshot != "" {
			// Snapshots are not necessarily with a dataset ID matching its parent of dataset promotions, just match
			// its name.
			if strings.HasSuffix(d.Name, "@"+snapshot) {
				bootsDataset = append(bootsDataset, d)
				continue
			}
		}
		// For clones just match the base datasetname or its children.
		if strings.HasSuffix(d.Name, stateDatasetID) || strings.Contains(d.Name, "/"+stateDatasetID+"/") {
			bootsDataset = append(bootsDataset, d)
		}
	}
	h.SystemDatasets = append(h.SystemDatasets, bootsDataset...)

	// Userdata datasets. Don't base on machineID name as it's a tag on the dataset (the same userdataset can be
	// linked to multiple clones and systems).
	var userDatasets []zfs.Dataset
	for _, d := range userdatas {
		if snapshot != "" {
			// Snapshots won't match dataset ID matching its system dataset as multiple system datasets can link
			// to the same user dataset. Use only snapshot name.
			if strings.HasSuffix(d.Name, "@"+snapshot) {
				userDatasets = append(userDatasets, d)
				continue
			}
		}

		if d.IsSnapshot {
			continue
		}
		// For clones, proceed as with main system:
		// Only match datasets corresponding to the linked bootfs datasets (string slice separated by :)
		for _, bootfsDataset := range strings.Split(d.BootfsDatasets, ":") {
			if bootfsDataset == h.ID || strings.HasPrefix(d.BootfsDatasets, h.ID+"/") {
				userDatasets = append(userDatasets, d)
			}
		}
	}
	h.UserDatasets = append(h.UserDatasets, userDatasets...)

	// Persistent datasets
	h.PersistentDatasets = persistents
}
