package machines

import (
	"encoding/json"
	"io/ioutil"
	"path/filepath"
	"testing"

	"github.com/ubuntu/zsys/internal/zfs"
)

// LoadDatasets returns datasets from a def file path.
func LoadDatasets(t *testing.T, def string) (ds []zfs.Dataset) {
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
