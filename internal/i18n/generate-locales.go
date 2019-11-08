//+build ignore

package main

import (
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/ubuntu/zsys/internal/config"
)

const usage = `Usage of %s:

   create-po DIRECTORY LOC [LOC...]
     Create new LOC.po file(s) from an existing pot in DIRECTORY.
   update-po DIRECTORY
     Create/Update in directory pot and refresh any po existing po files.
   generate-mo PODIR DIRECTORY
     Create .mo files for any .po in POTDIR in an structured hierarchy in DIRECTORY.
`

func main() {
	if len(os.Args) < 2 {
		log.Fatalf(usage, os.Args[0])
	}

	switch os.Args[1] {
	case "create-po":
		if len(os.Args) < 4 {
			log.Fatalf(usage, os.Args[0])
		}
		if err := createPo(os.Args[2], os.Args[3:]); err != nil {
			log.Fatalf("Error when creating po files: %v", err)
		}

	case "update-po":
		if len(os.Args) != 3 {
			log.Fatalf(usage, os.Args[0])
		}
		if err := updatePo(os.Args[2]); err != nil {
			log.Fatalf("Error when updating po files: %v", err)
		}

	case "generate-mo":
		if len(os.Args) != 4 {
			log.Fatalf(usage, os.Args[0])
		}
		if err := generateMo(os.Args[2], os.Args[3]); err != nil {
			log.Fatalf("Error when generating mo files: %v", err)
		}
	default:
		log.Fatalf(usage, os.Args[0])
	}
}

// createPo creates new po files
func createPo(localeDir string, locs []string) error {
	potfile := filepath.Join(localeDir, config.TEXTDOMAIN+".pot")
	if _, err := os.Stat(potfile); err != nil {
		return fmt.Errorf("%q can't be read: %v", potfile, err)
	}

	for _, loc := range locs {
		pofile := filepath.Join(localeDir, loc+".po")
		if _, err := os.Stat(pofile); err == nil {
			log.Printf("Skipping %q as already exists. Please use update-po to refresh it or delete it first.", loc)
			continue
		}

		if out, err := exec.Command("msginit",
			"--input="+potfile, "--locale="+loc+".UTF-8", "--no-translator", "--output="+pofile).CombinedOutput(); err != nil {
			return fmt.Errorf("couldn't create %q: %v.\nCommand output: %s", pofile, err, out)
		}
	}

	return nil
}

// updatePo creates pot files and update any existing .po ones
func updatePo(localeDir string) error {
	if err := os.MkdirAll(localeDir, 0755); err != nil {
		return fmt.Errorf("couldn't create directory for %q: %v", localeDir, err)
	}

	// Create temporary pot file
	var files []string
	root := filepath.Dir(localeDir)
	err := filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return fmt.Errorf("fail to access %q: %v", p, err)
		}
		// Only deal with files
		if info.IsDir() {
			return nil
		}

		if !strings.HasSuffix(p, ".go") && !strings.HasSuffix(p, ".go.template") {
			return nil
		}
		files = append(files, strings.TrimPrefix(p, root+"/"))

		return nil
	})

	if err != nil {
		return err
	}

	potfile := filepath.Join(localeDir, config.TEXTDOMAIN+".pot")
	args := append([]string{
		"--keyword=G", "--keyword=GN", "--add-comments", "--sort-output", "--package-name=" + config.TEXTDOMAIN,
		"-D", root, "--output=" + potfile}, files...)
	if out, err := exec.Command("xgettext", args...).CombinedOutput(); err != nil {
		return fmt.Errorf("couldn't compile pot file: %v\nCommand output: %s", err, out)
	}

	// Merge existing po files
	poCandidates, err := ioutil.ReadDir(localeDir)
	if err != nil {
		log.Fatalf("couldn't list content of %q: %v", localeDir, err)
	}
	for _, f := range poCandidates {
		if !strings.HasSuffix(f.Name(), ".po") {
			continue
		}

		pofile := filepath.Join(localeDir, f.Name())
		if out, err := exec.Command("msgmerge", "--update", "--backup=none", pofile, potfile).CombinedOutput(); err != nil {
			return fmt.Errorf("couldn't refresh %q: %v.\nCommand output: %s", pofile, err, out)
		}
	}

	return nil
}

// generateMo generates a locale directory stucture with a mo for each po in localeDir.
func generateMo(in, out string) error {
	baseOut := filepath.Join(out, "locale")
	if err := os.RemoveAll(baseOut); err != nil {
		log.Fatalf("couldn't clean %q: %v", baseOut, err)
	}

	poCandidates, err := ioutil.ReadDir(in)
	if err != nil {
		log.Fatalf("couldn't list content of %q: %v", in, err)
	}
	for _, f := range poCandidates {
		if !strings.HasSuffix(f.Name(), ".po") {
			continue
		}

		candidate := filepath.Join(in, f.Name())
		outDir := filepath.Join(baseOut, strings.TrimSuffix(f.Name(), ".po"), "LC_MESSAGES")
		if err := os.MkdirAll(outDir, 0755); err != nil {
			return fmt.Errorf("couldn't create %q: %v", out, err)
		}
		if out, err := exec.Command("msgfmt",
			"--output-file="+filepath.Join(outDir, config.TEXTDOMAIN+".mo"),
			candidate).CombinedOutput(); err != nil {
			return fmt.Errorf("couldn't compile mo file from %q: %v.\nCommand output: %s", candidate, err, out)
		}
	}
	return nil
}
