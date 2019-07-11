package machines

import (
	"github.com/ubuntu/zsys/internal/config"
	"github.com/ubuntu/zsys/internal/zfs"
	"golang.org/x/xerrors"

	log "github.com/sirupsen/logrus"
)

// ZfsPropertySetter interface tries to set dataset property on a zfs system
type ZfsPropertySetter interface {
	SetProperty(name, value, datasetName string, force bool) error
}

// EnsureBoot consolidate canmount states for early boot.
// A transactional zfs element should be passed to optionally revert if an error is returned (only part of the datasets
// were changed).
// A new machines element should be built afterwards to ensure that datasets attached to State have the correct
// canmount state.
// TODO: propagate error to user
func (machines Machines) EnsureBoot(z ZfsPropertySetter) error {
	if !machines.current.IsZsys {
		log.Debugln("current machine isn't Zsys, nothing to do")
		return nil
	}

	// Start switching every non current machine to noauto
	for _, m := range machines.all {
		if m != machines.current {
			if err := m.switchCanMount(z, "noauto"); err != nil {
				return err
			}
		}
		for _, h := range m.History {
			if err := h.switchCanMount(z, "noauto"); err != nil {
				return err
			}
		}
	}

	// Switch current machine to on (that way, overlapping userdataset will have the correct state)
	return machines.current.switchCanMount(z, "on")
}

// switchCanMount switches for a given state all system and user datasets to canMount state
func (s *State) switchCanMount(z ZfsPropertySetter, canMount string) error {
	ds := append(s.SystemDatasets, s.UserDatasets...)
	for _, d := range ds {
		if d.CanMount == canMount {
			continue
		}
		if err := z.SetProperty(zfs.CanmountProp, canMount, d.Name, false); err != nil {
			return xerrors.Errorf("couldn't switch %q canmount property to %d: "+config.ErrorFormat, d.Name, canMount, err)
		}
	}
	return nil
}
