package zfs_test

import (
	"encoding/json"
	"flag"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/ubuntu/zsys/internal/config"
	"github.com/ubuntu/zsys/internal/zfs"
)

var (
	update = flag.Bool("update", false, "update golden files")
)

func init() {
	config.SetVerboseMode(true)
}

func TestScan(t *testing.T) {
	tests := map[string]struct {
		def string

		wantErr bool
	}{
		"basic": {def: "basic.yaml"},
		//		"basic2": {def: "basic.yaml"},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			dir, cleanup := tempDir(t)
			defer cleanup()
			fPools := newFakePools(t, filepath.Join("testdata", tc.def))
			defer fPools.create(dir, strings.Replace(t.Name(), "/", "_", -1))()

			z := zfs.New()
			got, err := z.Scan()
			if err != nil {
				if !tc.wantErr {
					t.Fatalf("expected no error but got: %v", err)
				}
				return
			}
			if tc.wantErr {
				t.Fatal("expected an error but got none")
			}

			var want []zfs.Dataset
			loadFromGoldenFile(t, got, &want)

			if diff := cmp.Diff(want, got); diff != "" {
				t.Errorf("Scan() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func tempDir(t *testing.T) (string, func()) {
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

	/*dir := "/tmp/zsystmp"
	os.RemoveAll(dir)
	os.Mkdir(dir, 0777)

	return dir, func() {}*/

}

// loadFromGoldenFile loads expected content to "want", after optionally refreshing it
// from "got" if udpate flag is passed
func loadFromGoldenFile(t *testing.T, got interface{}, want interface{}) {
	t.Helper()

	testDirname := strings.ToLower(strings.Split(t.Name(), "/")[0])
	nparts := strings.Split(t.Name(), "/")
	n := nparts[len(nparts)-1]

	goldenFile := filepath.Join("testdata", testDirname, n+".golden")
	if *update {
		b, err := json.MarshalIndent(got, "", "   ")
		if err != nil {
			t.Fatal("couldn't convert to json:", err)
		}
		if err := ioutil.WriteFile(goldenFile, b, 0644); err != nil {
			t.Fatal("couldn't save golden file:", err)
		}
	}
	b, err := ioutil.ReadFile(goldenFile)
	if err != nil {
		t.Fatal("couldn't read golden file")
	}

	if err := json.Unmarshal(b, &want); err != nil {
		t.Fatal("couldn't convert golden file content to structure:", err)
	}
}
