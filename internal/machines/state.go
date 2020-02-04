package machines

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/ubuntu/zsys/internal/config"
	"github.com/ubuntu/zsys/internal/i18n"
	"github.com/ubuntu/zsys/internal/log"
	"github.com/ubuntu/zsys/internal/zfs"
)

// GetStateAndDependencies fetches a given state and all its deps
// s can be:
//   * dataset path (fully determinated)
//   * dataset ID (can match basename, multiple results)
//   * snapshot name (can match snapshot, multiple results)
func (ms Machines) GetStateAndDependencies(s string) ([]State, []UserState, error) {
	var matches, deps []State
	for _, m := range ms.all {
		if s == m.ID || s == filepath.Base(m.ID) {
			matches = append(matches, m.State)
			deps = m.getStateDependencies(m.State)
		}

		for _, state := range m.History {
			if s == state.ID || s == filepath.Base(state.ID) || strings.HasSuffix(state.ID, "@"+s) {
				matches = append(matches, *state)
				deps = m.getStateDependencies(*state)
			}
		}
	}

	if len(matches) == 0 {
		return nil, nil, fmt.Errorf(i18n.G("no matching state for %s"), s)
	} else if len(matches) > 1 {
		var errmsg string
		for _, match := range matches {
			errmsg += fmt.Sprintf(i18n.G("  - %s (%s)\n"), match.ID, match.LastUsed.Format("2006-01-02 15:04:05"))
		}
		return nil, nil, fmt.Errorf(i18n.G("multiple states are matching %s:\n%s\nPlease use full state path."), s, errmsg)
	}

	matches = append(matches, deps...)

	originsToDatasets := make(map[string][]string)
	for _, d := range append(ms.allPersistentDatasets, ms.unmanagedDatasets...) {
		if d.Origin == "" {
			continue
		}
		originsToDatasets[d.Origin] = append(originsToDatasets[d.Origin], d.Name)
	}

	var errmsg string
	// Look for manually cloned datasets in persistent OR remaining datasets outside of zsys machines
	for _, state := range matches {
		// Only snapshots can have clone dependencies outside of their system path
		if !state.isSnapshot() {
			continue
		}

		var dNames []string
		for _, ds := range state.SystemDatasets {
			for _, d := range ds {
				dNames = append(dNames, d.Name)
			}
		}
		for _, ds := range state.UserDatasets {
			for _, d := range ds {
				dNames = append(dNames, d.Name)
			}
		}
		for _, n := range dNames {
			if names, ok := originsToDatasets[n]; ok {
				for _, m := range names {
					errmsg += fmt.Sprintf(i18n.G("  - %s is a clone of %s\n"), m, n)
				}
			}
		}
	}
	if errmsg != "" {
		return nil, nil, fmt.Errorf(i18n.G("one or multiple manually cloned datasets should be removed first.\n%s\nPlease use \"zfs destroy\" to remove them manually."), errmsg)
	}

	// Get clones and snapshots for our userdatasets state save which aren’t linked to a system state
	var matchesOtherUsers []UserState
	errmsg = ""
	for dName := range matches[0].UserDatasets {
		user := userFromDatasetName(dName)
		match, err := ms.GetUserStateAndDependencies(user, dName, true)
		if err != nil {
			errmsg += fmt.Sprintf(i18n.G("one or multiple manually cloned datasets on user %q: %v\n"), user, err)
		} else {
			matchesOtherUsers = append(matchesOtherUsers, match...)
		}
	}
	if errmsg != "" {
		return nil, nil, errors.New(errmsg)
	}

	return matches, matchesOtherUsers, nil
}

// GetUserStateAndDependencies fetches a given state and all its deps
// s can be:
//   * dataset path (fully determinated)
//   * dataset ID (can match basename, multiple results)
//   * snapshot name (can match snapshot, multiple results)
// onlyUserStateSave will only list "pure" user state (not linked to any system state) and won't error out
// if it finds any.
func (ms Machines) GetUserStateAndDependencies(user, s string, onlyUserStateSave bool) ([]UserState, error) {
	if user == "" {
		return nil, errors.New(i18n.G("user is mandatory"))
	}
	if s == "" {
		return nil, errors.New(i18n.G("state id is mandatory"))
	}

	var matches, candidates, deps []UserState
	for _, m := range ms.all {
		for id, state := range m.Users[user] {
			if s == id || s == filepath.Base(id) || fmt.Sprintf("%s_%s", user, s) == filepath.Base(id) || strings.HasSuffix(id, "@"+s) {
				candidates = append(candidates, state)
				deps = m.getUserStateDependencies(user, state)
			}
		}
	}

	if len(candidates) == 0 {
		return nil, fmt.Errorf(i18n.G("no matching user state for %s"), s)
	} else if len(candidates) > 1 {
		var errmsg string
		for _, match := range candidates {
			errmsg += fmt.Sprintf(i18n.G("  - %s (%s)\n"), match.ID, match.LastUsed.Format("2006-01-02 15:04:05"))
		}
		return nil, fmt.Errorf(i18n.G("multiple user states are matching %s:\n%sPlease use full user state path."), s, errmsg)
	}

	candidates = append(candidates, deps...)

	// Check that no system states or in the dep list from this user state
	matchingSystemStates := make(map[string][]string)
nextUserState:
	for _, userState := range candidates {
		userStateID := userState.ID
		for _, m := range ms.all {
			for _, ds := range m.UserDatasets {
				for _, d := range ds {
					if d.Name == userStateID {
						if onlyUserStateSave {
							continue nextUserState
						}
						matchingSystemStates[userStateID] = append(matchingSystemStates[userStateID], m.ID)
					}
				}
			}

			for _, state := range m.History {
				for _, ds := range state.UserDatasets {
					for _, d := range ds {
						if d.Name == userStateID {
							if onlyUserStateSave {
								continue nextUserState
							}
							matchingSystemStates[userStateID] = append(matchingSystemStates[userStateID], m.ID)
						}
					}
				}
			}
		}
		matches = append(matches, userState)
	}

	if len(matchingSystemStates) > 0 {
		var errmsg string

		for k, states := range matchingSystemStates {
			errmsg += fmt.Sprintf(i18n.G("%s has a dependency linked to several system states: %v"), k, states)
		}

		if errmsg != "" {
			return nil, errors.New(errmsg)
		}
	}

	originsToDatasets := make(map[string][]string)
	for _, d := range append(ms.allPersistentDatasets, ms.unmanagedDatasets...) {
		if d.Origin == "" {
			continue
		}
		originsToDatasets[d.Origin] = append(originsToDatasets[d.Origin], d.Name)
	}

	var errmsg string
	// Look for manually cloned datasets in persistent OR remaining datasets outside of zsys machines
	for _, state := range matches {
		// Only snapshots can have clone dependencies outside of their system path
		if !state.isSnapshot() {
			continue
		}

		for _, d := range state.Datasets {
			if names, ok := originsToDatasets[d.Name]; ok {
				for _, m := range names {
					errmsg += fmt.Sprintf(i18n.G("  - %s is a clone of %s\n"), m, d.Name)
				}
			}
		}
	}
	if errmsg != "" {
		return nil, fmt.Errorf(i18n.G("one or multiple manually cloned datasets should be removed first.\n%s\nPlease use \"zfs destroy\" to remove them manually."), errmsg)
	}

	return matches, nil
}

func (m Machine) getStateDependencies(s State) (deps []State) {
	for k := range m.History {
		if (s.isSnapshot() && m.History[k].SystemDatasets[m.History[k].ID][0].Origin != s.ID) || // clones pointing to this snapshot
			(!s.isSnapshot() && !strings.HasPrefix(k, s.ID+"@")) { // k is a snapshot of this clone
			continue
		}
		deps = append(deps, *m.History[k])
		deps = append(deps, m.getStateDependencies(*m.History[k])...)
	}

	return deps
}

func (m Machine) getUserStateDependencies(user string, s UserState) (deps []UserState) {
	for k := range m.Users[user] {
		if (s.isSnapshot() && m.Users[user][k].Datasets[0].Origin != s.ID) || // clones pointing to this snapshot
			(!s.isSnapshot() && !strings.HasPrefix(k, s.ID+"@")) { // k is a snapshot of this clone
			continue
		}
		deps = append(deps, m.Users[user][k])
		deps = append(deps, m.getUserStateDependencies(user, m.Users[user][k])...)
	}

	return deps
}

// RemoveSystemStates remove this and all depending states from entry. It starts the removal in the slice order.
func (ms *Machines) RemoveSystemStates(ctx context.Context, states []State) error {
	nt := ms.z.NewNoTransaction(ctx)

	var currentID string
	if ms.current != nil {
		currentID = ms.current.ID
	}

	var fsDatasetsID []string
	for _, s := range states {
		if s.ID == currentID {
			return fmt.Errorf(i18n.G("cannot remove current state: %s"), currentID)
		}
		if !s.isSnapshot() {
			fsDatasetsID = append(fsDatasetsID, s.ID)
		}

	}

nextState:
	for _, s := range states {
		if s.isSnapshot() {
			// If there is a matching fsDatasetsID for a snapshot, don’t remove it: destroy will take care of it (recursively)
			for _, n := range fsDatasetsID {
				if strings.HasPrefix(s.ID, n+"@") {
					continue nextState
				}
			}
		}

		for route, ds := range s.UserDatasets {
			user := userFromDatasetName(route)
			us, err := ms.GetUserStateAndDependencies(user, route, true)
			if err != nil {
				log.Warningf(ctx, i18n.G("Cannot get list of dependencies for user %s and state %s: %v"), user, route, err)
				continue
			}
			userStatesToRemove := []UserState{UserState{ID: route, Datasets: ds}}

			for i := len(us) - 1; i >= 0; i-- {
				userStatesToRemove = append(userStatesToRemove, us[i])
			}

			if err := ms.RemoveUserStates(ctx, userStatesToRemove, s.ID); err != nil {
				log.Warningf(ctx, i18n.G("Can't untag or destroy user dataset for %s: %v"), s.ID, err)
			}
		}

		for route := range s.SystemDatasets {
			if err := nt.Destroy(route); err != nil {
				return fmt.Errorf(i18n.G("Couldn't destroy %s: %v"), route, err)
			}
		}
	}

	ms.refresh(ctx)
	return nil
}

// RemoveUserStates remove this or untag and all depending states from entry. It starts the removal in the slice reverse order.
// If systemStateID is provided, it will try to untag the association to this system before considering it for removal
// or not.
// If systemStateID is empty, all UserStates will be removed without considering their bootfsdataset tags.
func (ms *Machines) RemoveUserStates(ctx context.Context, states []UserState, systemStateID string) error {
	nt := ms.z.NewNoTransaction(ctx)

	var candidates []UserState
	// If we have a snapshot and a filesystem userstate, only keep the filesystem userstate
	// which will destroy the snapshot.
	// Snapshots don’t have bootfsdatasets tags, so we need this logic
nextState:
	for _, s := range states {
		if !s.isSnapshot() {
			candidates = append(candidates, s)
		}
		base, _ := splitSnapshotName(s.ID)
		// check for parents
		for _, parent := range states {
			if parent.ID == base {
				continue nextState
			}
		}
		candidates = append(candidates, s)
	}

	var datasetsToDelete []*zfs.Dataset
	for route, s := range candidates {
		for _, d := range s.Datasets {
			var newTags []string
			if systemStateID != "" {
				for _, n := range strings.Split(d.BootfsDatasets, bootfsdatasetsSeparator) {
					if n != systemStateID {
						newTags = append(newTags, n)
						break
					}
				}
			}

			newTag := strings.Join(newTags, bootfsdatasetsSeparator)

			if newTag != "" {
				// Associated with more than one: untag this one and all children
				t, cancel := ms.z.NewTransaction(ctx)
				defer t.Done()
				if err := t.SetProperty(zfs.BootfsDatasetsProp, newTag, d.Name, false); err != nil {
					cancel()
					return fmt.Errorf(i18n.G("couldn't remove %q to BootfsDatasets property of %q: ")+config.ErrorFormat, route, d.Name, err)
				}
			} else {
				// Associated with only this one: destroy (in reverse order)
				datasetsToDelete = prependDataset(datasetsToDelete, d)
			}
		}
	}

	// Remove all datasets (and its children if any not destroyed yet). The predicate is that base datasets
	// should have more or the same bootfs datasets association than its children to be valid.
	for _, d := range datasetsToDelete {
		if err := nt.Destroy(d.Name); err != nil {
			return fmt.Errorf(i18n.G("Couldn't destroy %s: %v"), d.Name, err)
		}
	}

	ms.refresh(ctx)
	return nil
}

func (s State) isSnapshot() bool {
	return strings.Contains(s.ID, "@")
}

func (s UserState) isSnapshot() bool {
	return strings.Contains(s.ID, "@")
}

func prependDataset(ds []*zfs.Dataset, d *zfs.Dataset) []*zfs.Dataset {
	ds = append(ds, nil)
	copy(ds[1:], ds)
	ds[0] = d
	return ds
}
