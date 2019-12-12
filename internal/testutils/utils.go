package testutils

import (
	"flag"
	"io/ioutil"
	"os"
	"testing"
)

var withSystemZFS *bool

func init() {
	withSystemZFS = flag.Bool("with-system-zfs", false, "use system's libzfs to run the tests")
}

// TempDir creates a temporary directory and returns the created directory and a teardown removal function to defer
func TempDir(t *testing.T) (string, func()) {
	t.Helper()

	dir, err := ioutil.TempDir("", "zsystest-")
	if err != nil {
		t.Fatal("can't create temporary directory", err)
	}
	return dir, func() {
		if err = os.RemoveAll(dir); err != nil {
			t.Error("can't clean temporary directory", err)
		}
	}
}

// UseSystemZFS returns true if the flag --with-system-zfs is set to run the tests
func UseSystemZFS() bool {
	return withSystemZFS != nil && *withSystemZFS
}
