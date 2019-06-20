package zfs_test

import (
	"encoding/json"
	"flag"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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
		"One pool, N datasets, N children":                                         {def: "one_pool_n_datasets_n_children.yaml"},
		"One pool, N datasets, N children, N snapshots":                            {def: "one_pool_n_datasets_n_children_n_snapshots.yaml"},
		"One pool, N datasets, N children, N snapshots, intermediate canmount=off": {def: "one_pool_n_datasets_n_children_n_snapshots_canmount_off.yaml"},
		"One pool, one dataset":                                                    {def: "one_pool_one_dataset.yaml"},
		"One pool, one dataset, different mountpoints":                             {def: "one_pool_one_dataset_different_mountpoints.yaml"},
		"One pool, one dataset, no property":                                       {def: "one_pool_one_dataset_no_property.yaml"},
		"One pool, one dataset, with systemdataset property":                       {def: "one_pool_one_dataset_with_systemdataset.yaml"},
		"One pool, N datasets":                                                     {def: "one_pool_n_datasets.yaml"},
		"One pool, one dataset, one snapshot":                                      {def: "one_pool_one_dataset_one_snapshot.yaml"},
		"One pool, one dataset, one snapshot, canmount=noauto":                     {def: "one_pool_one_dataset_canmount_noauto.yaml"},
		"One pool, N datasets, one snapshot":                                       {def: "one_pool_n_datasets_one_snapshot.yaml"},
		"One pool non-root mpoint, N datasets no mountpoint":                       {def: "one_pool_with_nonroot_mountpoint_n_datasets_no_mountpoint.yaml"},
		"Two pools, N datasets":                                                    {def: "two_pools_n_datasets.yaml"},
		"Two pools, N datasets, N snapshots":                                       {def: "two_pools_n_datasets_n_snapshots.yaml"},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			dir, cleanup := tempDir(t)
			defer cleanup()

			ta := timeAsserter(time.Now())
			fPools := newFakePools(t, filepath.Join("testdata", tc.def))
			defer fPools.create(dir, strings.Replace(testNameToPath(t), "/", "_", -1))()

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

			var ds []*zfs.Dataset
			for k := range got {
				if !got[k].IsSnapshot {
					continue
				}
				ds = append(ds, &got[k])
			}

			ta.assertAndReplaceCreationTimeInRange(t, ds)

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

	goldenFile := filepath.Join("testdata", testNameToPath(t)+".golden")
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

func testNameToPath(t *testing.T) string {
	t.Helper()

	testDirname := strings.Split(t.Name(), "/")[0]
	nparts := strings.Split(t.Name(), "/")
	name := strings.ToLower(nparts[len(nparts)-1])

	var elems []string
	for _, e := range []string{testDirname, name} {
		for _, k := range []string{"/", " ", ",", "="} {
			e = strings.Replace(e, k, "_", -1)
		}
		elems = append(elems, strings.ToLower(strings.Replace(e, "__", "_", -1)))
	}

	return strings.Join(elems, "/")
}

// timeAsserter ensures that dates will be between a start and end time
type timeAsserter time.Time

const currentMagicTime = 2000000000

// assertAndReplaceCreationTimeInRange ensure that last-used is between starts and endtime.
// It replaces those datasets last-used with the current fake "current time"
func (ta timeAsserter) assertAndReplaceCreationTimeInRange(t *testing.T, ds []*zfs.Dataset) {
	t.Helper()
	curr := time.Now().Unix()
	start := time.Time(ta).Unix()

	for _, r := range ds {
		if int64(r.LastUsed) < start || int64(r.LastUsed) > curr {
			t.Errorf("expected snapshot time outside of range: %d", r.LastUsed)
		} else {
			r.LastUsed = currentMagicTime
		}
	}
}
