package authorizer

import (
	"context"
	"fmt"
	"io/ioutil"
	"net"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/ubuntu/zsys/internal/testutils"
)

func TestIsAllowed(t *testing.T) {
	t.Parallel()
	defer StartLocalSystemBus(t)()

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

			errAllowed := a.isAllowed(context.Background(), tc.action, tc.pid, tc.uid)

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
			t.Fatalf("Couldn't resolve client socket address: %v", err)
		}
		conn, err := net.DialUnix("unix", nil, unixAddr)
		if err != nil {
			t.Fatalf("Couldn't contact unix socket: %v", err)
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

var (
	sdbus sync.Once
	wg    sync.WaitGroup
)

func StartLocalSystemBus(t *testing.T) func() {
	t.Helper()
	cleanupFunc := func() { wg.Done() }

	wg.Add(1)
	sdbus.Do(func() {
		dir, cleanup := testutils.TempDir(t)
		savedDbusSystemAddress := os.Getenv("DBUS_SYSTEM_BUS_ADDRESS")
		config := filepath.Join(dir, "dbus.config")
		ioutil.WriteFile(config, []byte(`<!DOCTYPE busconfig PUBLIC "-//freedesktop//DTD D-Bus Bus Configuration 1.0//EN"
 "http://www.freedesktop.org/standards/dbus/1.0/busconfig.dtd">
<busconfig>
  <type>system</type>
  <keep_umask/>
  <listen>unix:tmpdir=/tmp</listen>
  <standard_system_servicedirs />
  <policy context="default">
    <allow send_destination="*" eavesdrop="true"/>
    <allow eavesdrop="true"/>
    <allow own="*"/>
  </policy>
</busconfig>`), 0666)
		ctx, stopDbus := context.WithCancel(context.Background())
		cmd := exec.CommandContext(ctx, "dbus-daemon", "--print-address=1", "--config-file="+config)
		dbusStdout, err := cmd.StdoutPipe()
		if err != nil {
			t.Fatalf("couldn't get stdout of dbus-daemon: %v", err)
		}
		if err := cmd.Start(); err != nil {
			t.Fatalf("couldn't start dbus-daemon: %v", err)
		}
		dbusAddr := make([]byte, 256)
		n, err := dbusStdout.Read(dbusAddr)
		if err != nil {
			t.Fatalf("couldn't get dbus address: %v", err)
		}
		dbusAddr = dbusAddr[:n]
		if err := os.Setenv("DBUS_SYSTEM_BUS_ADDRESS", string(dbusAddr)); err != nil {
			t.Fatalf("couldn't set DBUS_SYSTEM_BUS_ADDRESS: %v", err)
		}

		cleanupFunc = func() {
			wg.Done()
			wg.Wait()

			stopDbus()
			cmd.Wait()

			if err := os.Setenv("DBUS_SYSTEM_BUS_ADDRESS", savedDbusSystemAddress); err != nil {
				t.Errorf("couldn't restore DBUS_SYSTEM_BUS_ADDRESS: %v", err)
			}
			cleanup()
		}
	})

	return cleanupFunc
}
