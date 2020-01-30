package machines

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/ubuntu/zsys/internal/i18n"
)

// GetStateAndDependencies fetches a given state and all its deps
// s can be:
//   * dataset path (fully determinated)
//   * dataset ID (can match basename, multiple results)
//   * snapshot name (can match snapshot, multiple results)
func (ms Machines) GetStateAndDependencies(s string) ([]State, error) {
	// Match(es) for s
	var matches []State

	var deps []State
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
		return nil, fmt.Errorf(i18n.G("multiple states are matching %s:\n%sPlease use full state path."), s, errmsg)
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

func (s State) isSnapshot() bool {
	return strings.Contains(s.ID, "@")
}
