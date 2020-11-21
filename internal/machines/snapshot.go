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
	if err := validateStateName(name); err != nil {
		return "", err
	}

	t, cancel := ms.z.NewTransaction(ctx)
	defer t.Done()

	var toSnapshot []*zfs.Dataset
	if onlyUser != "" {
		userState, ok := m.State.Users[onlyUser]
		if !ok {
			return "", fmt.Errorf(i18n.G("user %q doesn't exist"), onlyUser)
		}
		// check if a system history entry matches the desired snapshot name.
		for n := range m.History {
			if strings.HasSuffix(n, "@"+name) {
				return "", fmt.Errorf(i18n.G("A snapshot %q already exists on system and can create an incoherent state"), name)
			}
		}
		toSnapshot = userState.getDatasets()
	} else {
		toSnapshot = append(m.State.getDatasets(), m.State.getUsersDatasets()...)
	}

	// check pool capacity before saving state
	pools := make(map[string]bool)
	for _, d := range toSnapshot {
		pools[strings.Split(d.Name, "/")[0]] = true
	}

	for p := range pools {
		free, err := ms.z.GetPoolFreeSpace(p)
		if err != nil {
			return "", err
		}

		if free <= ms.conf.General.MinFreePoolSpace {
			return "", fmt.Errorf(i18n.G(`Minimum free space to take a snapshot and preserve ZFS performance is %d%%.
Free space on pool %q is %d%%.
Please remove some states manually to free up space.`), ms.conf.General.MinFreePoolSpace, p, free)
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

func validateStateName(stateName string) error {
	if strings.HasPrefix(stateName, "-") {
		return errors.New(i18n.G("state name cannot start with '-'"))
	}

	// List of valid characters from zcommon->zfs_namecheck->valid_char()
	// Space is also valid but not supported by grub and init so booting from a snapshot with a space in the name fails
	var invalidChars []string
	for _, c := range stateName {
		if !((c >= 'a' && c <= 'z') ||
			(c >= 'A' && c <= 'Z') ||
			(c >= '0' && c <= '9') ||
			c == '-' || c == '_' || c == '.' || c == ':') {
			invalidChars = append(invalidChars, string(c))
		}
	}
	if invalidChars != nil {
		// Deduplicate list of invalid chars so they appear only once in error message
		keys := make(map[string]bool)
		uniqueChars := []string{}

		for _, c := range invalidChars {
			if _, v := keys[c]; !v {
				keys[c] = true
				uniqueChars = append(uniqueChars, c)
			}
		}
		return fmt.Errorf(i18n.G("the following characters are not supported in state name: '%s'"), strings.Join(uniqueChars, "','"))
	}

	return nil
}
