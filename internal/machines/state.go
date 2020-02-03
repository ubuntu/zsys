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
func (ms Machines) GetStateAndDependencies(s string) ([]State, error) {
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
		return nil, fmt.Errorf(i18n.G("no matching state for %s"), s)
	} else if len(matches) > 1 {
		var errmsg string
		for _, match := range matches {
			errmsg += fmt.Sprintf(i18n.G("  - %s (%s)\n"), match.ID, match.LastUsed.Format("2006-01-02 15:04:05"))
		}
		return nil, fmt.Errorf(i18n.G("multiple states are matching %s:\n%s\nPlease use full state path."), s, errmsg)
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
		return nil, fmt.Errorf(i18n.G("one or multiple manually cloned datasets should be removed first.\n%s\nPlease use \"zfs destroy\" to remove them manually."), errmsg)
	}

	return matches, nil
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

	var notSnapshotID []string
	for _, s := range states {
		if s.ID == currentID {
			return fmt.Errorf(i18n.G("cannot remove current state: %s"), currentID)
		}
		if !s.isSnapshot() {
			notSnapshotID = append(notSnapshotID, s.ID)
		}

	}

nextState:
	for _, s := range states {

		// Removing a main dataset will remove all its snapshots, we can skip those states
		for _, n := range notSnapshotID {
			if strings.HasPrefix(s.ID, n+"@") {
				continue nextState
			}
		}

		for route := range s.SystemDatasets {
			if err := nt.Destroy(route); err != nil {
				return fmt.Errorf(i18n.G("Couldn't destroy %s: %v"), route, err)
			}
		}

		for route, ds := range s.UserDatasets {
			r := s.UserDatasets[route][0]
			var newTags []string
			for _, n := range strings.Split(r.BootfsDatasets, bootfsdatasetsSeparator) {
				if n != s.ID {
					newTags = append(newTags, n)
					break
				}
			}
			newTag := strings.Join(newTags, bootfsdatasetsSeparator)

			if newTag != "" {
				// Associated with more than one: untag this one and all children
				t, _ := ms.z.NewTransaction(ctx)
				defer t.Done()
				for _, d := range ds {
					if err := t.SetProperty(zfs.BootfsDatasetsProp, newTag, d.Name, false); err != nil {
						// The error is only an untag issue, and so, the dataset will now be linked via bootfsdataset to an unexisting one.
						// This will be fixed by next GC run.
						log.Warningf(ctx, i18n.G("couldn't remove %q to BootfsDatasets property of %q. Ignoring: ")+config.ErrorFormat, route, d.Name, err)
					}
				}
			} else {
				// Associated with only this one: destroy (recursively)
				if err := nt.Destroy(route); err != nil {
					return fmt.Errorf(i18n.G("Couldn't destroy %s: %v"), route, err)
				}
			}
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
