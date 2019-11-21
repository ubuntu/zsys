package authorizer_test

import (
	"context"
	"errors"
	"testing"

	"github.com/godbus/dbus"
	"github.com/stretchr/testify/assert"
	"github.com/ubuntu/zsys/internal/authorizer"
)

func TestIsAllowed(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		action authorizer.Action
		pid    int32
		uid    uint32

		wantAuthorized  bool
		wantPolkitError bool
	}{
		"Root is always authorized":             {uid: 0, wantAuthorized: true},
		"ActionAlwaysAllowed is always allowed": {action: authorizer.ActionAlwaysAllowed, uid: 1000, wantAuthorized: true},
		"Valid process and ACK":                 {pid: 10000, uid: 1000, wantAuthorized: true},
		"Valid process and NACK":                {pid: 10000, uid: 1000, wantAuthorized: false},

		"Process doesn't exists":                         {pid: 99999, uid: 1000, wantAuthorized: false},
		"Invalid process stat file: missing )":           {pid: 10001, uid: 1000, wantAuthorized: false},
		"Invalid process stat file: ) at the end":        {pid: 10002, uid: 1000, wantAuthorized: false},
		"Invalid process stat file: field isn't present": {pid: 10003, uid: 1000, wantAuthorized: false},
		"Invalid process stat file: field isn't an int":  {pid: 10004, uid: 1000, wantAuthorized: false},

		"Polkit dbus call errors out": {wantPolkitError: true, pid: 10000, uid: 1000, wantAuthorized: false},
	}
	for name, tc := range tests {
		tc := tc
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if tc.action == "" {
				tc.action = authorizer.ActionManageService
			}

			d := dbusMock{
				isAuthorized:    tc.wantAuthorized,
				wantPolkitError: tc.wantPolkitError}
			a, err := authorizer.New(authorizer.WithAuthority(d), authorizer.WithRoot("testdata"))
			if err != nil {
				t.Fatalf("Failed to create authorizer: %v", err)
			}

			allowed := a.IsAllowed(context.Background(), tc.action, tc.pid, tc.uid)

			assert.Equal(t, tc.wantAuthorized, allowed, "IsAllowed returned state match expectations")
		})
	}
}

type dbusMock struct {
	isAuthorized    bool
	wantPolkitError bool
}

func (d dbusMock) Call(method string, flags dbus.Flags, args ...interface{}) *dbus.Call {
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
