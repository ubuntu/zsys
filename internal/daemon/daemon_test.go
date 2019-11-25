package daemon_test

import (
	"fmt"
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

	dir, cleanup := testutils.TempDir(t)
	defer cleanup()

	s, err := daemon.New(filepath.Join(dir, "daemon_test.sock"))
	if err != nil {
		t.Fatalf("expected no error but got: %v", err)
	}

	s.Stop()
}
func TestServerStartListenStop(t *testing.T) {
	//t.Parallel()

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

	dir, cleanup := testutils.TempDir(t)
	defer cleanup()

	s, errs := startDaemonAndListen(t, dir, time.Millisecond)
	assertServerTimeout(t, s, errs)
}

func TestServerDontTimeoutOnRequest(t *testing.T) {
	//t.Parallel()

	dir, cleanup := testutils.TempDir(t)
	defer cleanup()

	s, errs := startDaemonAndListen(t, dir, 10*time.Millisecond)

	reqDone := s.TrackRequest()
	select {
	case <-time.After(1000 * time.Millisecond):
	case <-errs:
		t.Fatalf("server exited prematurily: we had a request in flight. Exited with %v", errs)
	}
	reqDone()

	// wait now for the server to timeout
	assertServerTimeout(t, s, errs)
}

func TestServerDontTimeoutWithMultipleRequests(t *testing.T) {
	//t.Parallel()

	dir, cleanup := testutils.TempDir(t)
	defer cleanup()

	s, errs := startDaemonAndListen(t, dir, 10*time.Millisecond)

	req1Done := s.TrackRequest()
	req2Done := s.TrackRequest()
	req1Done()
	select {
	case <-time.After(1000 * time.Millisecond):
	case <-errs:
		t.Fatalf("server exited prematurily: we had a request in flight. Exited with %v", errs)
	}
	req2Done()

	// wait now for the server to timeout
	assertServerTimeout(t, s, errs)
}

func TestServerCannotCreateSocket(t *testing.T) {
	//t.Parallel()

	_, err := daemon.New("/path/does/not/exist/daemon_test.sock")
	if err == nil {
		t.Fatalf("expected an error but got none")
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

	s, err := daemon.New(filepath.Join(dir, "daemon_test.sock"), daemon.IdleTimeout(timeout))
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
