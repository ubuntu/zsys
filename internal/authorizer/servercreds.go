package authorizer

import (
	"context"
	"fmt"
	"net"

	"golang.org/x/sys/unix"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

// WithUnixPeerCreds returns the credentials of the caller
func WithUnixPeerCreds() grpc.ServerOption {
	return grpc.Creds(serverPeerCreds{})
}

// serverPeerCreds encapsulates a TransportCredentials which extracts uid and pid of caller via Unix Socket SO_PEERCRED
type serverPeerCreds struct{}

func (serverPeerCreds) ServerHandshake(conn net.Conn) (net.Conn, credentials.AuthInfo, error) {
	var cred *unix.Ucred

	// net.Conn is an interface. Expect only *net.UnixConn types
	uc, ok := conn.(*net.UnixConn)
	if !ok {
		return conn, nil, fmt.Errorf("unexpected socket type")
	}

	// Fetches raw network connection from UnixConn
	raw, err := uc.SyscallConn()
	if err != nil {
		return conn, nil, fmt.Errorf("error opening raw connection: %s", err)
	}

	// The raw.Control() callback does not return an error directly.
	// In order to capture errors, we wrap already defined variable
	// 'err' within the closure. 'err2' is then the error returned
	// by Control() itself.
	err2 := raw.Control(func(fd uintptr) {
		cred, err = unix.GetsockoptUcred(int(fd),
			unix.SOL_SOCKET,
			unix.SO_PEERCRED)
	})
	if err != nil {
		return conn, nil, fmt.Errorf("GetsockoptUcred() error: %s", err)
	}
	if err2 != nil {
		return conn, nil, fmt.Errorf("Control() error: %s", err2)
	}

	return conn, peerCredsInfo{uid: cred.Uid, pid: cred.Pid}, nil
}
func (serverPeerCreds) ClientHandshake(ctx context.Context, authority string, conn net.Conn) (net.Conn, credentials.AuthInfo, error) {
	return conn, nil, nil
}
func (serverPeerCreds) Info() credentials.ProtocolInfo {
	return credentials.ProtocolInfo{}
}
func (serverPeerCreds) Clone() credentials.TransportCredentials {
	return nil
}
func (serverPeerCreds) OverrideServerName(s string) error {
	return nil
}

type peerCredsInfo struct {
	uid uint32
	pid int32
}

// AuthType returns a string encrypting uid and pid of caller.
func (p peerCredsInfo) AuthType() string {
	return fmt.Sprintf("%d,%d", p.uid, p.pid)
}
