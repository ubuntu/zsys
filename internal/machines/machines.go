package machines

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/k0kubun/pp"
	"github.com/ubuntu/zsys/internal/config"
	"github.com/ubuntu/zsys/internal/i18n"
	"github.com/ubuntu/zsys/internal/log"
	"github.com/ubuntu/zsys/internal/zfs"
)

// Machines hold a zfs system states, with a map of main root system dataset name to a given Machine,
// current machine and nextState if an upgrade has been proceeded.
type Machines struct {
	all               map[string]*Machine
	cmdline           string
	current           *Machine
	nextState         *State
	allSystemDatasets []*zfs.Dataset
	allUsersDatasets  []*zfs.Dataset

	z *zfs.Zfs
}

// Machine is a group of Main and its History children states
type Machine struct {
	// Main machine State
	State
	// Users is a per user reference to each of its state
	Users map[string]map[string][]*zfs.Dataset `json:",omitempty"`
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
	SystemDatasets []*zfs.Dataset `json:",omitempty"`
	// UserDatasets are all datasets that are attached to the given State (in <pool>/USERDATA/)
	UserDatasets []*zfs.Dataset `json:",omitempty"`
	// PersistentDatasets are all datasets that are canmount=on and and not in ROOT, USERDATA or BOOT dataset containers.
	// Those are common between all machines, as persistent (and detected without snapshot information)
	PersistentDatasets []*zfs.Dataset `json:",omitempty"`
}

// machineAndState is an internal helper to associate a dataset path to a machine and state
type machineAndState struct {
	state   *State
	machine *Machine
}

const (
	userdatasetsContainerName = "/userdata/"
)

// WithLibZFS allows overriding default libzfs implementations with a mock
func WithLibZFS(libzfs zfs.LibZFSInterface) func(o *options) error {
	return func(o *options) error {
		o.libzfs = libzfs
		return nil
	}
}

type options struct {
	libzfs zfs.LibZFSInterface
}

type option func(*options) error

// New detects and generate machines elems
func New(ctx context.Context, cmdline string, opts ...option) (Machines, error) {
	log.Info(ctx, i18n.G("Building new machines list"))
	args := options{
		libzfs: &zfs.LibZFSAdapter{},
	}
	for _, o := range opts {
		if err := o(&args); err != nil {
			return Machines{}, fmt.Errorf(i18n.G("Couldn't apply option to server: %v"), err)
		}
	}

	z, err := zfs.New(ctx, zfs.WithLibZFS(args.libzfs))
	if err != nil {
		return Machines{}, fmt.Errorf(i18n.G("couldn't scan zfs filesystem"), err)
	}

	machines := Machines{
		all:     make(map[string]*Machine),
		cmdline: cmdline,
		z:       z,
	}
	if err := machines.refresh(ctx); err != nil {
		return Machines{}, err
	}

	return machines, nil
}

// Refresh reloads the list of machines after rescanning zfs datasets state from system
func (machines *Machines) Refresh(ctx context.Context) error {
	newMachines := Machines{
		all:     make(map[string]*Machine),
		cmdline: machines.cmdline,
		z:       machines.z,
	}
	if err := newMachines.z.Refresh(ctx); err != nil {
		return err
	}

	if err := newMachines.refresh(ctx); err != nil {
		return err
	}

	*machines = newMachines
	return nil
}

// refresh reloads the list of machines, based on already loaded zfs datasets state
func (machines *Machines) refresh(ctx context.Context) error {
	// We are going to transform the origin of datasets, get a copy first
	zDatasets := machines.z.Datasets()
	datasets := make([]*zfs.Dataset, 0, len(zDatasets))
	for i := range zDatasets {
		datasets = append(datasets, &zDatasets[i])
	}

	// Sort datasets so that children datasets are after their parents.
	sortedDataset := sortedDataset(datasets)
	sort.Sort(sortedDataset)

	// Resolve out to its root origin for /, /boot* and user datasets
	origins := resolveOrigin(ctx, []*zfs.Dataset(sortedDataset), "/")

	// First, set main datasets, then set clones
	mainDatasets := make([]zfs.Dataset, 0, len(sortedDataset))
	cloneDatasets := make([]zfs.Dataset, 0, len(sortedDataset))
	otherDatasets := make([]zfs.Dataset, 0, len(sortedDataset))
	for _, d := range sortedDataset {
		if origins[d.Name] == nil {
			otherDatasets = append(otherDatasets, *d)
			continue
		}
		if *origins[d.Name] == "" {
			mainDatasets = append(mainDatasets, *d)
		} else {
			cloneDatasets = append(cloneDatasets, *d)
		}
	}

	// First, handle system datasets (active for each machine and history) and return remaining ones.
	mns, boots, flattenedUserDatas, persistents := machines.populate(ctx, append(append(mainDatasets, cloneDatasets...), otherDatasets...), origins)

	// Get a userdata map from parent to its children
	rootUserDatasets := getRootDatasets(ctx, flattenedUserDatas)

	var rootsOnlyUserDatasets []*zfs.Dataset
	for k := range rootUserDatasets {
		rootsOnlyUserDatasets = append(rootsOnlyUserDatasets, k)
	}
	originsUserDatasets := resolveOrigin(ctx, rootsOnlyUserDatasets, "")

	for r, children := range rootUserDatasets {
		// Handle snapshots userdatasets
		if r.IsSnapshot {
			for n, ms := range mns {
				// Snapshots are not necessarily with a dataset ID matching its parent of dataset promotions, just match
				// its name.
				_, snapshot := splitSnapshotName(r.Name)
				if strings.HasSuffix(n, snapshot) {
					ms.machine.addUserDatasets(ctx, r, children, ms.state)
					continue
				}
			}
		}

		// Handle regular userdatasets (main or clone), associated to a system state
		if !r.IsSnapshot {
			var associateWithAtLeastOne bool
			for n, ms := range mns {
				var isAssociated bool
				for _, bootfsDataset := range strings.Split(r.BootfsDatasets, ":") {
					if bootfsDataset == n || strings.HasPrefix(r.BootfsDatasets, n+"/") {
						isAssociated = true
						break
					}
				}
				if !isAssociated {
					continue
				}

				// TODO: Verify that we have tests that cover the case:
				// 		- root is associated to 2 states
				//		- children are associated with only 1
				associateWithAtLeastOne = true
				var associatedChildren []*zfs.Dataset
				for _, d := range children {
					isAssociated = false
					for _, bootfsDataset := range strings.Split(d.BootfsDatasets, ":") {
						if bootfsDataset == n || strings.HasPrefix(d.BootfsDatasets, n+"/") {
							isAssociated = true
							break
						}
					}
					if !isAssociated {
						continue
					}
					associatedChildren = append(associatedChildren, d)
				}
				ms.machine.addUserDatasets(ctx, r, associatedChildren, ms.state)
			}

			if associateWithAtLeastOne {
				continue
			}
		}

		// Handle regular userdatasets (main or clone), not associated to a system state.
		// This is a userdataset "snapshot" (clone or snapshot).
		// WARNING: We only consider the dataset "group" (clones and promoted) attached to main state of a given machine
		// to regroup on a known machine.
		if r.IsSnapshot {
			base, _ := splitSnapshotName(r.Name)
			var associated bool
			for _, m := range machines.all {
				t := strings.Split(filepath.Base(base), "_")
				user := t[0]
				if len(t) > 1 {
					user = strings.Join(t[:len(t)-1], "_")
				}
				for _, ds := range m.Users[user] {
					if ds[0].Name == base {
						m.addUserDatasets(ctx, r, children, nil)
						associated = true
						break
					}
				}
				// we don’t break here, as a userdata only snapshot can be associated with multiple machines
			}
			if !associated {
				log.Warningf(ctx, i18n.G("Couldn’t find any association for user dataset %s"), r.Name)
			}
			continue
		}

		origin := *(originsUserDatasets[r.Name])
		// This is manual promotion from the user on a user dataset without promoting the whole state:
		// ignore the dataset and issue a warning.
		// If we iterated over all the user datasets from all machines and states, we may find a match, but ignore
		// for now as this is already a manual user interaction.
		if origin == "" {
			log.Warningf(ctx, i18n.G("Couldn’t find any association for user dataset %s"), r.Name)
			continue
		}

		var associateWithAtLeastOne bool
		for _, m := range machines.all {
			var associated bool
			for _, userStates := range m.Users {
				for _, userState := range userStates {
					if userState[0].Name == origin {
						m.addUserDatasets(ctx, r, children, nil)
						associated = true
						associateWithAtLeastOne = true
						break
					}
				}
				// Go on on other machines (same user "snapshot" datasets can be associated to multiple machines)
				if associated {
					break
				}
			}
		}

		if !associateWithAtLeastOne {
			log.Warningf(ctx, i18n.G("Couldn’t find any association for user dataset %s"), r.Name)
		}
	}

	for _, d := range flattenedUserDatas {
		if d.CanMount == "off" {
			continue
		}
		machines.allUsersDatasets = append(machines.allUsersDatasets, d)
	}

	// Attach to machine zsys boots and userdata non persisent datasets per machines before attaching persistents.
	// Same with children and history datasets.
	// We want reproducibility, so iterate to attach datasets in a given order.
	for _, k := range sortedMachineKeys(machines.all) {
		m := machines.all[k]
		m.attachRemainingDatasets(ctx, boots, persistents)

		// attach to global list all system datasets of this machine
		machines.allSystemDatasets = append(machines.allSystemDatasets, m.SystemDatasets...)
		for _, k := range sortedStateKeys(m.History) {
			h := m.History[k]
			machines.allSystemDatasets = append(machines.allSystemDatasets, h.SystemDatasets...)
		}
	}

	// Append unlinked boot datasets to ensure we will switch to noauto everything
	machines.allSystemDatasets = appendIfNotPresent(machines.allSystemDatasets, boots, true)

	root, _ := bootParametersFromCmdline(machines.cmdline)
	m, _ := machines.findFromRoot(root)
	machines.current = m

	log.Debugf(ctx, i18n.G("current machines scanning layout:\n"+pp.Sprint(machines)))

	return nil
}

// populate attach main system datasets to machines and returns other types of datasets for later triage/attachment, alongside
// a map to direct access to a given state and machine
func (machines *Machines) populate(ctx context.Context, allDatasets []zfs.Dataset, origins map[string]*string) (mns map[string]machineAndState, boots, userdatas, persistents []*zfs.Dataset) {
	mns = make(map[string]machineAndState)

	for _, d := range allDatasets {
		// we are taking the d address. Ensure we have a local variable that isn’t going to be reused
		d := d
		// Main active system dataset building up a machine
		m := newMachineFromDataset(d, origins[d.Name])
		if m != nil {
			machines.all[d.Name] = m
			mns[d.Name] = machineAndState{
				state:   &m.State,
				machine: m,
			}
			continue
		}

		// Check for children, clones and snapshots
		if machines.populateSystemAndHistory(ctx, d, origins[d.Name], &mns) {
			continue
		}

		// Starting from now, there is no children of system datasets

		// Extract boot datasets if any. We can't attach them directly with machines as if they are on another pool,
		// the machine is not necessiraly loaded yet.
		if strings.HasPrefix(d.Mountpoint, "/boot") {
			boots = append(boots, &d)
			continue
		}

		// Extract zsys user datasets if any. We can't attach them directly with machines as if they are on another pool,
		// the machine is not necessiraly loaded yet.
		if strings.Contains(strings.ToLower(d.Name), userdatasetsContainerName) {
			userdatas = append(userdatas, &d)
			continue
		}

		// At this point, it's either non zfs system or persistent dataset. Filters out canmount != "on" as nothing
		// will mount them.
		if d.CanMount != "on" {
			log.Debugf(ctx, i18n.G("ignoring %q: either an orphan clone or not a boot, user or system datasets and canmount isn't on"), d.Name)
			continue
		}

		// should be persistent datasets
		persistents = append(persistents, &d)
	}

	return mns, boots, userdatas, persistents
}

// newMachineFromDataset returns a new machine if the given dataset is a main system one.
func newMachineFromDataset(d zfs.Dataset, origin *string) *Machine {
	// Register all zsys non cloned mountable / to a new machine
	if d.Mountpoint == "/" && d.CanMount != "off" && origin != nil && *origin == "" {
		m := Machine{
			State: State{
				ID:             d.Name,
				IsZsys:         d.BootFS,
				SystemDatasets: []*zfs.Dataset{&d},
			},
			Users:   make(map[string]map[string][]*zfs.Dataset),
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

// populateSystemAndHistory identified if the given dataset is a system dataset (children of root one) or a history
// one. It creates and attach the states as needed.
// It returns ok if the dataset matches any machine and is attached.
func (machines *Machines) populateSystemAndHistory(ctx context.Context, d zfs.Dataset, origin *string, mns *map[string]machineAndState) (ok bool) {
	for _, m := range machines.all {

		// Direct main machine state children
		if ok, err := isChild(m.ID, d); err != nil {
			log.Warningf(ctx, i18n.G("ignoring %q as couldn't assert if it's a child: ")+config.ErrorFormat, d.Name, err)
		} else if ok {
			m.SystemDatasets = append(m.SystemDatasets, &d)
			return true
		}

		// Clones or snapshot root dataset (origins points to origin dataset)
		if d.Mountpoint == "/" && d.CanMount != "off" && origin != nil && *origin == m.ID {
			s := &State{
				ID:             d.Name,
				IsZsys:         d.BootFS,
				SystemDatasets: []*zfs.Dataset{&d},
			}
			m.History[d.Name] = s
			// We don't want lastused to be 1970 in our golden files
			if d.LastUsed != 0 {
				lu := time.Unix(int64(d.LastUsed), 0)
				m.History[d.Name].LastUsed = &lu
			}
			(*mns)[d.Name] = machineAndState{
				state:   s,
				machine: m,
			}
			return true
		}

		// Clones or snapshot children
		for _, h := range m.History {
			if ok, err := isChild(h.ID, d); err != nil {
				log.Warningf(ctx, i18n.G("ignoring %q as couldn't assert if it's a child: ")+config.ErrorFormat, d.Name, err)
			} else if ok {
				h.SystemDatasets = append(h.SystemDatasets, &d)
				return true
			}
		}
	}

	return false
}

// addUserDatasets attach user datasets to a given state and append to the map of all users on the machine
func (m *Machine) addUserDatasets(ctx context.Context, r *zfs.Dataset, children []*zfs.Dataset, state *State) {
	// Add dataset to given state
	if state != nil {
		state.UserDatasets = append(state.UserDatasets, r)
		state.UserDatasets = append(state.UserDatasets, children...)
	}

	// Extract user name
	t := strings.Split(filepath.Base(r.Name), "_")
	user := t[0]
	if len(t) > 1 {
		user = strings.Join(t[:len(t)-1], "_")
	}
	if m.Users[user] == nil {
		m.Users[user] = make(map[string][]*zfs.Dataset)
	}

	// Create dataset with state timestamp
	var timestamp string
	if state != nil && m.ID == state.ID {
		timestamp = "current"
	} else {
		timestamp = strconv.Itoa(r.LastUsed)
	}

	if d, ok := m.Users[user][timestamp]; ok {
		if d[0].Name != r.Name {
			log.Warningf(ctx, i18n.G("User %s has already a user snapshot attached at %s (%s) for machine %s. Can’t add %s"), user, timestamp, d[0].Name, r.Name)
		}
		return
	}

	// Attach to global user map
	m.Users[user][timestamp] = append([]*zfs.Dataset{r}, children...)
}

// attachRemainingDatasets attaches to machine boot and persistent datasets if they fit current machine.
func (m *Machine) attachRemainingDatasets(ctx context.Context, boots, persistents []*zfs.Dataset) {
	e := strings.Split(m.ID, "/")
	// machineDatasetID is the main State dataset ID.
	machineDatasetID := e[len(e)-1]

	// Boot datasets
	var bootsDataset []*zfs.Dataset
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

	// Persistent datasets
	m.PersistentDatasets = persistents

	// Handle history now
	// We want reproducibility, so iterate to attach datasets in a given order.
	for _, k := range sortedStateKeys(m.History) {
		h := m.History[k]
		h.attachRemainingDatasetsForHistory(boots, persistents)
	}
	// TODO: REMOVE
	m.Users = nil
}

// attachRemainingDatasetsForHistory attaches to a given history state boot and persistent datasets if they fit.
// It's similar to attachRemainingDatasets with some particular rules on snapshots.
func (h *State) attachRemainingDatasetsForHistory(boots, persistents []*zfs.Dataset) {
	e := strings.Split(h.ID, "/")
	// stateDatasetID may contain @snapshot, which we need to strip to test the suffix
	stateDatasetID := e[len(e)-1]
	var snapshot string
	if j := strings.LastIndex(stateDatasetID, "@"); j > 0 {
		snapshot = stateDatasetID[j+1:]
	}

	// Boot datasets
	var bootsDataset []*zfs.Dataset
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

	// Persistent datasets
	h.PersistentDatasets = persistents
}

// isZsys returns if there is a current machine, and if it's the case, if it's zsys.
func (m *Machine) isZsys() bool {
	if m == nil {
		return false
	}
	return m.IsZsys
}
