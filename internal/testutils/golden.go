package testutils

import (
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

var update *bool
var updateFlagOnce = sync.Once{}

// InstallUpdateFlag adds the update golden files flag
func InstallUpdateFlag() {
	updateFlagOnce.Do(func() { update = flag.Bool("update", false, "update golden files") })
}

// LoadFromGoldenFile loads expected content to "want", after optionally refreshing it
// from "got" if udpate flag is passed.
func LoadFromGoldenFile(t *testing.T, got interface{}, want interface{}) {
	t.Helper()

	goldenFile := filepath.Join("testdata", testNameToPath(t)+".golden")
	if update != nil && *update {
		b, err := json.MarshalIndent(got, "", "   ")
		if err != nil {
			t.Fatal("couldn't convert to json:", err)
		}
		if err := ioutil.WriteFile(goldenFile, b, 0644); err != nil {
			t.Fatal("couldn't save golden file:", err)
		}
		if p, err := filepath.Abs(goldenFile); err == nil {
			fmt.Println("Updated", p)
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

// testNameToPath transform the test path official name to a [subdirectory/]*test_base_name
// for golden files generations
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
