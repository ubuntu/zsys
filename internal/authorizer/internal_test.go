package authorizer

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIsAllowed(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		action          Action
		pid             int32
		uid             uint32
		polkitAuthorize bool

		wantAuthorized  bool
		wantPolkitError bool
	}{
		"Root is always authorized":             {uid: 0, wantAuthorized: true},
		"ActionAlwaysAllowed is always allowed": {action: ActionAlwaysAllowed, uid: 1000, wantAuthorized: true},
		"Valid process and ACK":                 {pid: 10000, uid: 1000, polkitAuthorize: true, wantAuthorized: true},
		"Valid process and NACK":                {pid: 10000, uid: 1000, polkitAuthorize: false, wantAuthorized: false},

		"Process doesn't exists":                         {pid: 99999, uid: 1000, polkitAuthorize: true, wantAuthorized: false},
		"Invalid process stat file: missing )":           {pid: 10001, uid: 1000, polkitAuthorize: true, wantAuthorized: false},
		"Invalid process stat file: ) at the end":        {pid: 10002, uid: 1000, polkitAuthorize: true, wantAuthorized: false},
		"Invalid process stat file: field isn't present": {pid: 10003, uid: 1000, polkitAuthorize: true, wantAuthorized: false},
		"Invalid process stat file: field isn't an int":  {pid: 10004, uid: 1000, polkitAuthorize: true, wantAuthorized: false},

		"Polkit dbus call errors out": {wantPolkitError: true, pid: 10000, uid: 1000, polkitAuthorize: true, wantAuthorized: false},
	}
	for name, tc := range tests {
		tc := tc
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if tc.action == "" {
				tc.action = ActionManageService
			}

			d := DbusMock{
				IsAuthorized:    tc.polkitAuthorize,
				WantPolkitError: tc.wantPolkitError}
			a, err := New(WithAuthority(d), WithRoot("testdata"))
			if err != nil {
				t.Fatalf("Failed to create authorizer: %v", err)
			}

			allowed := a.isAllowed(context.Background(), tc.action, tc.pid, tc.uid)

			assert.Equal(t, tc.wantAuthorized, allowed, "isAllowed returned state match expectations")
		})
	}
}
