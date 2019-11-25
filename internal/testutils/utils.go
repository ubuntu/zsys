package testutils

import (
	"io/ioutil"
	"os"
	"testing"
)

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
