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
	"path/filepath"
	"strconv"
	"strings"

	"github.com/godbus/dbus"
	"github.com/ubuntu/zsys/internal/i18n"
	"github.com/ubuntu/zsys/internal/log"
	"google.golang.org/grpc/peer"
)

type caller interface {
	Call(method string, flags dbus.Flags, args ...interface{}) *dbus.Call
}

// Authorizer is an abstraction of polkit authorization.
type Authorizer struct {
	authority caller
	pid       uint32
	uid       uint32

	root string
}

func withAuthority(c caller) func(*Authorizer) {
	return func(a *Authorizer) {
		a.authority = c
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
		authority: authority,
		root:      "/",
	}

	for _, option := range options {
		option(&a)
	}

	return &a, nil
}

// Action is an polkit action
type Action string

const (
	// ActionAlwaysAllowed is a no-op bypassing any user or dbus checks.
	ActionAlwaysAllowed Action = "always-allowed"
	// ActionManageService is the action to perform read operations.
	ActionManageService Action = "com.ubuntu.zsys.manage-service"
	// ActionSystemList is the action to perform system list operations.
	ActionSystemList Action = "com.ubuntu.zsys.system-list"
	// ActionSystemWrite is the action to perform system write operations.
	ActionSystemWrite Action = "com.ubuntu.zsys.system-write"
	// ActionUserWriteSelf is the action to perform user write operations on own user's datasets.
	ActionUserWriteSelf Action = "com.ubuntu.zsys.user-write-self"
	//ActionUserWriteOthers is the action to perform user operation on other user's datasets.
	ActionUserWriteOthers Action = "com.ubuntu.zsys.user-write-others"
)

type polkitCheckFlags uint32

const (
	checkAllowInteration polkitCheckFlags = 0x01
)

type authSubject struct {
	Kind    string
	Details map[string]dbus.Variant
}

type authResult struct {
	IsAuthorized bool
	IsChallenge  bool
	Details      map[string]string
}

// IsAllowedFromContext returns if the user is allowed to perform an operation.
// The pid and uid are extracted from peerCredsInfo grpc context
func (a Authorizer) IsAllowedFromContext(ctx context.Context, action Action) bool {
	log.Debug(ctx, i18n.G("Check if grpc request peer is authorized"))

	p, ok := peer.FromContext(ctx)
	if !ok {
		log.Warning(ctx, i18n.G("Context request doesn't have grpc peer creds informations. Denying request."))
		return false
	}
	pci, ok := p.AuthInfo.(peerCredsInfo)
	if !ok {
		log.Warning(ctx, i18n.G("Context request grpc peer creeds information is not a peerCredsInfo. Denying request."))
		return false
	}

	return a.isAllowed(ctx, action, pci.pid, pci.uid)
}

// isAllowed returns if the user is allowed to perform an operation.
func (a Authorizer) isAllowed(ctx context.Context, action Action, pid int32, uid uint32) bool {
	if uid == 0 {
		log.Debug(ctx, i18n.G("Authorized as being administrator"))
		return true
	} else if action == ActionAlwaysAllowed {
		log.Debug(ctx, i18n.G("Any user always authorized"))
		return true
	}

	f, err := os.Open(filepath.Join(a.root, fmt.Sprintf("proc/%d/stat", pid)))
	if err != nil {
		log.Errorf(ctx, i18n.G("Couldn't open stat file for process: %v"), err)
		return false
	}
	defer f.Close()

	startTime, err := getStartTimeFromReader(f)
	if err != nil {
		log.Errorf(ctx, i18n.G("Couldn't determine start time of client process: %v"), err)
		return false
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
		log.Errorf(ctx, i18n.G("Call to polkit failed: %v"), err)
		return false
	}

	log.Debugf(ctx, i18n.G("Polkit call result, authorized: %t"), result.IsAuthorized)

	return result.IsAuthorized
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
