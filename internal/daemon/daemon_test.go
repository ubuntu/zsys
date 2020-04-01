package daemon_test

import (
	"errors"
	"fmt"
	"net"
	"path/filepath"
	"testing"
	"time"

	"github.com/ubuntu/zsys/internal/daemon"
	"github.com/ubuntu/zsys/internal/testutils"
)

func TestServerStartStop(t *testing.T) {
	/* FIXME: Parallel is disabled for the moment.
	   It leads to a race in the properties cache of the dataset
	   It can be reenabled once the race is fixed
	*/
	//t.Parallel()
	defer testutils.StartLocalSystemBus(t)()

	dir, cleanup := testutils.TempDir(t)
	defer cleanup()

	s, err := daemon.New(filepath.Join(dir, "daemon_test.sock"), daemon.WithLibZFS(testutils.GetMockZFS(t)))
	if err != nil {
		t.Fatalf("expected no error but got: %v", err)
	}

	s.Stop()
}

func TestServerFailingOption(t *testing.T) {
	t.Parallel()
	defer testutils.StartLocalSystemBus(t)()

	if _, err := daemon.New("foo", daemon.FailingOption(), daemon.WithLibZFS(testutils.GetMockZFS(t))); err == nil {
		t.Fatal("expected an error but got none")
	}
}

func TestServerStartListenStop(t *testing.T) {
	//t.Parallel()
	defer testutils.StartLocalSystemBus(t)()

	dir, cleanup := testutils.TempDir(t)
	defer cleanup()

	s, errs := startDaemonAndListen(t, dir, 1000*time.Millisecond)
	// Let the server listen
	time.Sleep(10 * time.Millisecond)
	s.Stop()

	select {
	case <-time.After(100 * time.Second):
		t.Fatalf("server shouldn't have timed out but did")
	case err := <-errs:
		if err != nil {
			t.Fatalf("got an error from the server but expected none: %v", err)
		}
	}
}

func TestServerStartListenTimeout(t *testing.T) {
	//t.Parallel()
	defer testutils.StartLocalSystemBus(t)()

	dir, cleanup := testutils.TempDir(t)
	defer cleanup()

	s, errs := startDaemonAndListen(t, dir, time.Millisecond)
	assertServerTimeout(t, s, errs)
}

func TestServerDontTimeoutOnRequest(t *testing.T) {
	//t.Parallel()
	defer testutils.StartLocalSystemBus(t)()

	dir, cleanup := testutils.TempDir(t)
	defer cleanup()

	s, errs := startDaemonAndListen(t, dir, 10*time.Millisecond)

	reqDone := s.TrackRequest()
	select {
	case <-time.After(1000 * time.Millisecond):
	case <-errs:
		t.Fatalf("server exited prematurely: we had a request in flight. Exited with %v", errs)
	}
	reqDone()

	// wait now for the server to timeout
	assertServerTimeout(t, s, errs)
}

func TestServerDontTimeoutWithMultipleRequests(t *testing.T) {
	//t.Parallel()
	defer testutils.StartLocalSystemBus(t)()

	dir, cleanup := testutils.TempDir(t)
	defer cleanup()

	s, errs := startDaemonAndListen(t, dir, 10*time.Millisecond)

	req1Done := s.TrackRequest()
	req2Done := s.TrackRequest()
	req1Done()
	select {
	case <-time.After(1000 * time.Millisecond):
	case <-errs:
		t.Fatalf("server exited prematurely: we had a request in flight. Exited with %v", errs)
	}
	req2Done()

	// wait now for the server to timeout
	assertServerTimeout(t, s, errs)
}

func TestServerCannotCreateSocket(t *testing.T) {
	t.Parallel()

	_, err := daemon.New("/path/does/not/exist/daemon_test.sock", daemon.WithLibZFS(testutils.GetMockZFS(t)))
	if err == nil {
		t.Fatal("expected an error but got none")
	}
}

func TestServerSocketActivation(t *testing.T) {
	defer testutils.StartLocalSystemBus(t)()

	tests := map[string]struct {
		sockets      []string
		listenerFail bool

		wantErr bool
	}{
		"success with one socket":    {sockets: []string{"sock1"}},
		"fail when Listeners() fail": {listenerFail: true, wantErr: true},
		"fail with many sockets":     {sockets: []string{"socket1", "socket2"}, wantErr: true},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			dir, cleanup := testutils.TempDir(t)
			defer cleanup()

			var listeners []net.Listener
			for _, socket := range tc.sockets {
				l, err := net.Listen("unix", filepath.Join(dir, socket))
				if err != nil {
					t.Fatalf("couldn't create unix socket: %v", err)
				}
				defer l.Close()
				listeners = append(listeners, l)
			}

			var f func() ([]net.Listener, error)
			if len(tc.sockets) == 0 {
				if !tc.listenerFail {
					t.Fatal("expected listenerFail is true and got false")
				}
				f = func() ([]net.Listener, error) {
					return nil, errors.New("systemd activation error")
				}
			} else {
				f = func() ([]net.Listener, error) {
					return listeners, nil
				}
			}

			s, err := daemon.New("foo", daemon.WithSystemdActivationListener(f), daemon.WithLibZFS(testutils.GetMockZFS(t)))
			if tc.wantErr && err == nil {
				t.Fatal("expected an error but none")
			} else if !tc.wantErr && err != nil {
				t.Fatalf("expected no error but got: %v", err)
			}
			if err != nil {
				return
			}

			go func() {
				time.Sleep(10 * time.Millisecond)
				s.Stop()
			}()
			if err := s.Listen(); err != nil {
				t.Fatalf("expected to start listening but couldn't: %v", err)
			}

		})
	}
}

func TestServerSdNotifier(t *testing.T) {
	defer testutils.StartLocalSystemBus(t)()

	tests := map[string]struct {
		sent         bool
		notifierFail bool

		wantErr bool
	}{
		"send signal":                         {sent: true},
		"doesn't fail when not under systemd": {sent: false},
		"fail when notifier fails":            {notifierFail: true, wantErr: true},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			dir, cleanup := testutils.TempDir(t)
			defer cleanup()

			l, err := net.Listen("unix", filepath.Join(dir, "socket"))
			if err != nil {
				t.Fatalf("couldn't create unix socket: %v", err)
			}
			defer l.Close()

			s, err := daemon.New("foo",
				daemon.WithSystemdActivationListener(func() ([]net.Listener, error) { return []net.Listener{l}, nil }),
				daemon.WithSystemdSdNotifier(func(unsetEnvironment bool, state string) (bool, error) {
					if tc.notifierFail {
						return false, errors.New("systemd notifier error")
					}
					return tc.sent, nil
				}),
				daemon.WithLibZFS(testutils.GetMockZFS(t)))
			if !tc.wantErr && err != nil {
				t.Fatalf("expected no error but got: %v", err)
			}

			go func() {
				time.Sleep(10 * time.Millisecond)
				s.Stop()
			}()

			err = s.Listen()
			if tc.wantErr && err == nil {
				t.Fatal("expected an error but none")
			} else if !tc.wantErr && err != nil {
				t.Fatalf("expected no error but got: %v", err)
			}

		})
	}
}

func assertServerTimeout(t *testing.T, s *daemon.Server, errs chan error) {
	t.Helper()

	select {
	case <-time.After(time.Second):
		s.Stop()
		t.Fatalf("server should have timed out, but it didn't")
	case err := <-errs:
		if err != nil {
			t.Fatal(err)
		}
	}
}

func startDaemonAndListen(t *testing.T, dir string, timeout time.Duration) (*daemon.Server, chan error) {
	t.Helper()

	s, err := daemon.New(filepath.Join(dir, "daemon_test.sock"), daemon.WithIdleTimeout(timeout), daemon.WithLibZFS(testutils.GetMockZFS(t)))
	if err != nil {
		t.Fatalf("expected no error but got: %v", err)
	}

	errs := make(chan error)
	go func() {
		if err := s.Listen(); err != nil {
			errs <- fmt.Errorf("Server exited with error: %v", err)
		}
		close(errs)
	}()

	return s, errs
}
