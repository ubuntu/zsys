package machines

import (
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	log "github.com/sirupsen/logrus"
	"github.com/ubuntu/zsys/internal/config"
	"github.com/ubuntu/zsys/internal/zfs"
	"golang.org/x/xerrors"
)

// ZfsSetPropertyScanCreater can only Create, Scan and SetProperty on datasets
type ZfsSetPropertyScanCreater interface {
	Create(path, mountpoint, canmount string) error
	zfsScanner
	zfsPropertySetter
}

// CreateUserData creates a new dataset for homepath and attach to current system.
// It creates intermediates user datasets if needed.
func (ms *Machines) CreateUserData(user, homepath string, z ZfsSetPropertyScanCreater) error {
	if !ms.current.isZsys() {
		return errors.New("Current machine isn't Zsys, nothing to create")
	}

	log.Infof("create user dataset for %q\n", homepath)

	// Take same pool as existing userdatasets for current system
	userdatasetRoot := ""
	if len(ms.current.UserDatasets) > 0 {
		userdatasetRoot = getUserDatasetPath(ms.current.UserDatasets[0].Name)
	}
	// If there is none attached to the current system, try to take first existing userdataset detected pool
	if len(ms.allUsersDatasets) > 0 {
		userdatasetRoot = getUserDatasetPath(ms.allUsersDatasets[0].Name)
	}

	// If there is still none found, take the current system pool
	if userdatasetRoot == "" {
		p := ms.current.SystemDatasets[0].Name
		i := strings.Index(p, "/")
		if i != -1 {
			p = p[0:i]
		}
		userdatasetRoot = filepath.Join(p, "USERDATA")

		// Create parent USERDATA.
		if err := z.Create(userdatasetRoot, "/", "off"); err != nil {
			return xerrors.Errorf("couldn't create user data embedder dataset: "+config.ErrorFormat, err)
		}
	}

	userdataset := filepath.Join(userdatasetRoot, fmt.Sprintf("%s_%s", user, generateID(6)))
	if err := z.Create(userdataset, homepath, "on"); err != nil {
		return err
	}

	// Tag to associate with current system and lastUsed
	if err := z.SetProperty(zfs.BootfsDatasetsProp, ms.current.ID, userdataset, false); err != nil {
		return xerrors.Errorf("couldn't add %q to BootfsDatasets property of %q: "+config.ErrorFormat, ms.current.ID, userdataset, err)
	}

	currentTime := strconv.Itoa(int(time.Now().Unix()))
	if err := z.SetProperty(zfs.LastUsedProp, currentTime, userdataset, false); err != nil {
		return xerrors.Errorf("couldn't set last used time to %q: "+config.ErrorFormat, currentTime, err)
	}

	// Rescan datasets, with new user datasets
	ds, err := z.Scan()
	if err != nil {
		// don't fail if Scan is failing, as the dataset was created
		log.Warningf("couldn't rescan after committing boot: "+config.ErrorFormat, err)
	}
	*ms = New(ds, ms.cmdline)

	return nil
}

func getUserDatasetPath(path string) string {
	lpath := strings.ToLower(path)
	i := strings.Index(lpath, userdatasetsContainerName)
	if i == -1 {
		return ""
	}
	return path[0 : i+len(userdatasetsContainerName)]
}
