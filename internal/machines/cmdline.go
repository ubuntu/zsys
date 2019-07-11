package machines

import "strings"

const (
	zfsRootPrefix        = "root=ZFS="
	zfsRevertUserDataTag = "zsys-revert=userdata"
)

// findStateFromCmdline parses cmdline and find any active State.
// Returns the corresponding machine and its selected state if any.
func (machines Machines) findStateFromCmdline(cmdline string) (*Machine, *State) {
	root, _ := parseCmdLine(cmdline)

	// Not a zfs system
	if root == "" {
		return nil, nil
	}

	// Fast path: if root is already a main dataset state
	if m, exists := machines.all[root]; exists {
		return m, &m.State
	}

	var fromSnapshot bool
	if strings.Contains(root, "@") {
		fromSnapshot = true
	}

	// We know that our desired target is a history one
	for _, m := range machines.all {
		// Only match on names as we booted an existing clone directly.
		if !fromSnapshot {
			if h, ok := m.History[root]; ok {
				return m, h
			}
			continue
		}

		// We have a snapshot, we need to find the corresponding mounted main dataset on /.
		for _, h := range m.History {
			if h.PersistentDatasets[0].Mounted && h.PersistentDatasets[0].Mountpoint == "/" {
				return m, h
			}
		}
	}

	return nil, nil
}

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
