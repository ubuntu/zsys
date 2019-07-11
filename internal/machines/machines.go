package machines

import (
	"sort"
	"strings"

	"github.com/ubuntu/zsys/internal/config"

	log "github.com/sirupsen/logrus"
	"github.com/ubuntu/zsys/internal/zfs"
	"golang.org/x/xerrors"
)

// Machines is the map of main root system dataset name to a given Machine
type Machines map[string]*Machine

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
	// SystemDatasets are all datasets that constitues this State (in <pool>/ROOT/ + <pool>/BOOT/)
	SystemDatasets []zfs.Dataset `json:",omitempty"`
	// UserDatasets are all datasets that are attached to the given State (in <pool>/USERDATA/)
	UserDatasets []zfs.Dataset `json:",omitempty"`
	// PersistentDatasets are all datasets that are canmount=on and and not in ROOT, USERDATA or BOOT dataset containers.
	// Those are common between all machines, as persistent (and detected without snapshot information)
	PersistentDatasets []zfs.Dataset `json:",omitempty"`
}

var (
	current *State
	next    *State
)

// sortDataset enables sorting a slice of Dataset elements.
type sortedDataset []zfs.Dataset

func (s sortedDataset) Len() int           { return len(s) }
func (s sortedDataset) Less(i, j int) bool { return s[i].Name < s[j].Name }
func (s sortedDataset) Swap(i, j int)      { s[i], s[j] = s[j], s[i] }

// isChild returns if a dataset d is a child of name.
// An error will mean that the dataset name isn't what we expected it to be.
func isChild(name string, d zfs.Dataset) (bool, error) {
	names := strings.Split(name, "@")
	var err error
	switch len(names) {
	// direct system or clone child
	case 1:
		if strings.HasPrefix(d.Name, names[0]+"/") {
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
// This is only done for /, /boot* and /home* datasets for performance reasons.
func resolveOrigin(sortedDataset *sortedDataset) {
	for i := range *sortedDataset {
		curDataset := &((*sortedDataset)[i])
		if curDataset.Origin == "" ||
			(curDataset.Mountpoint != "/" && !strings.HasPrefix(curDataset.Mountpoint, "/boot") &&
				!strings.HasPrefix(curDataset.Mountpoint, "/home")) {
			continue
		}

	nextOrigin:
		for {
			// origin for a clone points to a snapshot, points to the origin or clone
			if j := strings.LastIndex(curDataset.Origin, "@"); j > 0 {
				curDataset.Origin = curDataset.Origin[:j]
			}

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
func New(ds []zfs.Dataset) Machines {
	machines := make(Machines)

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
				State: State{
					ID:             d.Name,
					IsZsys:         d.BootFS == "yes",
					SystemDatasets: []zfs.Dataset{d},
				},
				History: make(map[string]*State),
			}
			machines[d.Name] = &m
			continue
		}

		// Check for children, clones and snapshots
		for _, m := range machines {
			// Direct children
			if ok, err := isChild(m.ID, d); err != nil {
				log.Warningf("ignoring %q as couldn't assert if it's a child: "+config.ErrorFormat, d.Name, err)
			} else if ok {
				m.SystemDatasets = append(m.SystemDatasets, d)
				continue nextDataset
			}

			// Clones root dataset (origin has been modified to point to origin dataset)
			if strings.HasPrefix(d.Origin, m.ID) {
				m.History[d.Name] = &State{
					ID:             d.Name,
					SystemDatasets: []zfs.Dataset{d},
				}
				continue nextDataset
			}

			// Snapshots root dataset.
			// This is possible because we ascii ordered the list of zfs datasets, and so main State and clone root
			// of a given snapshot will have been already treated.
			// 1. test if snapshot of machine main State dataset. We would have encountered the machine main State dataset first.
			if strings.HasPrefix(d.Name, m.ID+"@") {
				m.History[d.Name] = &State{
					ID:             d.Name,
					IsZsys:         d.BootFS == "yes",
					SystemDatasets: []zfs.Dataset{d},
				}
				continue nextDataset
			}
			// 2. test if snapshot of any cloned root dataset. We would have encountered the main clone State dataset first.
			for _, h := range m.History {
				if strings.HasPrefix(d.Name, h.ID+"@") {
					m.History[d.Name] = &State{
						ID:             d.Name,
						IsZsys:         d.BootFS == "yes",
						SystemDatasets: []zfs.Dataset{d},
					}
					continue nextDataset
				}
			}

			// Clones or snapshot children
			for _, h := range m.History {
				if ok, err := isChild(h.ID, d); err != nil {
					log.Warningf("ignoring %q as couldn't assert if it's a child: "+config.ErrorFormat, d.Name, err)
				} else if ok {
					h.SystemDatasets = append(h.SystemDatasets, d)
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
	for _, m := range machines {
		e := strings.Split(m.ID, "/")
		// machineDatasetID is the main State dataset ID.
		machineDatasetID := e[len(e)-1]

		// Boot datasets
		var bootsDataset []zfs.Dataset
		for _, d := range boots {
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
		for lu, h := range m.History {
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
					// Snapshots are not necessarily with a dataset ID maching its parent of dataset promotions, just match
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
					// Snapshots wo'nt match dataset ID maching its system dataset as multiple system datasets can link
					// to the same user dataset. Use only snapshot name.
					if strings.HasSuffix(d.Name, "@"+snapshot) {
						userDatasets = append(userDatasets, d)
						continue
					}
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

			m.History[lu] = h
		}
	}

	return machines
}
