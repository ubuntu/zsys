package authorizer

import (
	"context"
	"fmt"
	"io/ioutil"
	"net"
	"os"
	"os/user"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/ubuntu/zsys/internal/testutils"
)

func TestIsAllowed(t *testing.T) {
	t.Parallel()
	defer testutils.StartLocalSystemBus(t)()

	tests := map[string]struct {
		action    Action
		pid       int32
		uid       uint32
		actionUID uint32

		polkitAuthorize bool

		wantActionRequested Action
		wantAuthorized      bool
		wantPolkitError     bool
	}{
		"Root is always authorized":             {uid: 0, wantAuthorized: true},
		"ActionAlwaysAllowed is always allowed": {action: ActionAlwaysAllowed, uid: 1000, wantAuthorized: true},
		"Valid process and ACK":                 {pid: 10000, uid: 1000, polkitAuthorize: true, wantAuthorized: true},
		"Valid process and NACK":                {pid: 10000, uid: 1000, polkitAuthorize: false, wantAuthorized: false},

		"ActionUserWrite on its own datasets is transformed on actionUserWriteSelf": {action: ActionUserWrite, actionUID: 1000, pid: 10000, uid: 1000, wantActionRequested: actionUserWriteSelf},
		"ActionUserWrite on other datasets is transformed on actionUserWriteOthers": {action: ActionUserWrite, actionUID: 999, pid: 10000, uid: 1000, wantActionRequested: actionUserWriteOthers},

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

			d := &DbusMock{
				IsAuthorized:    tc.polkitAuthorize,
				WantPolkitError: tc.wantPolkitError}
			a, err := New(WithAuthority(d), WithRoot("testdata"))
			if err != nil {
				t.Fatalf("Failed to create authorizer: %v", err)
			}

			errAllowed := a.isAllowed(context.Background(), tc.action, tc.pid, tc.uid, tc.actionUID)

			if tc.wantActionRequested != "" {
				assert.Equal(t, string(tc.wantActionRequested), string(d.actionRequested), "Unexpected action received by polkit")
			}

			assert.Equal(t, tc.wantAuthorized, errAllowed == nil, "isAllowed returned state match expectations")
		})
	}
}

func TestPeerCredsInfoAuthType(t *testing.T) {
	t.Parallel()

	p := peerCredsInfo{
		uid: 11111,
		pid: 22222,
	}
	assert.Equal(t, "uid: 11111, pid: 22222", p.AuthType(), "AuthType returns expected uid and pid")
}
func TestServerPeerCredsHandshake(t *testing.T) {
	t.Parallel()

	s := serverPeerCreds{}
	d, err := ioutil.TempDir(os.TempDir(), "zsystest")
	if err != nil {
		t.Fatalf("Failed to create temporary directory: %v", err)
	}
	defer os.RemoveAll(d)

	socket := filepath.Join(d, "zsys.sock")
	l, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatalf("Couldn't listen on socket: %v", err)
	}
	defer l.Close()

	wg := sync.WaitGroup{}
	wg.Add(1)
	go func() {
		defer wg.Done()
		unixAddr, err := net.ResolveUnixAddr("unix", socket)
		if err != nil {
			t.Errorf("Couldn't resolve client socket address: %v", err)
			return
		}
		conn, err := net.DialUnix("unix", nil, unixAddr)
		if err != nil {
			t.Errorf("Couldn't contact unix socket: %v", err)
			return
		}
		defer conn.Close()
	}()

	conn, err := l.Accept()
	if err != nil {
		t.Fatalf("Couldn't accept connexion from client: %v", err)
	}

	c, i, err := s.ServerHandshake(conn)
	if err != nil {
		t.Fatalf("Server handshake failed unexpectedly: %v", err)
	}
	if c == nil {
		t.Error("Received connexion is nil when we expected it not to")
	}

	user, err := user.Current()
	if err != nil {
		t.Fatalf("Couldn't retrieve current user: %v", err)
	}

	assert.Equal(t, fmt.Sprintf("uid: %s, pid: %d", user.Uid, os.Getpid()),
		i.AuthType(), "uid or pid received doesn't match what we expected")

	l.Close()
	wg.Wait()
}
func TestServerPeerCredsInvalidSocket(t *testing.T) {
	t.Parallel()

	s := serverPeerCreds{}
	s.ServerHandshake(nil)
}
