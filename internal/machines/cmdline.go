package machines

import (
	"path/filepath"
	"strings"
)

const (
	kernelPrefix         = "BOOT_IMAGE="
	zfsRootPrefix        = "root=ZFS="
	zfsRevertUserDataTag = "zsys-revert=userdata"
)

// bootParametersFromCmdline returns the rootDataset name and revertuserData state if we are in a revert case
func bootParametersFromCmdline(cmdline string) (rootDataset string, revertuserData bool) {
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

// kernelFromCmdline returns the used kernel name in cmdline
func kernelFromCmdline(cmdline string) (kernel string) {
	for _, entry := range strings.Fields(cmdline) {
		e := strings.TrimPrefix(entry, kernelPrefix)
		if e == entry {
			continue
		}
		return filepath.Base(e)
	}

	return ""
}

// findFromRoot returns the active machine and state if any.
// If rootName is a snapshot, it fallbacks to current mounted root dataset. If no root dataset is mounted, s can be nil
func (machines *Machines) findFromRoot(rootName string) (*Machine, *State) {
	// Not a zfs system
	if rootName == "" {
		return nil, nil
	}

	// Fast path: if rootName is already a main dataset state
	if m, exists := machines.all[rootName]; exists {
		return m, &m.State
	}

	var fromSnapshot bool
	if strings.Contains(rootName, "@") {
		fromSnapshot = true
	}

	// We know that our desired target is a history one
	for _, m := range machines.all {
		// Only match on names as we booted an existing clone directly.
		if !fromSnapshot {
			s, ok := m.History[rootName]
			if !ok {
				continue
			}
			return m, s
		}

		// We have a snapshot, we need to find the corresponding mounted main dataset on /.
		// Look first on current machine
		if m.Datasets[m.ID][0].Mounted && m.Datasets[m.ID][0].Mountpoint == "/" {
			return m, &m.State
		}
		// Look now on History
		for _, h := range m.History {
			if h.Datasets[h.ID][0].Mounted && h.Datasets[h.ID][0].Mountpoint == "/" {
				return m, h
			}
		}
	}

	return nil, nil
}

func hasBootedOnSnapshot(cmdline string) bool {
	root, _ := bootParametersFromCmdline(cmdline)
	return strings.Contains(root, "@")
}
