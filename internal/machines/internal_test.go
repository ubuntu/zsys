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
		"one dataset":                                 {"d_one_machine_one_dataset.json"},
		"one machine with one snapshot":               {"d_one_machine_with_one_snapshot.json"},
		"one machine with one snapshot and one clone": {"d_one_machine_with_clone_dataset.json"},
		"one machine with multiple snapshots and clones, with chain of dependency":           {"d_one_machine_with_multiple_clones_recursive.json"},
		"one machine with multiple snapshots and clones, with chain of unordered dependency": {"d_one_machine_with_multiple_clones_recursive_unordered.json"},
		"one machine with children": {"d_one_machine_with_children.json"},
		"two machines":              {"d_two_machines_one_dataset.json"},

		// More real systems
		"Real machine, no snapshot, no clone":       {"m_layout1_one_machine.json"},
		"Real machines with snapshots and clones":   {"m_layout1_machines_with_snapshots_clones.json"},
		"Server machine, no snapshot, no clone":     {"m_layout2_one_machine.json"},
		"Server machines with snapshots and clones": {"m_layout2_machines_with_snapshots_clones.json"},

		// Failing cases
		"missing clone referenced by a snapshot clone (broken ZFS)": {"d_one_machine_missing_clone.json"},
		"no dataset":                 {"d_no_dataset.json"},
		"no interesting mountpoints": {"d_no_machine.json"},
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
	mock := zfs.NewLibZFSMock()
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
