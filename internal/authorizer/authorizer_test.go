package authorizer_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/ubuntu/zsys/internal/authorizer"
	"google.golang.org/grpc/peer"
)

func TestIsAllowedFromContext(t *testing.T) {
	t.Parallel()
	defer authorizer.StartLocalSystemBus(t)()

	tests := map[string]struct {
		action authorizer.Action
		pid    int32
		uid    uint32

		wantAuthorized  bool
		wantPolkitError bool
	}{
		"Root is always authorized": {uid: 0, wantAuthorized: true},
		"Valid process and ACK":     {pid: 10000, uid: 1000, wantAuthorized: true},
		"Valid process and NACK":    {pid: 10000, uid: 1000, wantAuthorized: false},
	}
	for name, tc := range tests {
		tc := tc
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if tc.action == "" {
				tc.action = authorizer.ActionManageService
			}

			p := peer.Peer{
				AuthInfo: authorizer.NewTestPeerCredsInfo(tc.uid, tc.pid),
			}
			ctx := peer.NewContext(context.Background(), &p)

			d := authorizer.DbusMock{
				IsAuthorized:    tc.wantAuthorized,
				WantPolkitError: tc.wantPolkitError}
			a, err := authorizer.New(authorizer.WithAuthority(d), authorizer.WithRoot("testdata"))
			if err != nil {
				t.Fatalf("Failed to create authorizer: %v", err)
			}

			errAllowed := a.IsAllowedFromContext(ctx, tc.action)

			assert.Equal(t, tc.wantAuthorized, errAllowed == nil, "IsAllowedFromContext returned state match expectations")
		})
	}
}

func TestIsAllowedFromContextWithoutPeer(t *testing.T) {
	t.Parallel()
	defer authorizer.StartLocalSystemBus(t)()

	a, err := authorizer.New()
	if err != nil {
		t.Fatalf("Failed to create authorizer: %v", err)
	}

	errAllowed := a.IsAllowedFromContext(context.Background(), authorizer.ActionAlwaysAllowed)
	assert.Equal(t, false, errAllowed == nil, "IsAllowedFromContext must deny without peer creds info")
}

func TestIsAllowedFromContextWithInvalidPeerCreds(t *testing.T) {
	t.Parallel()
	defer authorizer.StartLocalSystemBus(t)()

	a, err := authorizer.New()
	if err != nil {
		t.Fatalf("Failed to create authorizer: %v", err)
	}

	p := peer.Peer{
		AuthInfo: invalidPeerCredsInfo{},
	}
	ctx := peer.NewContext(context.Background(), &p)

	errAllowed := a.IsAllowedFromContext(ctx, authorizer.ActionAlwaysAllowed)
	assert.Equal(t, false, errAllowed == nil, "IsAllowedFromContext must deny with an unexpected peer creds info type")
}

type invalidPeerCredsInfo struct{}

func (invalidPeerCredsInfo) AuthType() string { return "" }
