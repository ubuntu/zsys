package machines

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/ubuntu/zsys/internal/i18n"
	"github.com/ubuntu/zsys/internal/zfs"
)

const automatedSnapshotPrefix = "autozsys_"

// CreateSystemSnapshot creates a snapshot of a system and all users datasets.
// If snapshotname is not empty, it is used as the id of the snapshot otherwise an id
// is generated with a random string.
func (ms *Machines) CreateSystemSnapshot(ctx context.Context, snapshotname string) (string, error) {
	return ms.createSnapshot(ctx, snapshotname, "")
}

// CreateUserSnapshot creates a snapshot for the provided user.
// If snapshotName is not empty, it is used as the id of the snapshot otherwise an id
// is generated with a random string.
// userName is the name of the user to snapshot the datasets from.
func (ms *Machines) CreateUserSnapshot(ctx context.Context, userName, snapshotName string) (string, error) {
	if userName == "" {
		return "", errors.New(i18n.G("Needs a valid user name, got nothing"))
	}
	return ms.createSnapshot(ctx, snapshotName, userName)
}

// createSnapshot creates a snapshot of a system and all users datasets.
// If name is not empty, it is used as the id of the snapshot otherwise an id
// is generated with a random string.
// If onlyUser is empty a snapshot of all the system datasets is taken,
// otherwise only a snapshot of the given username is done
func (ms *Machines) createSnapshot(ctx context.Context, name string, onlyUser string) (string, error) {
	m := ms.current
	if !m.isZsys() {
		return "", errors.New(i18n.G("Current machine isn't Zsys, nothing to create"))
	}

	if name == "" {
		name = automatedSnapshotPrefix + ms.z.GenerateID(6)
	}

	t, cancel := ms.z.NewTransaction(ctx)
	defer t.Done()

	var toSnapshot []*zfs.Dataset
	if onlyUser != "" {
		userStates, ok := m.Users[onlyUser]
		if !ok {
			return "", fmt.Errorf(i18n.G("user %q doesn't exist"), onlyUser)
		}
		// check if a system history entry matches the desired snapshot name.
		for n := range m.History {
			if strings.HasSuffix(n, "@"+name) {
				return "", fmt.Errorf(i18n.G("A snapshot %q already exists on system and can create an incoherent state"), name)
			}
		}

		// Only filter datasets attached to current state, as some subdataset could be linked to another
		// system state but not that particular one.
		for _, userState := range userStates {
			// Don't take snapshots of snapshots
			if userState.isSnapshot() {
				continue
			}
			for _, d := range userState.Datasets {
				if nameInBootfsDatasets(m.ID, *d) {
					toSnapshot = append(toSnapshot, d)
				}
			}
		}
	} else {
		for _, ds := range m.SystemDatasets {
			toSnapshot = append(toSnapshot, ds...)
		}
		for _, ds := range m.UserDatasets {
			toSnapshot = append(toSnapshot, ds...)
		}
	}
	for _, d := range toSnapshot {
		if err := t.Snapshot(name, d.Name, false); err != nil {
			cancel()
			return "", err
		}
	}

	ms.refresh(ctx)
	return name, nil
}
