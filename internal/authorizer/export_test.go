package authorizer

import (
	"errors"

	"github.com/godbus/dbus/v5"
)

var (
	WithAuthority  = withAuthority
	WithRoot       = withRoot
	WithUserLookup = withUserLookup
)

type PeerCredsInfo = peerCredsInfo

func NewTestPeerCredsInfo(uid uint32, pid int32) PeerCredsInfo {
	return PeerCredsInfo{uid: uid, pid: pid}
}

type DbusMock struct {
	IsAuthorized    bool
	WantPolkitError bool

	actionRequested Action
}

func (d *DbusMock) Call(method string, flags dbus.Flags, args ...interface{}) *dbus.Call {
	var errPolkit error

	d.actionRequested = Action(args[1].(string))

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
