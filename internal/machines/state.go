package machines

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/ubuntu/zsys/internal/config"
	"github.com/ubuntu/zsys/internal/i18n"
	"github.com/ubuntu/zsys/internal/log"
	"github.com/ubuntu/zsys/internal/zfs"
	"github.com/ubuntu/zsys/internal/zfs/libzfs"
)

// ErrStateRemovalNeedsConfirmation is returned when a state operation cannot be performed because a state has dependencies
type ErrStateRemovalNeedsConfirmation struct {
	s string
}

func (e *ErrStateRemovalNeedsConfirmation) Error() string {
	return e.s
}

type stateWithLinkedState struct {
	*State
	linkedStateID string
}

// getDependencies returns the list of states that a given one depends on (user or systems) and the external datasets
// depending on us.
// Note that a system states will list all its user states (as when requesting to delete a system state, we will delete
// the associated system states), BUT listing a user state won’t list the associated system states.
// If we unlink an user system states from a system state (due to the system state being removed), we don’t consider we will destroy
// this user state immediately, and keep it for clones and snapshots (we don’t go over its deps).
func (s *State) getDependencies(ctx context.Context, ms *Machines) (stateDeps []stateWithLinkedState, datasetDeps []*zfs.Dataset) {
	nt := ms.z.NewNoTransaction(ctx)

	// build cache and lookup for all states
	var allStates []*State
	for _, m := range ms.all {
		allStates = append(allStates, &m.State)
		for _, h := range m.History {
			allStates = append(allStates, h)
		}
		for _, ustates := range m.AllUsersStates {
			for _, us := range ustates {
				allStates = append(allStates, us)
			}
		}
	}
	datasetToState := make(map[*zfs.Dataset]*State)
	for _, s := range allStates {
		for _, ds := range s.Datasets {
			for _, d := range ds {
				datasetToState[d] = s
			}
		}
	}

	var reason string
	// Direct call on user datasets linked to multiple states: only unlink from attached states and don’t list any dep
	// Count of unique bootfsdatasets must be > 1
	var linkedToMultipleStates bool
	if s.Users == nil {
		for _, d := range s.Datasets {
			bfsds := strings.Split(d[0].BootfsDatasets, bootfsdatasetsSeparator)
			bfsdsUnique := make(map[string]bool)
			for _, v := range bfsds {
				v = strings.TrimSpace(v)
				if v != "" {
					bfsdsUnique[v] = true
				}
			}
			if len(bfsdsUnique) > 1 {
				linkedToMultipleStates = true
			}
		}
	}
	if linkedToMultipleStates {
	findState:
		for _, m := range ms.all {
			for _, us := range m.Users {
				if us == s {
					reason = m.ID
					break findState
				}
			}
			for _, h := range m.History {
				for _, us := range h.Users {
					if us == s {
						reason = h.ID
						break findState
					}
				}
			}
		}

	}

	return s.getDependenciesWithCache(nt, ms, reason, allStates, datasetToState, make(map[stateWithLinkedState]stateToDeps))
}

type stateToDeps struct {
	stateDeps   []stateWithLinkedState
	datasetDeps []*zfs.Dataset
}

func (s *State) getDependenciesWithCache(nt *zfs.NoTransaction, ms *Machines, reason string, allStates []*State, datasetToState map[*zfs.Dataset]*State, depsResolvedCache map[stateWithLinkedState]stateToDeps) (stateDeps []stateWithLinkedState, datasetDeps []*zfs.Dataset) {
	log.Debugf(nt.Context(), "getDependenciesWithCache for state %s and reason: %s", s.ID, reason)
	// Look in cache
	if dep, ok := depsResolvedCache[stateWithLinkedState{s, reason}]; ok {
		return dep.stateDeps, dep.datasetDeps
	}

	// We will only untag the datasets and not remove them, only return ourself
	if reason != "" {
		return []stateWithLinkedState{{State: s, linkedStateID: reason}}, nil
	}

	for _, ds := range s.Datasets {
		// As we detect complete dependencies hierarchy, we only take the root dataset for each route
		d := ds[0]

		deps := nt.Dependencies(*d)

		// Look for corresponding state (user or system)
		for _, dataset := range deps {
			datasetState := datasetToState[dataset]
			if datasetState != nil {
				// We skip current state always as the last one. Discard it if brought by children datasets.
				if datasetState == s {
					continue
				}
				// If this is a system state, get related user states deps
				for _, us := range datasetState.Users {
					log.Debugf(nt.Context(), i18n.G("Getting dependencies for user state %s"), us.ID)
					uDeps, udDeps := us.getDependenciesWithCache(nt, ms, datasetState.ID, allStates, datasetToState, depsResolvedCache)
					depsResolvedCache[stateWithLinkedState{us, datasetState.ID}] = stateToDeps{uDeps, udDeps}
					stateDeps = append(stateDeps, uDeps...)
					datasetDeps = append(datasetDeps, udDeps...)
				}
				cDeps, cdDeps := datasetState.getDependenciesWithCache(nt, ms, "", allStates, datasetToState, depsResolvedCache)
				depsResolvedCache[stateWithLinkedState{datasetState, ""}] = stateToDeps{cDeps, cdDeps}
				stateDeps = append(stateDeps, cDeps...)
				datasetDeps = append(datasetDeps, cdDeps...)
			} else {
				datasetDeps = append(datasetDeps, dataset)
			}
		}
	}

	// If current state is a system one, add its user states and deps.
	// (If we added it above before if datasetState == s {continue}, those would be only added if current state had children datasets)
	for _, us := range s.Users {
		log.Debugf(nt.Context(), i18n.G("Getting dependencies for user state %s"), us.ID)
		uDeps, udDeps := us.getDependenciesWithCache(nt, ms, s.ID, allStates, datasetToState, depsResolvedCache)
		depsResolvedCache[stateWithLinkedState{us, s.ID}] = stateToDeps{uDeps, udDeps}
		stateDeps = append(stateDeps, uDeps...)
		datasetDeps = append(datasetDeps, udDeps...)
	}
	// Add current state as the last dep
	stateDeps = append(stateDeps, stateWithLinkedState{s, reason})

	// Deduplicate state dependencies, keeping first which will has its inverse states just after (as depending on getDependecies order)
	keys := make(map[stateWithLinkedState]bool)
	var uniqStateDeps []stateWithLinkedState
	for _, entry := range stateDeps {
		if _, alreadyAnalyzed := keys[entry]; alreadyAnalyzed {
			continue
		}
		keys[entry] = true

		// Keep position only for with filesystem datasets states or snapshots without parent
		uniqStateDeps = append(uniqStateDeps, entry)
	}

	// Deduplicate datasets dependencies, keeping first which will has its inverse deps just after (as depending on getDependecies order)
	keysDS := make(map[string]bool)
	var uniqDatasetDeps []*zfs.Dataset
	for _, entry := range datasetDeps {
		if _, value := keysDS[entry.Name]; !value {
			keysDS[entry.Name] = true
			uniqDatasetDeps = append(uniqDatasetDeps, entry)
		}
	}

	return uniqStateDeps, uniqDatasetDeps
}

// RemoveState removes a system or user state with name as Id of the state and an optional user.
// It will prevent removing user states linked to an viable system state.
func (ms *Machines) RemoveState(ctx context.Context, name, user string, force, dryrun bool) error {
	s, err := ms.IDToState(ctx, name, user)
	if err != nil {
		return fmt.Errorf(i18n.G("Couldn't find state: %v"), err)
	}

	if ms.current != nil && s == &ms.current.State {
		return errors.New(i18n.G("Removing current system state isn't allowed"))
	}

	states, datasets := s.getDependencies(ctx, ms)

	log.Debug(ctx, "Depending states found:")
	for _, s := range states {
		log.Debugf(ctx, "    - %s", s.ID)
	}
	log.Debug(ctx, "Depending datasets found:")
	for _, d := range datasets {
		log.Debugf(ctx, "    - %s", d.Name)
	}

	if !force {
		var errmsg string
		// Check that current state is not linked to a system state.
		// Dependencies will trigger a message and list themselves if linked or not to system state
		if user != "" {
			ps := s.parentSystemState(ms)
			if ps != nil {
				errmsg += fmt.Sprintf(i18n.G("%s will be detached from system state %s\n"), s.ID, ps.ID)
			}
		}

		// we always added us as a system state
		if len(states) > len(s.Users)+1 {
			errmsg += fmt.Sprintf(i18n.G("%s has a dependency linked to some states:\n"), s.ID)
			for i := len(states) - 2; i >= 0; i-- {
				curr := states[i]
				lu := i18n.G("No timestamp")
				if !curr.LastUsed.Equal(time.Time{}) {
					lu = curr.LastUsed.Format("2006-01-02 15:04:05")
				}
				var additionalInfo string
				if curr.linkedStateID != "" {
					additionalInfo = fmt.Sprintf(" "+i18n.G("to unlink from %s"), curr.linkedStateID)
				} else {
					bmap := make(map[string]bool)
					for _, d := range curr.Datasets {
						for _, b := range strings.Split(d[0].BootfsDatasets, bootfsdatasetsSeparator) {
							bmap[b] = true
						}
					}
					var keys []string
					for k := range bmap {
						if strings.TrimSpace(k) != "" {
							keys = append(keys, k)
						}
					}
					if len(keys) > 0 {
						additionalInfo = fmt.Sprintf(" "+i18n.G("to remove. Currently linked to %s"), strings.Join(keys, ", "))
					}
				}
				errmsg += fmt.Sprintf(i18n.G("  - %s (%s)%s\n"), curr.ID, lu, additionalInfo)
			}
		}
		if len(datasets) > 0 {
			errmsg += fmt.Sprintf(i18n.G("%s has a dependency on some datasets:\n"), s.ID)
			for i := len(datasets) - 1; i >= 0; i-- {
				errmsg += fmt.Sprintf(i18n.G("  - %s\n"), datasets[i].Name)
			}
		}
		if errmsg != "" {
			return &ErrStateRemovalNeedsConfirmation{s: errmsg}
		}
	}

	// Remove datasets
	nt := ms.z.NewNoTransaction(ctx)
	for _, d := range datasets {
		if dryrun {
			log.RemotePrintf(ctx, i18n.G("Deleting dataset %s\n"), d.Name)
			continue
		}
		if err := nt.Destroy(d.Name); err != nil {
			return fmt.Errorf(i18n.G("Couldn't remove dataset %s: %v"), d.Name, err)
		}
	}

	// Remove only listed states in dependencies.
	for _, state := range states {
		if dryrun {
			log.RemotePrintf(ctx, i18n.G("Deleting state %s\n"), state.ID)
			continue
		}
		if err := state.remove(ctx, ms, state.linkedStateID); err != nil {
			return fmt.Errorf(i18n.G("Couldn't remove state %s: %v"), state.ID, err)
		}
	}

	ms.refresh(ctx)
	return nil
}

// Remove removes a given state by deleting all of its system datasets and unlink user states
// If called on system states: always try to destroy this state. all user states will be unlinked.
// If called on user states:
// - with empty linkedStateID -> this is a direct call on this state, always try to destroy.
// - with a non empty linkedStateID -> indirect call, only unlink to this system state. This is a no-op on
// snapshots.
// If the user state has some snapshots as children: this will error out.
func (s *State) remove(ctx context.Context, ms *Machines, linkedStateID string) error {
	nt := ms.z.NewNoTransaction(ctx)

	log.Debugf(ctx, i18n.G("Removing state %s. linkedStateID: %s\n"), s.ID, linkedStateID)

	// Note: if we remove a user States which is a file system dataset, all snapshots (user snapshots) will be removed as well.
	// This is OK for now as:
	// - we already asked for direct user request removal on snapshots before (as a dependency of this user state)
	// - the gc rules are aligned between system and users (and so, if we decide to remove a clone,
	//   it means that we already have enough states)

	// Untag all datasets associated with this state for non snapshots
	if !s.isSnapshot() && linkedStateID != "" {
		log.Debug(ctx, i18n.G("Untagging all datasets\n"))
		t, cancel := ms.z.NewTransaction(ctx)
		defer t.Done()
		for _, d := range s.getDatasets() {
			var newTags []string
			for _, n := range strings.Split(d.BootfsDatasets, bootfsdatasetsSeparator) {
				if n != linkedStateID {
					newTags = append(newTags, n)
					break
				}
			}

			newTag := strings.Join(newTags, bootfsdatasetsSeparator)

			if newTag == d.BootfsDatasets {
				continue
			}

			log.Debugf(ctx, i18n.G("Setting new bootfs tag %s on %s\n"), newTag, d.Name)

			if err := t.SetProperty(libzfs.BootfsDatasetsProp, newTag, d.Name, false); err != nil {
				cancel()
				return fmt.Errorf(i18n.G("couldn't remove %q to BootfsDatasets property of %q: ")+config.ErrorFormat, linkedStateID, d.Name, err)
			}
		}
	}

	// Unlink from parent
	if ps := s.parentSystemState(ms); ps != nil {
		for user, us := range ps.Users {
			if us == s {
				delete(ps.Users, user)
				break
			}
		}
	}

	// If we have a system state, request user cleaning (untag and maybe deletion)
	for _, us := range s.Users {
		if err := us.remove(ctx, ms, s.ID); err != nil {
			return err
		}
	}

	// Only destroy if called directly
	if linkedStateID != "" {
		return nil
	}

	// Remove the datasets
	for route := range s.Datasets {
		log.Debugf(ctx, "Destroying %s\n", route)
		if err := nt.Destroy(route); err != nil {
			return fmt.Errorf(i18n.G("Couldn't destroy %s: %v"), route, err)
		}
	}

	return nil
}

// getDatasets returns all Datasets from this given state.
func (s State) getDatasets() []*zfs.Dataset {
	var r []*zfs.Dataset
	for _, ds := range s.Datasets {
		r = append(r, ds...)
	}
	return r
}

// getUsersDatasets returns all user datasets attached to this particular state.
func (s State) getUsersDatasets() []*zfs.Dataset {
	var r []*zfs.Dataset
	for _, cs := range s.Users {
		r = append(r, cs.getDatasets()...)
	}
	return r
}

// isSnapshot returns if this state is a snapshot.
func (s State) isSnapshot() bool {
	return strings.Contains(s.ID, "@")
}

// prependDataset prepends d to ds.
func prependDataset(ds []*zfs.Dataset, d *zfs.Dataset) []*zfs.Dataset {
	ds = append(ds, nil)
	copy(ds[1:], ds)
	ds[0] = d
	return ds
}

// parentSystemState returns the parent state if exists
func (s *State) parentSystemState(ms *Machines) *State {
	// We gave a system state: no parent
	if len(s.Users) != 0 {
		return nil
	}

	for _, m := range ms.all {
		if m.State.Users != nil {
			for _, us := range m.State.Users {
				if s == us {
					return &m.State
				}
			}
		}

		for _, h := range m.History {
			if h.Users != nil {
				for _, us := range h.Users {
					if s == us {
						return h
					}
				}
			}
		}
	}
	return nil
}

// IDToState returns a state object from an Id and an error if there are many
// name can be:
// - the full path of a state
// - the suffix of the state (ubuntu_xxxx)
// - the snapshot name of the state (xxxx -> @xxxx)
// - the suffix after _ of the state (xxxx)
// user limits the research on the given user state, otherwise we limit the search on system states.
func (ms *Machines) IDToState(ctx context.Context, name, user string) (*State, error) {
	log.Debugf(ctx, "finding a matching state for id %s and user %s", name, user)
	if name == "" {
		return nil, errors.New(i18n.G("state id is mandatory"))
	}
	var matchingStates []*State
	for _, m := range ms.all {
		if user != "" {
			for id, us := range m.AllUsersStates[user] {
				if idMatches(id, name) {
					matchingStates = append(matchingStates, us)
				}
			}
			continue
		}

		// Active for machine
		if idMatches(m.ID, name) {
			matchingStates = append(matchingStates, &m.State)
		}

		// History
		for _, h := range m.History {
			if idMatches(h.ID, name) {
				matchingStates = append(matchingStates, h)
			}
		}
	}

	if len(matchingStates) == 0 {
		return nil, fmt.Errorf(i18n.G("no matching state for %s"), name)
	}
	if len(matchingStates) > 1 {
		var errmsg string
		for _, match := range matchingStates {
			errmsg += fmt.Sprintf(i18n.G("  - %s (%s)\n"), match.ID, match.LastUsed.Format("2006-01-02 15:04:05"))
		}
		return nil, fmt.Errorf(i18n.G("multiple states are matching %s:\n%sPlease use full state path."), name, errmsg)
	}

	return matchingStates[0], nil
}

// idMatches returns true if the candidate matches the conditions for a given name.
// - the full path of a state
// - the suffix of the state (ubuntu_xxxx)
// - the snapshot name of the state (xxxx -> @xxxx)
// - the suffix after _ of the state (xxxx)
func idMatches(candidate, name string) bool {
	if candidate == name || filepath.Base(candidate) == name || strings.HasSuffix(candidate, "@"+name) || strings.HasSuffix(candidate, "_"+name) {
		return true
	}
	return false
}
