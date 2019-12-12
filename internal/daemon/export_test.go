package daemon

import (
	"errors"
	"net"
)

func WithSystemdActivationListener(f func() ([]net.Listener, error)) func(o *options) error {
	return func(o *options) error {
		o.systemdActivationListener = f
		return nil
	}
}

func WithSystemdSdNotifier(f func(unsetEnvironment bool, state string) (bool, error)) func(o *options) error {
	return func(o *options) error {
		o.systemdSdNotifier = f
		return nil
	}
}

func FailingOption() func(o *options) error {
	return func(o *options) error {
		return errors.New("failing option")
	}
}
