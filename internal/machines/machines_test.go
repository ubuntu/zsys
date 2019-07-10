package machines_test

import (
	"encoding/json"
	"io/ioutil"
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/ubuntu/zsys/internal/config"
	"github.com/ubuntu/zsys/internal/machines"
	"github.com/ubuntu/zsys/internal/testutils"
	"github.com/ubuntu/zsys/internal/zfs"
)

func init() {
	testutils.InstallUpdateFlag()
	config.SetVerboseMode(false)
}

func TestNew(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		def string
	}{
		"One machine, one dataset": {def: "d_one_machine_one_dataset.json"},
		"One disabled machine":     {def: "d_one_disabled_machine.json"},
	}

	for name, tc := range tests {
		tc := tc
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ds := loadDatasets(t, tc.def)

			got := machines.New(ds)

			assertMachinesToGolden(t, got)
		})
	}
}

// assertMachinesToGolden compares got slice of machines to reference files, based on test name.
func assertMachinesToGolden(t *testing.T, got []machines.Machine) {
	var want []machines.Machine
	testutils.LoadFromGoldenFile(t, got, &want)

	if diff := cmp.Diff(want, got, cmpopts.EquateEmpty(),
		cmp.AllowUnexported(machines.Machine{}, zfs.DatasetProp{})); diff != "" {
		t.Errorf("Machines mismatch (-want +got):\n%s", diff)
	}
}

// loadDatasets returns datasets from a def file path.
func loadDatasets(t *testing.T, def string) (ds []zfs.Dataset) {
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
