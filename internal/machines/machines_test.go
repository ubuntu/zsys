package machines_test

import (
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
	config.SetVerboseMode(true)
}

func TestNew(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		def     string
		cmdline string
	}{
		"One machine, one dataset": {def: "d_one_machine_one_dataset.json"},
		"One disabled machine":     {def: "d_one_disabled_machine.json"},
	}

	for name, tc := range tests {
		tc := tc
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ds := machines.LoadDatasets(t, tc.def)

			got := machines.New(ds, tc.cmdline)

			assertMachinesToGolden(t, got)
		})
	}
}

// assertMachinesToGolden compares got slice of machines to reference files, based on test name.
func assertMachinesToGolden(t *testing.T, got machines.Machines) {
	want := machines.Machines{}
	testutils.LoadFromGoldenFile(t, got, &want)

	if diff := cmp.Diff(want, got, cmpopts.EquateEmpty(),
		cmp.AllowUnexported(machines.Machines{}, zfs.DatasetProp{})); diff != "" {
		t.Errorf("Machines mismatch (-want +got):\n%s", diff)
	}
}
