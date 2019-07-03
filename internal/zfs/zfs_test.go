package zfs_test

import (
	"encoding/json"
	"flag"
	"io/ioutil"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
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
			defer fPools.create(dir)()

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

			assertDatasetsToGolden(t, ta, got, false)
		})
	}
}

func TestSnapshot(t *testing.T) {
	tests := map[string]struct {
		def          string
		snapshotName string
		datasetName  string
		recursive    bool

		wantErr bool
	}{
		"Simple snapshot":                                      {def: "one_pool_one_dataset.yaml", snapshotName: "snap1", datasetName: "rpool"},
		"Recursive snapshots":                                  {def: "layout1__one_pool_n_datasets.yaml", snapshotName: "snap1", datasetName: "rpool/ROOT/ubuntu_1234", recursive: true},
		"Simple snapshot with children":                        {def: "layout1__one_pool_n_datasets.yaml", snapshotName: "snap1", datasetName: "rpool/ROOT/ubuntu_1234"},
		"Dataset doesn't exist":                                {def: "one_pool_one_dataset.yaml", snapshotName: "snap1", datasetName: "doesntexit", wantErr: true},
		"Invalid snapshot name":                                {def: "one_pool_one_dataset.yaml", snapshotName: "", datasetName: "rpool", wantErr: true},
		"Recursive snapshot on leaf dataset":                   {def: "one_pool_one_dataset.yaml", snapshotName: "snap1", datasetName: "rpool", recursive: true},
		"Recursive snapshots alongside existing ones":          {def: "layout1__one_pool_n_datasets_n_snapshots.yaml", snapshotName: "snap1", datasetName: "rpool/ROOT/ubuntu_1234", recursive: true},
		"Snapshot on dataset already exists":                   {def: "layout1__one_pool_n_datasets_n_snapshots.yaml", snapshotName: "snap_r1", datasetName: "rpool/ROOT/ubuntu_1234/opt", wantErr: true},
		"Snapshot on subdataset already exists":                {def: "layout1__one_pool_n_datasets_n_snapshots.yaml", snapshotName: "snap_r1", datasetName: "rpool/ROOT", recursive: true, wantErr: true},
		"Simple snapshot even if on subdataset already exists": {def: "layout1__one_pool_n_datasets_n_snapshots.yaml", snapshotName: "snap_r1", datasetName: "rpool/ROOT"},
		"Snapshot on dataset exists, but not on subdataset":    {def: "layout1_missing_intermediate_snapshot.yaml", snapshotName: "snap_r1", datasetName: "rpool/ROOT/ubuntu_1234", wantErr: true},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			dir, cleanup := tempDir(t)
			defer cleanup()

			ta := timeAsserter(time.Now())
			fPools := newFakePools(t, filepath.Join("testdata", tc.def))
			defer fPools.create(dir)()
			z := zfs.New()
			err := z.Snapshot(tc.snapshotName, tc.datasetName, tc.recursive)
			if err != nil {
				if !tc.wantErr {
					t.Fatalf("expected no error but got: %v", err)
				}
				// we don't return because we want to check that on error, Snapshot() is a no-op
			}
			if err == nil && tc.wantErr {
				t.Fatal("expected an error but got none")
			}

			got, err := z.Scan()
			if err != nil {
				t.Fatalf("expected no error on scan but got: %v", err)
			}

			assertDatasetsToGolden(t, ta, got, true)
		})
	}
}

func TestClone(t *testing.T) {
	tests := map[string]struct {
		def       string
		dataset   string
		suffix    string
		recursive bool

		wantErr bool
	}{
		// TODO: Test case with user properties changed between snapshot and parent (with children inheriting)
		"Simple clone":    {def: "layout1__one_pool_n_datasets_n_snapshots.yaml", dataset: "rpool/ROOT/ubuntu_1234@snap_r1", suffix: "5678"},
		"Recursive clone": {def: "layout1__one_pool_n_datasets_n_snapshots.yaml", dataset: "rpool/ROOT/ubuntu_1234@snap_r1", suffix: "5678", recursive: true},

		"Simple clone on dataset without suffix":    {def: "layout1__one_pool_n_datasets_n_snapshots_without_suffix.yaml", dataset: "rpool/ROOT/ubuntu@snap_r1", suffix: "5678"},
		"Recursive clone on dataset without suffix": {def: "layout1__one_pool_n_datasets_n_snapshots_without_suffix.yaml", dataset: "rpool/ROOT/ubuntu@snap_r1", suffix: "5678", recursive: true},

		"Recursive missing some leaf snapshots":    {def: "layout1_missing_leaf_snapshot.yaml", dataset: "rpool/ROOT/ubuntu_1234@snap_r1", suffix: "5678", recursive: true},
		"Recursive missing intermediate snapshots": {def: "layout1_missing_intermediate_snapshot.yaml", dataset: "rpool/ROOT/ubuntu_1234@snap_r1", suffix: "5678", recursive: true, wantErr: true},

		"Snapshot doesn't exists":         {def: "layout1__one_pool_n_datasets_n_snapshots.yaml", dataset: "rpool/ROOT/ubuntu_1234@doesntexists", suffix: "5678", wantErr: true},
		"Dataset doesn't exists":          {def: "layout1__one_pool_n_datasets_n_snapshots.yaml", dataset: "rpool/ROOT/ubuntu_doesntexist@something", suffix: "5678", wantErr: true},
		"No suffix provided":              {def: "layout1__one_pool_n_datasets_n_snapshots.yaml", dataset: "rpool/ROOT/ubuntu_1234@snap_r1", wantErr: true},
		"Suffixed dataset already exists": {def: "layout1__one_pool_n_datasets_n_snapshots.yaml", dataset: "rpool/ROOT/ubuntu_1234@snap_r1", suffix: "1234", wantErr: true},
		"Clone on root fails":             {def: "one_pool_one_dataset_one_snapshot.yaml", dataset: "rpool@snap1", suffix: "5678", wantErr: true},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			dir, cleanup := tempDir(t)
			defer cleanup()

			ta := timeAsserter(time.Now())
			fPools := newFakePools(t, filepath.Join("testdata", tc.def))
			defer fPools.create(dir)()
			z := zfs.New()
			err := z.Clone(tc.dataset, tc.suffix, tc.recursive)
			if err != nil {
				if !tc.wantErr {
					t.Fatalf("expected no error but got: %v", err)
				}
				// we don't return because we want to check that on error, Clone() is a no-op
			}
			if err == nil && tc.wantErr {
				t.Fatal("expected an error but got none")
			}

			got, err := z.Scan()
			if err != nil {
				t.Fatalf("expected no error on scan but got: %v", err)
			}

			assertDatasetsToGolden(t, ta, got, true)
		})
	}
}

func TestSetProperty(t *testing.T) {
	tests := map[string]struct {
		def           string
		propertyName  string
		propertyValue string
		dataset       string
		force         bool

		wantErr bool
	}{
		"User property (local)":       {def: "one_pool_one_dataset.yaml", propertyName: zfs.BootfsProp, propertyValue: "SetProperty Value", dataset: "rpool"},
		"Authorized property (local)": {def: "one_pool_one_dataset.yaml", propertyName: zfs.CanmountProp, propertyValue: "noauto", dataset: "rpool"},
		"User property (none)":        {def: "one_pool_one_dataset.yaml", propertyName: zfs.SystemDataProp, propertyValue: "SetProperty Value", dataset: "rpool"},
		// There is no authorized properties that can be "none" for now

		"User property on snapshot (parent local)": {def: "one_pool_one_dataset_one_snapshot.yaml", propertyName: zfs.BootfsProp, propertyValue: "SetProperty Value", dataset: "rpool@snap1"},
		"User property on snapshot (parent none)":  {def: "one_pool_one_dataset_one_snapshot.yaml", propertyName: zfs.SystemDataProp, propertyValue: "SetProperty Value", dataset: "rpool@snap1"},

		"User property (inherit)":                               {def: "one_pool_n_datasets_n_children.yaml", propertyName: zfs.BootfsProp, propertyValue: "SetProperty Value", dataset: "rpool/ROOT/ubuntu/var"},
		"User property on snapshot (parent inherit)":            {def: "one_pool_n_datasets_n_children_n_snapshots.yaml", propertyName: zfs.BootfsProp, propertyValue: "SetProperty Value", dataset: "rpool/ROOT/ubuntu/var@snap_v1"},
		"User property (inherit but forced)":                    {def: "one_pool_n_datasets_n_children.yaml", propertyName: zfs.BootfsProp, propertyValue: "SetProperty Value", dataset: "rpool/ROOT/ubuntu/var", force: true},
		"User property on snapshot (parent inherit but forced)": {def: "one_pool_n_datasets_n_children_n_snapshots.yaml", propertyName: zfs.BootfsProp, propertyValue: "SetProperty Value", dataset: "rpool/ROOT/ubuntu/var@snap_v1", force: true},
		// There is no authorized properties that can be inherited

		"Unauthorized property":                          {def: "one_pool_one_dataset.yaml", propertyName: "mountpoint", propertyValue: "/setproperty/value", dataset: "rpool", wantErr: true},
		"Dataset doesn't exists":                         {def: "one_pool_one_dataset.yaml", propertyName: zfs.SystemDataProp, propertyValue: "SetProperty Value", dataset: "rpool10", wantErr: true},
		"Authorized property on snapshot doesn't exists": {def: "one_pool_one_dataset_one_snapshot.yaml", propertyName: zfs.CanmountProp, propertyValue: "yes", dataset: "rpool@snap1", wantErr: true},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			dir, cleanup := tempDir(t)
			defer cleanup()

			ta := timeAsserter(time.Now())
			fPools := newFakePools(t, filepath.Join("testdata", tc.def))
			defer fPools.create(dir)()
			z := zfs.New()
			err := z.SetProperty(tc.propertyName, tc.propertyValue, tc.dataset, tc.force)
			if err != nil {
				if !tc.wantErr {
					t.Fatalf("expected no error but got: %v", err)
				}
				// we don't return because we want to check that on error, SetProperty() is a no-op
			}
			if err == nil && tc.wantErr {
				t.Fatal("expected an error but got none")
			}

			got, err := z.Scan()
			if err != nil {
				t.Fatalf("expected no error on scan but got: %v", err)
			}

			assertDatasetsToGolden(t, ta, got, true)
		})
	}
}

// transformToReproducibleDatasetSlice applied transformation to ensure that the comparison is reproducible via
// DataSlices.
func transformToReproducibleDatasetSlice(t *testing.T, ta timeAsserter, got []zfs.Dataset, includePrivate bool) zfs.DatasetSlice {
	t.Helper()

	// Ensure datasets were created at expected range time and replace them with magic time.
	var ds []*zfs.Dataset
	for k := range got {
		if !got[k].IsSnapshot {
			continue
		}
		ds = append(ds, &got[k])
	}
	ta.assertAndReplaceCreationTimeInRange(t, ds)

	// Sort the golden file order to be reproducible.
	gotForGolden := zfs.DatasetSlice{DS: got, IncludePrivate: includePrivate}
	sort.Sort(gotForGolden)
	return gotForGolden
}

// datasetsEquals prints a diff if datasets aren't equals and fails the test
func datasetsEquals(t *testing.T, want, got []zfs.Dataset, includePrivate bool) {
	t.Helper()

	// Actual diff assertion.
	privateOpt := cmpopts.IgnoreUnexported(zfs.DatasetProp{})
	if includePrivate {
		privateOpt = cmp.AllowUnexported(zfs.DatasetProp{})
	}
	if diff := cmp.Diff(want, got, privateOpt); diff != "" {
		t.Errorf("Scan() mismatch (-want +got):\n%s", diff)
	}
}

// assertDatasetsToGolden compares (and update if needed) a slice of dataset got from a Scan() for instance
// to a golden file.
// We can optionnally include private fields in the comparison and saving.
func assertDatasetsToGolden(t *testing.T, ta timeAsserter, got []zfs.Dataset, includePrivate bool) {
	t.Helper()

	gotForGolden := transformToReproducibleDatasetSlice(t, ta, got, includePrivate)
	got = gotForGolden.DS

	// Get expected dataset list from golden file, update as needed.
	wantFromGolden := zfs.DatasetSlice{IncludePrivate: includePrivate}
	loadFromGoldenFile(t, gotForGolden, &wantFromGolden)
	want := []zfs.Dataset(wantFromGolden.DS)

	datasetsEquals(t, want, got, includePrivate)
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
}

// loadFromGoldenFile loads expected content to "want", after optionally refreshing it
// from "got" if udpate flag is passed.
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
		for _, k := range []string{"/", " ", ",", "=", "'"} {
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
