package machines

import (
	"context"
	"encoding/json"
	"io/ioutil"
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/ubuntu/zsys/internal/config"
	"github.com/ubuntu/zsys/internal/testutils"
	"github.com/ubuntu/zsys/internal/zfs"
)

func init() {
	testutils.InstallUpdateFlag()
	config.SetVerboseMode(1)
}

func TestResolveOrigin(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		def string
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
			t.Parallel()
			ds := LoadDatasets(t, tc.def)

			got := resolveOrigin(context.Background(), ds)

			assertDatasetsOrigin(t, got)
		})
	}
}

type FatalHelper interface {
	Fatalf(format string, args ...interface{})
	Helper()
}

// LoadDatasets returns datasets from a def file path.
func LoadDatasets(t FatalHelper, def string) (ds []zfs.Dataset) {
	t.Helper()

	p := filepath.Join("testdata", def)
	b, err := ioutil.ReadFile(p)
	if err != nil {
		t.Fatalf("couldn't read definition file %q: %v", def, err)
	}

	if err := json.Unmarshal(b, &ds); err != nil {
		t.Fatalf("couldn't convert definition file %q to slice of dataset: %v", def, err)
	}
	return ds
}

// assertDatasetsOrigin compares got maps of origin to reference files, based on test name.
func assertDatasetsOrigin(t *testing.T, got map[string]*string) {
	want := make(map[string]*string)
	testutils.LoadFromGoldenFile(t, got, &want)

	if diff := cmp.Diff(want, got, cmpopts.EquateEmpty()); diff != "" {
		t.Errorf("Dataset origin mismatch (-want +got):\n%s", diff)
	}
}
