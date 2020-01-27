// Package authorizer deals client authorization based on a definite set of polkit actions.
// The client uid and pid are obtained via the unix socket (SO_PEERCRED) information,
// that are attached to the grpc request by the server.
package authorizer

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/godbus/dbus/v5"
	"github.com/ubuntu/zsys/internal/i18n"
	"github.com/ubuntu/zsys/internal/log"
	"google.golang.org/grpc/peer"
)

type caller interface {
	Call(method string, flags dbus.Flags, args ...interface{}) *dbus.Call
}

// Authorizer is an abstraction of polkit authorization.
type Authorizer struct {
	authority  caller
	userLookup func(string) (*user.User, error)
	pid        uint32
	uid        uint32

	root string
}

func withAuthority(c caller) func(*Authorizer) {
	return func(a *Authorizer) {
		a.authority = c
	}
}

func withUserLookup(userLookup func(string) (*user.User, error)) func(*Authorizer) {
	return func(a *Authorizer) {
		a.userLookup = userLookup
	}
}

func withRoot(root string) func(*Authorizer) {
	return func(a *Authorizer) {
		a.root = root
	}
}

// New returns a new authorizer.
func New(options ...func(*Authorizer)) (*Authorizer, error) {
	bus, err := dbus.SystemBus()
	if err != nil {
		return nil, err
	}
	authority := bus.Object("org.freedesktop.PolicyKit1",
		"/org/freedesktop/PolicyKit1/Authority")

	a := Authorizer{
		authority:  authority,
		root:       "/",
		userLookup: user.Lookup,
	}

	for _, option := range options {
		option(&a)
	}

	return &a, nil
}

// Action is an polkit action
type Action string

//go:generate go run ../generators/copy.go com.ubuntu.zsys.policy polkit-1/actions ../../generated
const (
	// ActionAlwaysAllowed is a no-op bypassing any user or dbus checks.
	ActionAlwaysAllowed Action = "always-allowed"
	// ActionManageService is the action to perform read operations.
	ActionManageService Action = "com.ubuntu.zsys.manage-service"
	// ActionSystemList is the action to perform system list operations.
	ActionSystemList Action = "com.ubuntu.zsys.system-list"
	// ActionSystemWrite is the action to perform system write operations.
	ActionSystemWrite Action = "com.ubuntu.zsys.system-write"

	// ActionUserWrite is the action which will be transformed to Self or Others depending on the request and requester.
	ActionUserWrite Action = "internal-for-actionUserWriteSelf-or-actionUserWriteOthers-based-on-uid"
	// actionUserWriteSelf is the action to perform user write operations on own user's datasets.
	actionUserWriteSelf Action = "com.ubuntu.zsys.user-write-self"
	// actionUserWriteOthers is the action to perform user operation on other user's datasets.
	actionUserWriteOthers Action = "com.ubuntu.zsys.user-write-others"
)

type polkitCheckFlags uint32

const (
	checkAllowInteration polkitCheckFlags = 0x01
)

type onUserKey string

// OnUserKey is the authorizer context key passing optional user name
var OnUserKey onUserKey = "UserName"

type authSubject struct {
	Kind    string
	Details map[string]dbus.Variant
}

type authResult struct {
	IsAuthorized bool
	IsChallenge  bool
	Details      map[string]string
}

// IsAllowedFromContext returns nil if the user is allowed to perform an operation.
// The pid and uid are extracted from peerCredsInfo grpc context
func (a Authorizer) IsAllowedFromContext(ctx context.Context, action Action) (err error) {
	log.Debug(ctx, i18n.G("Check if grpc request peer is authorized"))

	defer func() {
		if err != nil {
			err = fmt.Errorf(i18n.G("Permission denied: %w"), err)
		}
	}()

	p, ok := peer.FromContext(ctx)
	if !ok {
		return errors.New(i18n.G("Context request doesn't have grpc peer creds informations."))
	}
	pci, ok := p.AuthInfo.(peerCredsInfo)
	if !ok {
		return errors.New(i18n.G("Context request grpc peer creeds information is not a peerCredsInfo."))
	}

	var actionUID uint32
	if action == ActionUserWrite {
		userName, ok := ctx.Value(OnUserKey).(string)
		if !ok {
			return errors.New(i18n.G("Request to act on user dataset should have a user name attached"))
		}
		user, err := a.userLookup(userName)
		if err != nil {
			return fmt.Errorf(i18n.G("Couldn't retrieve user for %q: %v"), userName, err)
		}
		uid, err := strconv.Atoi(user.Uid)
		if err != nil {
			return fmt.Errorf(i18n.G("Couldn't convert %q to a valid uid for %q"), user.Uid, userName)
		}
		actionUID = uint32(uid)
	}

	return a.isAllowed(ctx, action, pci.pid, pci.uid, actionUID)
}

// isAllowed returns nil if the user is allowed to perform an operation.
// ActionUID is only used for ActionUserWrite which will be converted to corresponding polkit action
// (self or others)
func (a Authorizer) isAllowed(ctx context.Context, action Action, pid int32, uid uint32, actionUID uint32) error {
	if uid == 0 {
		log.Debug(ctx, i18n.G("Authorized as being administrator"))
		return nil
	} else if action == ActionAlwaysAllowed {
		log.Debug(ctx, i18n.G("Any user always authorized"))
		return nil
	} else if action == ActionUserWrite {
		action = actionUserWriteOthers
		if actionUID == uid {
			action = actionUserWriteSelf
		}
	}

	f, err := os.Open(filepath.Join(a.root, fmt.Sprintf("proc/%d/stat", pid)))
	if err != nil {
		return fmt.Errorf(i18n.G("Couldn't open stat file for process: %v"), err)
	}
	defer f.Close()

	startTime, err := getStartTimeFromReader(f)
	if err != nil {
		return fmt.Errorf(i18n.G("Couldn't determine start time of client process: %v"), err)
	}

	subject := authSubject{
		Kind: "unix-process",
		Details: map[string]dbus.Variant{
			"pid":        dbus.MakeVariant(uint32(pid)), // polkit requests an uint32 on dbus
			"start-time": dbus.MakeVariant(startTime),
			"uid":        dbus.MakeVariant(uid),
		},
	}

	var result authResult
	var details map[string]string
	err = a.authority.Call(
		"org.freedesktop.PolicyKit1.Authority.CheckAuthorization", dbus.FlagAllowInteractiveAuthorization,
		subject, string(action), details, checkAllowInteration, "").Store(&result)
	if err != nil {
		return fmt.Errorf(i18n.G("Call to polkit failed: %v"), err)
	}

	log.Debugf(ctx, i18n.G("Polkit call result, authorized: %t"), result.IsAuthorized)

	if !result.IsAuthorized {
		return errors.New(i18n.G("Polkit denied access"))
	}
	return nil
}

// getStartTimeFromReader determines the start time from a process stat file content
//
// The implementation is intended to be compatible with polkit:
//    https://cgit.freedesktop.org/polkit/tree/src/polkit/polkitunixprocess.c
func getStartTimeFromReader(r io.Reader) (uint64, error) {
	data, err := ioutil.ReadAll(r)
	if err != nil {
		return 0, err
	}
	contents := string(data)

	// start time is the token at index 19 after the '(process
	// name)' entry - since only this field can contain the ')'
	// character, search backwards for this to avoid malicious
	// processes trying to fool us
	//
	// See proc(5) man page for a description of the
	// /proc/[pid]/stat file format and the meaning of the
	// starttime field.
	idx := strings.IndexByte(contents, ')')
	if idx < 0 {
		return 0, errors.New(i18n.G("parsing error: missing )"))
	}
	idx += 2 // skip ") "
	if idx > len(contents) {
		return 0, errors.New(i18n.G("parsing error: ) at the end"))
	}
	tokens := strings.Split(contents[idx:], " ")
	if len(tokens) < 20 {
		return 0, errors.New(i18n.G("parsing error: less fields than required"))
	}
	v, err := strconv.ParseUint(tokens[19], 10, 64)
	if err != nil {
		return 0, fmt.Errorf(i18n.G("parsing error: %v"), err)
	}
	return v, nil
}
