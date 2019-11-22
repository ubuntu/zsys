package authorizer

import (
	"errors"

	"github.com/godbus/dbus"
)

var (
	WithAuthority = withAuthority
	WithRoot      = withRoot
)

type DbusMock struct {
	isAuthorized    bool
	wantPolkitError bool
}

func (d DbusMock) Call(method string, flags dbus.Flags, args ...interface{}) *dbus.Call {
	var errPolkit error

	if d.wantPolkitError {
		errPolkit = errors.New("Polkit error")
	}

	return &dbus.Call{
		Err: errPolkit,
		Body: []interface{}{
			[]interface{}{
				d.isAuthorized,
				true,
				map[string]string{
					"polkit.retains_authorization_after_challenge": "true",
					"polkit.dismissed": "true",
				},
			},
		},
	}
}
