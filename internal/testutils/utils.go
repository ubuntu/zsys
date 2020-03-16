package testutils

import (
	"encoding/json"
	"flag"
	"io/ioutil"
	"os"
	"testing"
)

var withSystemZFS *bool

func init() {
	withSystemZFS = flag.Bool("with-system-zfs", false, "use system's libzfs to run the tests")
}

type tester interface {
	Helper()
	Error(args ...interface{})
	Fatal(args ...interface{})
	Fatalf(format string, args ...interface{})
	Logf(format string, args ...interface{})
}

// TempDir creates a temporary directory and returns the created directory and a teardown removal function to defer
func TempDir(t tester) (string, func()) {
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

// Deepcopy makes a deep copy from src into dst.
// Note that private fields won’t be copied unless you override
// by json Marshalling and Unmarshalling.
func Deepcopy(t *testing.T, dst interface{}, src interface{}) {
	t.Helper()

	if dst == nil {
		t.Fatal("deepcopy: dst cannot be nil")
	}
	if src == nil {
		t.Fatal("deepcopy: src cannot be nil")
	}
	bytes, err := json.Marshal(src)
	if err != nil {
		t.Fatalf("deepcopy: Unable to marshal src: %s", err)
	}
	err = json.Unmarshal(bytes, dst)
	if err != nil {
		t.Fatalf("deepcopy: Unable to unmarshal into dst: %s", err)
	}
	return
}
