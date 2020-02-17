package machines

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/ubuntu/zsys/internal/config"
	"github.com/ubuntu/zsys/internal/testutils"
	"github.com/ubuntu/zsys/internal/zfs"
	"github.com/ubuntu/zsys/internal/zfs/mock"
)

func init() {
	config.SetVerboseMode(1)
}

func TestResolveOrigin(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		def              string
		onlyOnMountpoint string
	}{
		"one dataset":                                 {def: "d_one_machine_one_dataset.yaml"},
		"one machine with one snapshot":               {def: "d_one_machine_with_one_snapshot.yaml"},
		"one machine with one snapshot and one clone": {def: "d_one_machine_with_clone_dataset.yaml"},
		"one machine with multiple snapshots and clones, with chain of dependency":           {def: "d_one_machine_with_multiple_clones_recursive.yaml"},
		"one machine with multiple snapshots and clones, with chain of unordered dependency": {def: "d_one_machine_with_multiple_clones_recursive_unordered.yaml"},
		"one machine with children": {def: "d_one_machine_with_children.yaml"},
		"two machines":              {def: "d_two_machines_one_dataset.yaml"},

		// More real systems
		"Real machine, no snapshot, no clone":       {def: "m_layout1_one_machine.yaml"},
		"Real machines with snapshots and clones":   {def: "m_layout1_machines_with_snapshots_clones.yaml"},
		"Server machine, no snapshot, no clone":     {def: "m_layout2_one_machine.yaml"},
		"Server machines with snapshots and clones": {def: "m_layout2_machines_with_snapshots_clones.yaml"},

		"Select master only":                                {def: "d_one_machine_with_multiple_clones_recursive_with_chilren.yaml", onlyOnMountpoint: "/"},
		"Select a particular mountpoint":                    {def: "d_one_machine_with_multiple_clones_recursive_with_chilren.yaml", onlyOnMountpoint: "/child"},
		"Select no matching dataset mountpoints":            {def: "d_one_machine_with_multiple_clones_recursive_with_chilren.yaml", onlyOnMountpoint: "/none"},
		"Select all datasets without selecting mountpoints": {def: "d_one_machine_with_multiple_clones_recursive_with_chilren.yaml", onlyOnMountpoint: "-"},

		// Failing cases
		// NOTE: This case cannot happen and cannot be represented in the yaml test data
		//"missing clone referenced by a snapshot clone (broken ZFS)": {def: "d_one_machine_missing_clone.yaml"},
		"no dataset":                 {def: "d_no_dataset.yaml"},
		"no interesting mountpoints": {def: "d_no_machine.yaml"},
	}

	for name, tc := range tests {
		tc := tc
		t.Run(name, func(t *testing.T) {
			dir, cleanup := testutils.TempDir(t)
			defer cleanup()

			libzfs := getLibZFS(t)
			fPools := testutils.NewFakePools(t, filepath.Join("testdata", tc.def), testutils.WithLibZFS(libzfs))
			defer fPools.Create(dir)()

			z, err := zfs.New(context.Background(), zfs.WithLibZFS(libzfs))
			if err != nil {
				t.Fatalf("couldn’t create original zfs datasets state")
			}

			var ds []*zfs.Dataset
			for _, d := range z.Datasets() {
				d := d
				ds = append(ds, &d)
			}
			if tc.onlyOnMountpoint == "" {
				tc.onlyOnMountpoint = "/"
			}
			if tc.onlyOnMountpoint == "-" {
				tc.onlyOnMountpoint = ""
			}

			got := resolveOrigin(context.Background(), ds, tc.onlyOnMountpoint)

			assertDatasetsOrigin(t, got)
		})
	}
}

type testhelper interface {
	Helper()
}

// TODO: for now, we can only run with mock zfs system
func getLibZFS(t testhelper) testutils.LibZFSInterface {
	t.Helper()

	fmt.Println("Running tests with mocked libzfs")
	mock := mock.New()
	return &mock
}

// assertDatasetsOrigin compares got maps of origin to reference files, based on test name.
func assertDatasetsOrigin(t *testing.T, got map[string]*string) {
	want := make(map[string]*string)
	testutils.LoadFromGoldenFile(t, got, &want)

	if diff := cmp.Diff(want, got, cmpopts.EquateEmpty()); diff != "" {
		t.Errorf("Dataset origin mismatch (-want +got):\n%s", diff)
	}
}
