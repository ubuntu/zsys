package machines

import (
	"sort"
	"strings"

	"github.com/ubuntu/zsys/internal/config"

	log "github.com/sirupsen/logrus"
	"github.com/ubuntu/zsys/internal/zfs"
	"golang.org/x/xerrors"
)

// Machine is the machine element
type Machine struct {
	// ID is the path to the root system dataset
	ID string
	// IsZsys states if we have a zsys system. The other datasets type will be empty otherwise.
	IsZsys bool `json:",omitempty"`
	// CanBeEnabled is for noauto or on dataset groups
	CanBeEnabled bool `json:",omitempty"`
	machineDatasets
	// History is a map, by LastUsed of all other system datasets of this machine.
	History map[int]HistoryMachine `json:",omitempty"`
}

// HistoryMachine is the different state of a machine
type HistoryMachine struct {
	// ID is the path to the root system dataset
	ID string
	machineDatasets
}

// machieInternal is the common implementation between Machine and HistoryMachine
type machineDatasets struct {
	// SystemDatasets are all datasets that constitues a machine (in <pool>/ROOT/ + <pool>/BOOT/)
	SystemDatasets []zfs.Dataset `json:",omitempty"`
	// UserDatasets are all datasets that are attached to the given machine (in <pool>/USERDATA/)
	UserDatasets []zfs.Dataset `json:",omitempty"`
	// PersistentDatasets are all datasets that are canmount=on and and not in ROOT, USERDATA or BOOT dataset containers.
	// Those are common between machine and history, as persistent (and detected without snapshot information)
	PersistentDatasets []zfs.Dataset `json:",omitempty"`
}

var (
	current *Machine
	next    *Machine
)

// sortDataset enables sorting a slice of Dataset elements.
type sortedDataset []zfs.Dataset

func (s sortedDataset) Len() int           { return len(s) }
func (s sortedDataset) Less(i, j int) bool { return s[i].Name < s[j].Name }
func (s sortedDataset) Swap(i, j int)      { s[i], s[j] = s[j], s[i] }

// addIfChildren append dataset d (a copy) to a slice of datasets if its name is a children
// of the given name. Return true if match, false otherwise
// an error will mean that the dataset name isn't what we expected it to be.
func addIfChildren(name string, d zfs.Dataset, dest *[]zfs.Dataset) (bool, error) {
	names := strings.Split(name, "@")
	var err error
	switch len(names) {
	// direct system or clone child
	case 1:
		if strings.HasPrefix(d.Name, names[0]+"/") {
			*dest = append(*dest, d)
			return true, nil
		}
	// snapshot child
	case 2:
		if strings.HasPrefix(d.Name, names[0]+"/") && strings.HasSuffix(d.Name, "@"+names[1]) {
			*dest = append(*dest, d)
			return true, nil
		}
	default:
		err = xerrors.Errorf("unexpected number of @ in dataset name %q", d.Name)
	}
	return false, err
}

// resolveOrigin iterate over each datasets up to their true origin and replace them for / and /home* datasets
func resolveOrigin(sortedDataset *sortedDataset) {
	for i := range *sortedDataset {
		curDataset := &((*sortedDataset)[i])
		if curDataset.Origin == "" || (curDataset.Mountpoint != "/" && !strings.HasPrefix(curDataset.Mountpoint, "/home")) {
			continue
		}

	nextOrigin:
		for {
			originStart := curDataset.Origin
			for _, d := range *sortedDataset {
				if curDataset.Origin != d.Name {
					continue
				}
				if d.Origin != "" {
					curDataset.Origin = d.Origin
					break
				}
				break nextOrigin
			}
			if originStart == curDataset.Origin {
				log.Warningf("didn't find origin %q for %q matching any dataset", curDataset.Origin, curDataset.Name)
			}
		}
	}
}

// New detects and generate machines elems
func New(ds []zfs.Dataset) []Machine {
	var machines []Machine

	// We are going to transform the origin of datasets, get a copy first
	datasets := make([]zfs.Dataset, len(ds))
	copy(datasets, ds)

	// Sort datasets so that children datasets are after their parents.
	sortedDataset := sortedDataset(datasets)
	sort.Sort(sortedDataset)

	// Resolve out to its root origin for / and /home datasets
	resolveOrigin(&sortedDataset)

	var boots, userdatas, persistents []zfs.Dataset
	// First, handle system datasets (active for each machine and history)
nextDataset:
	for _, d := range sortedDataset {
		// Register all zsys non cloned mountable / to a new machine
		if d.Mountpoint == "/" && d.Origin == "" {
			// TODO: if canmount == off, we look at all children and don't add it if there is one with / mountpoint (it's only a container)
			m := Machine{
				ID:              d.Name,
				IsZsys:          d.BootFS == "yes",
				CanBeEnabled:    d.CanMount != "off",
				machineDatasets: machineDatasets{SystemDatasets: []zfs.Dataset{d}},
				History:         make(map[int]HistoryMachine),
			}
			machines = append(machines, m)
			continue
		}

		// Check for children, clones and snapshots
		for i := range machines {
			m := &machines[i]

			// Direct children
			if isChildren, err := addIfChildren(m.ID, d, &m.SystemDatasets); err != nil {
				log.Warningf("ignoring %q as couldn't assert if it's a child: "+config.ErrorFormat, d.Name, err)
			} else if isChildren {
				m.SystemDatasets = append(m.SystemDatasets, d)
				continue nextDataset
			}

			// Clones (origin has been modified to point to origin dataset)
			if strings.HasPrefix(d.Origin, m.ID) {
				m.History[d.LastUsed] = HistoryMachine{
					ID:              d.Name,
					machineDatasets: machineDatasets{SystemDatasets: []zfs.Dataset{d}},
				}
				continue nextDataset
			}

			// Snapshots or children snapshots of main dataset or clones
			if strings.HasPrefix(d.Name, m.ID+"@") {
				m.History[d.LastUsed] = HistoryMachine{
					ID:              d.Name,
					machineDatasets: machineDatasets{SystemDatasets: []zfs.Dataset{d}},
				}
				continue nextDataset
			}

			// Clones or snapshot children
			for lastused := range m.History {
				// This is a map, and so, not addressable, have to reassign
				h := m.History[lastused]
				if isChildren, err := addIfChildren(m.ID, d, &m.SystemDatasets); err != nil {
					log.Warningf("ignoring %q as couldn't assert if it's a child: "+config.ErrorFormat, d.Name, err)
				} else if isChildren {
					m.History[lastused] = h
					continue nextDataset
				}
			}
		}

		// Starting from now, there is no children of system dataset

		// Extract boot datasets if any. We can't attach them directly with machines as if they are on another pool,
		// the machine is not necessiraly loaded yet.
		if strings.HasPrefix(d.Mountpoint, "/boot") {
			boots = append(boots, d)
			continue
		}

		// Extract zsys user datasets if any. We can't attach them directly with machines as if they are on another pool,
		// the machine is not necessiraly loaded yet.
		if strings.HasPrefix(d.Mountpoint, "/home") && strings.Contains(strings.ToLower(d.Name), "/userdata/") {
			userdatas = append(userdatas, d)
			continue
		}

		// At this point, it's either non zfs system or persistent dataset. Filters out canmount != "on" as nothing
		// will mount them.
		if d.CanMount != "on" {
			log.Debugf("ignoring %q: not a boot, user or system datasets and canmount isn't on", d.Name)
			continue
		}

		// should be persistent datasets
		persistents = append(persistents)
	}

	// Attach to machine zsys boots and userdata non persisent datasets per machines before attaching persistents.
	// Same with children and history datasets.
	for i := range machines {
		m := &machines[i]
		e := strings.Split(m.ID, "/")
		machineDatasetID := e[len(e)-1]

		// Boot datasets
		var defaultBootsDataset []zfs.Dataset
		for _, d := range boots {
			if strings.HasSuffix(d.Name, "/"+machineDatasetID) || strings.Contains(d.Name, "/"+machineDatasetID+"/") {
				defaultBootsDataset = append(defaultBootsDataset, d)
			}
		}
		m.SystemDatasets = append(m.SystemDatasets, defaultBootsDataset...)

		// Userdata datasets
		var defaultUserDatasets, untaggedUserDatasets []zfs.Dataset
		for _, d := range userdatas {
			// Store untagged user datas, but prefer specifically tagged ones if any
			if d.SystemDataset == "" {
				untaggedUserDatasets = append(untaggedUserDatasets, d)
				continue
			}
			if d.SystemDataset == m.ID {
				defaultUserDatasets = append(defaultUserDatasets, d)
				continue
			}
		}
		if defaultUserDatasets == nil {
			defaultUserDatasets = untaggedUserDatasets
		}
		m.UserDatasets = append(defaultUserDatasets)

		// Persistent datasets
		m.PersistentDatasets = persistents

		// Handle history now
		for lu, h := range m.History {
			e := strings.Split(h.ID, "/")
			// machineDatasetID may contain @snapshot, which we need to strip to test the suffix
			machineDatasetID := e[len(e)-1]
			var baseMachineDatasetID, snapshot string
			if j := strings.LastIndex(machineDatasetID, "@"); j > 0 {
				baseMachineDatasetID = machineDatasetID[:j]
				snapshot = machineDatasetID[j+1:]
			}

			// Boot datasets
			var bootsDataset []zfs.Dataset
			for _, d := range boots {
				if strings.HasSuffix(d.Name, machineDatasetID) ||
					(strings.Contains(d.Name, "/"+baseMachineDatasetID+"/") && strings.HasSuffix(d.Name, snapshot)) {
					bootsDataset = append(bootsDataset, d)
				}
			}
			if bootsDataset != nil {
				h.SystemDatasets = append(h.SystemDatasets, bootsDataset...)
			} else {
				// fallback to default main dataset
				h.SystemDatasets = append(h.SystemDatasets, defaultBootsDataset...)
			}

			// Userdata datasets
			var userDataset []zfs.Dataset
			for _, d := range userdatas {
				if strings.HasSuffix(d.Name, machineDatasetID) ||
					(strings.Contains(d.Name, "/"+baseMachineDatasetID+"/") && strings.HasSuffix(d.Name, snapshot)) {
					userDataset = append(userDataset, d)
				}
			}
			if userDataset != nil {
				h.UserDatasets = append(h.UserDatasets, userDataset...)
			} else {
				// fallback to default main dataset
				h.UserDatasets = append(h.UserDatasets, defaultUserDatasets...)
			}

			// Persistent datasets
			h.PersistentDatasets = persistents

			m.History[lu] = h
		}
	}

	return machines
}
