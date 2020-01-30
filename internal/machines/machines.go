package machines

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
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
	// cantmount noauto or off datasets, which are not system, users or persistent
	unmanagedDatasets []*zfs.Dataset

	z *zfs.Zfs
}

// Machine is a group of Main and its History children states
type Machine struct {
	// Main machine State
	State
	// Users is a per user reference to each of its state
	Users map[string]map[string]userState `json:",omitempty"`
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
	// SystemDatasets are all datasets that constitutes this State (in <pool>/ROOT/ + <pool>/BOOT/).
	// The map index is each route for datasets.
	SystemDatasets map[string][]*zfs.Dataset `json:",omitempty"`
	// UserDatasets are all datasets that are attached to the given State (in <pool>/USERDATA/)
	UserDatasets map[string][]*zfs.Dataset `json:",omitempty"`
	// PersistentDatasets are all datasets that are canmount=on and and not in ROOT, USERDATA or BOOT dataset containers.
	// Those are common between all machines, as persistent (and detected without snapshot information)
	PersistentDatasets []*zfs.Dataset `json:",omitempty"`
}

// machineAndState is an internal helper to associate a dataset path to a machine and state
type machineAndState struct {
	state   *State
	machine *Machine
}

type userState struct {
	ID       string
	LastUsed *time.Time `json:",omitempty"`
	Datasets []*zfs.Dataset
}

const (
	userdatasetsContainerName = "/userdata/"
	bootfsdatasetsSeparator   = ","
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
	machines.refresh(ctx)
	return machines, nil
}

// Refresh reloads the list of machines after rescanning zfs datasets state from system
func (ms *Machines) Refresh(ctx context.Context) error {
	if err := ms.z.Refresh(ctx); err != nil {
		return err
	}

	ms.refresh(ctx)
	return nil
}

// refresh reloads the list of machines, based on already loaded zfs datasets state
func (ms *Machines) refresh(ctx context.Context) {
	machines := Machines{
		all:     make(map[string]*Machine),
		cmdline: ms.cmdline,
		z:       ms.z,
	}

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
	mns, boots, flattenedUserDatas, persistents, unmanagedDatasets := machines.populate(ctx, append(append(mainDatasets, cloneDatasets...), otherDatasets...), origins)

	// Get a userdata map from parent to its children
	rootUserDatasets := getRootDatasets(ctx, flattenedUserDatas)

	var rootsOnlyUserDatasets []*zfs.Dataset
	for k := range rootUserDatasets {
		rootsOnlyUserDatasets = append(rootsOnlyUserDatasets, k)
	}
	originsUserDatasets := resolveOrigin(ctx, rootsOnlyUserDatasets, "")

	unattachedSnapshotsUserDatasets, unattachedClonesUserDatasets := make(map[*zfs.Dataset][]*zfs.Dataset), make(map[*zfs.Dataset][]*zfs.Dataset) // user only snapshots or clone (not linked to a system state)
	for r, children := range rootUserDatasets {
		// Handle snapshots userdatasets
		if r.IsSnapshot {
			var associateWithAtLeastOne bool

			_, snapshot := splitSnapshotName(r.Name)
			for n, ms := range mns {
				// Snapshots are not necessarily with a dataset ID matching its parent of dataset promotions, just match
				// its name.
				if strings.HasSuffix(n, "@"+snapshot) {
					ms.machine.addUserDatasets(ctx, r, children, ms.state)
					associateWithAtLeastOne = true
					continue
				}
			}
			if associateWithAtLeastOne {
				continue
			}
			unattachedSnapshotsUserDatasets[r] = children
			continue
		}

		// Handle regular userdatasets (main or clone), associated to a system state
		if !r.IsSnapshot {
			var associateWithAtLeastOne bool
			for n, ms := range mns {
				if !nameInBootfsDatasets(n, *r) {
					continue
				}

				associateWithAtLeastOne = true
				var associatedChildren []*zfs.Dataset
				for _, d := range children {
					// We don’t break the outer loop here because we can have the case of:
					// - main userdataset is associated with 2 states
					// - one child is associated with only one of them
					if !nameInBootfsDatasets(n, *d) {
						continue
					}
					associatedChildren = append(associatedChildren, d)
				}
				ms.machine.addUserDatasets(ctx, r, associatedChildren, ms.state)
			}

			if associateWithAtLeastOne {
				continue
			}
			unattachedClonesUserDatasets[r] = children

		}
	}

	// Handle regular userdatasets (main or clone), not associated to a system state.

	// This is a userdataset "snapshot" clone dataset.
	for r, children := range unattachedClonesUserDatasets {
		// WARNING: We only consider the dataset "group" (clones and promoted) attached to main state of a given machine
		// to regroup on a known machine.
		origin := *(originsUserDatasets[r.Name])
		// This is manual promotion from the user on a user dataset without promoting the whole state:
		// ignore the dataset and issue a warning.
		// If we iterated over all the user datasets from all machines and states, we may find a match, but ignore
		// for now as this is already a manual user interaction.
		if origin == "" {
			log.Warningf(ctx, i18n.G("Couldn't find any association for user dataset %s"), r.Name)
			continue
		}

		var associateWithAtLeastOne bool
		for _, m := range machines.all {
			var associated bool
			for _, userStates := range m.Users {
				for _, userState := range userStates {
					if userState.ID == origin {
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
			log.Warningf(ctx, i18n.G("Couldn't find any association for user dataset %s"), r.Name)
		}
	}

	// This is a userdataset "snapshot" snapshot dataset.
	for r, children := range unattachedSnapshotsUserDatasets {
		base, _ := splitSnapshotName(r.Name)
		t := strings.Split(filepath.Base(base), "_")
		user := t[0]
		if len(t) > 1 {
			user = strings.Join(t[:len(t)-1], "_")
		}
		var associated bool
		for _, m := range machines.all {
			for _, userState := range m.Users[user] {
				if userState.ID == base {
					m.addUserDatasets(ctx, r, children, nil)
					associated = true
					break
				}
			}
			// we don’t break here, as a userdata only snapshot can be associated with multiple machines
		}
		if !associated {
			log.Warningf(ctx, i18n.G("Couldn't find any association for user dataset %s"), r.Name)
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
		for id := range m.SystemDatasets {
			machines.allSystemDatasets = append(machines.allSystemDatasets, m.SystemDatasets[id]...)
		}
		for _, k := range sortedStateKeys(m.History) {
			h := m.History[k]
			for id := range h.SystemDatasets {
				machines.allSystemDatasets = append(machines.allSystemDatasets, h.SystemDatasets[id]...)
			}
		}
	}

	// Append unlinked boot datasets to ensure we will switch to noauto everything
	machines.allSystemDatasets = appendIfNotPresent(machines.allSystemDatasets, boots, true)
	machines.unmanagedDatasets = unmanagedDatasets

	root, _ := bootParametersFromCmdline(machines.cmdline)
	m, _ := machines.findFromRoot(root)
	machines.current = m

	*ms = machines
	log.Debugf(ctx, i18n.G("current machines scanning layout:\n"+pp.Sprint(ms)))
}

// populate attach main system datasets to machines and returns other types of datasets for later triage/attachment, alongside
// a map to direct access to a given state and machine
func (ms *Machines) populate(ctx context.Context, allDatasets []zfs.Dataset, origins map[string]*string) (mns map[string]machineAndState, boots, userdatas, persistents, unmanagedDatasets []*zfs.Dataset) {
	mns = make(map[string]machineAndState)

	for _, d := range allDatasets {
		// we are taking the d address. Ensure we have a local variable that isn’t going to be reused
		d := d
		// Main active system dataset building up a machine
		m := newMachineFromDataset(d, origins[d.Name])
		if m != nil {
			ms.all[d.Name] = m
			mns[d.Name] = machineAndState{
				state:   &m.State,
				machine: m,
			}
			continue
		}

		// Check for children, clones and snapshots
		if ms.populateSystemAndHistory(ctx, d, origins[d.Name], &mns) {
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
			unmanagedDatasets = append(unmanagedDatasets, &d)
			continue
		}

		// should be persistent datasets
		persistents = append(persistents, &d)
	}

	return mns, boots, userdatas, persistents, unmanagedDatasets
}

// newMachineFromDataset returns a new machine if the given dataset is a main system one.
func newMachineFromDataset(d zfs.Dataset, origin *string) *Machine {
	// Register all zsys non cloned mountable / to a new machine
	if d.Mountpoint == "/" && d.CanMount != "off" && origin != nil && *origin == "" {
		m := Machine{
			State: State{
				ID:             d.Name,
				IsZsys:         d.BootFS,
				SystemDatasets: make(map[string][]*zfs.Dataset),
				UserDatasets:   make(map[string][]*zfs.Dataset),
			},
			Users:   make(map[string]map[string]userState),
			History: make(map[string]*State),
		}
		m.SystemDatasets[d.Name] = []*zfs.Dataset{&d}
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
func (ms *Machines) populateSystemAndHistory(ctx context.Context, d zfs.Dataset, origin *string, mns *map[string]machineAndState) (ok bool) {
	for _, m := range ms.all {

		// Direct main machine state children
		if ok, err := isChild(m.ID, d); err != nil {
			log.Warningf(ctx, i18n.G("ignoring %q as couldn't assert if it's a child: ")+config.ErrorFormat, d.Name, err)
		} else if ok {
			m.SystemDatasets[m.ID] = append(m.SystemDatasets[m.ID], &d)
			return true
		}

		// Clones or snapshot root dataset (origins points to origin dataset)
		if d.Mountpoint == "/" && d.CanMount != "off" && origin != nil && *origin == m.ID {
			s := &State{
				ID:             d.Name,
				IsZsys:         d.BootFS,
				SystemDatasets: make(map[string][]*zfs.Dataset),
				UserDatasets:   make(map[string][]*zfs.Dataset),
			}
			s.SystemDatasets[d.Name] = []*zfs.Dataset{&d}
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
				h.SystemDatasets[h.ID] = append(h.SystemDatasets[h.ID], &d)
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
		state.UserDatasets[r.Name] = []*zfs.Dataset{r}
		state.UserDatasets[r.Name] = append(state.UserDatasets[r.Name], children...)
	}

	// Extract user name
	base, _ := splitSnapshotName(r.Name)
	t := strings.Split(filepath.Base(base), "_")
	user := t[0]
	if len(t) > 1 {
		user = strings.Join(t[:len(t)-1], "_")
	}
	if m.Users[user] == nil {
		m.Users[user] = make(map[string]userState)
	}

	// Attach to global user map new userData
	// If the dataset has already been added  it is overwritten
	s := userState{
		ID:       r.Name,
		Datasets: append([]*zfs.Dataset{r}, children...),
	}
	// We don't want lastused to be 1970 in our golden files
	if r.LastUsed != 0 {
		lu := time.Unix(int64(r.LastUsed), 0)
		s.LastUsed = &lu
	}
	m.Users[user][r.Name] = s
}

// attachRemainingDatasets attaches to machine boot and persistent datasets if they fit current machine.
func (m *Machine) attachRemainingDatasets(ctx context.Context, boots, persistents []*zfs.Dataset) {
	// machineID is the basename of the State.
	machineID := filepath.Base(m.ID)

	// Boot datasets
	var bootDatasetsID string
	for _, d := range boots {
		d := d
		if d.IsSnapshot {
			continue
		}
		// Main boot base dataset (matching machine ID)
		if strings.HasSuffix(d.Name, "/"+machineID) {
			bootDatasetsID = d.Name
			m.SystemDatasets[bootDatasetsID] = []*zfs.Dataset{d}
		} else if bootDatasetsID != "" && strings.HasPrefix(d.Name, bootDatasetsID+"/") { // child
			m.SystemDatasets[bootDatasetsID] = append(m.SystemDatasets[bootDatasetsID], d)
		}
	}

	// Persistent datasets
	m.PersistentDatasets = persistents

	// Handle history now
	// We want reproducibility, so iterate to attach datasets in a given order.
	for _, k := range sortedStateKeys(m.History) {
		h := m.History[k]
		h.attachRemainingDatasetsForHistory(boots, persistents)
	}
}

// attachRemainingDatasetsForHistory attaches to a given history state boot and persistent datasets if they fit.
// It's similar to attachRemainingDatasets with some particular rules on snapshots.
func (h *State) attachRemainingDatasetsForHistory(boots, persistents []*zfs.Dataset) {
	// stateID is the basename of the State.
	stateID := filepath.Base(h.ID)

	var snapshot string
	if j := strings.LastIndex(stateID, "@"); j > 0 {
		snapshot = stateID[j+1:]
	}

	// Boot datasets
	var bootDatasetsID string
	for _, d := range boots {
		if snapshot != "" {
			// Snapshots are not necessarily with a dataset ID matching its parent of dataset promotions, just match
			// its name and take the first route we find.
			if bootDatasetsID == "" && strings.HasSuffix(d.Name, "@"+snapshot) {
				bootDatasetsID = d.Name
				h.SystemDatasets[bootDatasetsID] = []*zfs.Dataset{d}
				continue
			} else if bootDatasetsID != "" {
				baseBootDatasetsID, _ := splitSnapshotName(bootDatasetsID)
				if strings.HasPrefix(d.Name, baseBootDatasetsID+"/") && strings.HasSuffix(d.Name, "@"+snapshot) { // child
					h.SystemDatasets[bootDatasetsID] = append(h.SystemDatasets[bootDatasetsID], d)
				}
			}
		}
		// For clones just match the base datasetname or its children.

		// Main boot base dataset (matching machine ID)
		if strings.HasSuffix(d.Name, "/"+stateID) {
			bootDatasetsID = d.Name
			h.SystemDatasets[bootDatasetsID] = []*zfs.Dataset{d}
		} else if bootDatasetsID != "" && strings.HasPrefix(d.Name, bootDatasetsID+"/") { // child
			h.SystemDatasets[bootDatasetsID] = append(h.SystemDatasets[bootDatasetsID], d)
		}
	}

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
