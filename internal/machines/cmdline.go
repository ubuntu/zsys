package machines

import "strings"

const (
	zfsRootPrefix        = "root=ZFS="
	zfsRevertUserDataTag = "zsys-revert=userdata"
)

// parseCmdLine returns the rootDataset name and revertuserData state if we are in a revert case
func parseCmdLine(cmdline string) (rootDataset string, revertuserData bool) {
	for _, entry := range strings.Fields(cmdline) {
		e := strings.TrimPrefix(entry, zfsRootPrefix)
		if entry != e {
			rootDataset = e
			continue
		}
		if entry == zfsRevertUserDataTag {
			revertuserData = true
		}
	}

	return rootDataset, revertuserData
}

// ensureCurrentState resets the current machine state which is incorrect in case of snapshot
// before we promote (via m.Commit()) it.
func (machines *Machines) ensureCurrentState(cmdline string) {
	if !hasBootedOnSnapshot(cmdline) {
		return
	}

	root, _ := parseCmdLine(cmdline)
	m, bootedState := machines.findFromRoot(root)
	if m.ID == bootedState.ID {
		return
	}

	// switch between history state and main machine state dataset
	m.History[m.ID] = &m.State
	m.State = *bootedState
	delete(m.History, bootedState.ID)
	// switch now main index object, as main machine state dataset has changed
	machines.all[bootedState.ID] = m
	delete(machines.all, m.ID)
}

// hasBootedOnSnapshot returns if we booted on a snapshot
func hasBootedOnSnapshot(cmdline string) bool {
	root, _ := parseCmdLine(cmdline)
	return strings.Contains(root, "@")
}
