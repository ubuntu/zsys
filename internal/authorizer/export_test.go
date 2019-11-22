package authorizer

import (
	"errors"

	"github.com/godbus/dbus"
)

var (
	WithAuthority = withAuthority
	WithRoot      = withRoot
)

type PeerCredsInfo = peerCredsInfo

func NewTestPeerCredsInfo(uid uint32, pid int32) PeerCredsInfo {
	return PeerCredsInfo{uid: uid, pid: pid}
}

type DbusMock struct {
	IsAuthorized    bool
	WantPolkitError bool
}

func (d DbusMock) Call(method string, flags dbus.Flags, args ...interface{}) *dbus.Call {
	var errPolkit error

	if d.WantPolkitError {
		errPolkit = errors.New("Polkit error")
	}

	return &dbus.Call{
		Err: errPolkit,
		Body: []interface{}{
			[]interface{}{
				d.IsAuthorized,
				true,
				map[string]string{
					"polkit.retains_authorization_after_challenge": "true",
					"polkit.dismissed": "true",
				},
			},
		},
	}
}
