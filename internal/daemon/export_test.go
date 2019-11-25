package daemon

import (
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
