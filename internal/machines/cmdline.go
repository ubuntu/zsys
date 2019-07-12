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
