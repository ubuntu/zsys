//+build ignore

package main

import (
	"bufio"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/ubuntu/zsys/internal/config"
	"github.com/ubuntu/zsys/internal/generators"
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
		if generators.InstallOnlyMode() {
			return
		}
		if err := createPo(os.Args[2], os.Args[3:]); err != nil {
			log.Fatalf("Error when creating po files: %v", err)
		}

	case "update-po":
		if len(os.Args) != 3 {
			log.Fatalf(usage, os.Args[0])
		}
		if generators.InstallOnlyMode() {
			return
		}
		if err := updatePo(os.Args[2]); err != nil {
			log.Fatalf("Error when updating po files: %v", err)
		}

	case "generate-mo":
		if len(os.Args) != 4 {
			log.Fatalf(usage, os.Args[0])
		}
		if err := generateMo(os.Args[2], generators.DestDirectory(os.Args[3])); err != nil {
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

	// Create pot file
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
	var potcreation string
	// if already existed: extract POT creation date to keep it (xgettext always updates it)
	if _, err := os.Stat(potfile); err == nil {
		if potcreation, err = getPOTCreationDate(potfile); err != nil {
			log.Fatal(err)
		}
	}
	args := append([]string{
		"--keyword=G", "--keyword=GN", "--add-comments", "--sort-output", "--package-name=" + config.TEXTDOMAIN,
		"-D", root, "--output=" + potfile}, files...)
	if out, err := exec.Command("xgettext", args...).CombinedOutput(); err != nil {
		return fmt.Errorf("couldn't compile pot file: %v\nCommand output: %s", err, out)
	}
	if potcreation != "" {
		if err := rewritePOTCreationDate(potcreation, potfile); err != nil {
			log.Fatalf("couldn't change POT Creation file: %v", err)
		}
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

		// extract POT creation date to keep it (msgmerge always updates it)
		potcreation, err := getPOTCreationDate(pofile)
		if err != nil {
			log.Fatal(err)
		}

		if out, err := exec.Command("msgmerge", "--update", "--backup=none", pofile, potfile).CombinedOutput(); err != nil {
			return fmt.Errorf("couldn't refresh %q: %v.\nCommand output: %s", pofile, err, out)
		}

		if err := rewritePOTCreationDate(potcreation, pofile); err != nil {
			log.Fatalf("couldn't change POT Creation file: %v", err)
		}
	}

	return nil
}

// generateMo generates a locale directory stucture with a mo for each po in localeDir.
func generateMo(in, out string) error {
	baseLocaleDir := filepath.Join(out, "locale")
	if err := generators.CleanDirectory(baseLocaleDir); err != nil {
		log.Fatalln(err)
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
		outDir := filepath.Join(baseLocaleDir, strings.TrimSuffix(f.Name(), ".po"), "LC_MESSAGES")
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

const potCreationDatePrefix = `"POT-Creation-Date:`

func getPOTCreationDate(p string) (string, error) {
	f, err := os.Open(p)
	if err != nil {
		return "", fmt.Errorf("couldn't open %q: %v", p, err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		if strings.HasPrefix(scanner.Text(), potCreationDatePrefix) {
			return scanner.Text(), nil
		}
	}

	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("error while reading %q: %v", p, err)
	}

	return "", fmt.Errorf("didn't find %q in %q", potCreationDatePrefix, p)
}

func rewritePOTCreationDate(potcreation, p string) error {
	f, err := os.Open(p)
	if err != nil {
		return fmt.Errorf("couldn't open %q: %v", p, err)
	}
	defer f.Close()
	out, err := os.Create(p + ".new")
	if err != nil {
		return fmt.Errorf("couldn't open %q: %v", p+".new", err)
	}
	defer out.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		t := scanner.Text()
		if strings.HasPrefix(t, potCreationDatePrefix) {
			t = potcreation
		}
		if _, err := out.WriteString(t + "\n"); err != nil {
			return fmt.Errorf("couldn't write to %q: %v", p+".new", err)
		}
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("error while reading %q: %v", p, err)
	}
	f.Close()
	out.Close()

	if err := os.Rename(p+".new", p); err != nil {
		return fmt.Errorf("couldn't rename %q to %q: %v", p+".new", p, err)
	}
	return nil
}
