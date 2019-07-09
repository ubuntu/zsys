package machines_test

import (
	"encoding/json"
	"flag"
	"io/ioutil"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/ubuntu/zsys/internal/config"
	"github.com/ubuntu/zsys/internal/machines"
	"github.com/ubuntu/zsys/internal/zfs"
)

var (
	update = flag.Bool("update", false, "update golden files")
)

func init() {
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

func assertMachinesToGolden(t *testing.T, got []machines.Machine) {
	var want []machines.Machine
	loadFromGoldenFile(t, got, &want)

	if diff := cmp.Diff(want, got, cmpopts.EquateEmpty(), cmp.AllowUnexported(machines.Machine{}, zfs.DatasetProp{})); diff != "" {
		t.Errorf("Machines mismatch (-want +got):\n%s", diff)
	}
}

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
