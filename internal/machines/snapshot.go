package machines

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/ubuntu/zsys/internal/i18n"
	"github.com/ubuntu/zsys/internal/log"
	"github.com/ubuntu/zsys/internal/zfs"
)

// CreateSnapshot creates a snapshot of a system and all users datasets.
// If name is not empty, it is used as the id of the snapshot otherwise an id
// is generated with a random string.
// If onlyUser is empty a snapshot of all the system datasets is taken,
// otherwise only a snapshot of the given username is done
func (ms *Machines) CreateSnapshot(ctx context.Context, name string, onlyUser string) error {
	m := ms.current
	if !m.isZsys() {
		return errors.New(i18n.G("Current machine isn't Zsys, nothing to create"))
	}

	if name == "" {
		name = "autozsys_" + ms.z.GenerateID(6)
	}

	t, cancel := ms.z.NewTransaction(ctx)
	defer t.Done()

	var toSnapshot []*zfs.Dataset
	if onlyUser != "" {
		log.Infof(ctx, i18n.G("Requesting snapshot %q for user %q"), name, onlyUser)
		userStates, ok := m.Users[onlyUser]
		if !ok {
			return fmt.Errorf(i18n.G("user %q doesn't exist"), onlyUser)
		}
		// Only filter datasets attached to current state, as some subdataset could be linked to another
		// system state but not that particular one.
		for _, userState := range userStates {
			for _, d := range userState.Datasets {
				for _, bootfsDataset := range strings.Split(d.BootfsDatasets, ":") {
					if bootfsDataset == m.ID || strings.HasPrefix(d.BootfsDatasets, m.ID+"/") {
						toSnapshot = append(toSnapshot, d)
						break // go on on next dataset
					}
				}
			}
		}
	} else {
		log.Infof(ctx, i18n.G("Requesting current system snapshot %q"), name)
		toSnapshot = append(toSnapshot, m.SystemDatasets...)
		toSnapshot = append(toSnapshot, m.UserDatasets...)
	}
	toSnapshot = append(toSnapshot, m.UserDatasets...)
	for _, d := range toSnapshot {
		if err := t.Snapshot(name, d.Name, false); err != nil {
			cancel()
			return err
		}
	}

	ms.refresh(ctx)
	// TODO: if system snapshot: caller to call update-grub?
	return nil
}
