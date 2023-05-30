//go:build tools
// +build tools

package main

import (
	"bufio"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/cobra/doc"
	"github.com/ubuntu/zsys/cmd/zsysd/client"
	"github.com/ubuntu/zsys/cmd/zsysd/daemon"
	"github.com/ubuntu/zsys/internal/generators"
)

const usage = `Usage of %s:

   completion DIRECTORY
     Create completions files in a structured hierarchy in DIRECTORY.
   man DIRECTORY
     Create man pages files in a structured hierarchy in DIRECTORY.
   update-readme
     Update repository README with commands.
`

func main() {
	if len(os.Args) < 2 {
		log.Fatalf(usage, os.Args[0])
	}

	installCompletionCmd(client.Cmd())
	installCompletionCmd(daemon.Cmd())

	commands := []*cobra.Command{client.Cmd(), daemon.Cmd()}
	switch os.Args[1] {
	case "completion":
		if len(os.Args) < 3 {
			log.Fatalf(usage, os.Args[0])
		}
		dir := generators.DestDirectory(os.Args[2])
		genBashCompletions(commands, dir)
	case "man":
		if len(os.Args) < 3 {
			log.Fatalf(usage, os.Args[0])
		}
		dir := generators.DestDirectory(os.Args[2])
		genManPages(commands, dir)
	case "update-readme":
		if generators.InstallOnlyMode() {
			return
		}
		updateREADME(commands)
	default:
		log.Fatalf(usage, os.Args[0])
	}
}

func genBashCompletions(cmds []*cobra.Command, dir string) {
	bashCompDir := filepath.Join(dir, "bash-completion")
	if err := generators.CleanDirectory(bashCompDir); err != nil {
		log.Fatalln(err)
	}

	out := filepath.Join(bashCompDir, "completions")
	if err := os.MkdirAll(out, 0755); err != nil {
		log.Fatalf("Couldn't create bash completion directory: %v", err)
	}

	for _, cmd := range cmds {
		if err := genBashCompletionFile(cmd, filepath.Join(out, cmd.Name())); err != nil {
			log.Fatalf("Couldn't create bash completion for %s: %v", cmd.Name(), err)
		}
	}
}

func genManPages(cmds []*cobra.Command, dir string) {
	manBaseDir := filepath.Join(dir, "man")
	if err := generators.CleanDirectory(manBaseDir); err != nil {
		log.Fatalln(err)
	}

	out := filepath.Join(manBaseDir, "man1")
	if err := os.MkdirAll(out, 0755); err != nil {
		log.Fatalf("Couldn't create man pages directory: %v", err)
	}

	for _, cmd := range cmds {
		if err := genManTreeFromOpts(cmd, doc.GenManHeader{
			Title: fmt.Sprintf("ZSYS: %s", cmd.Name()),
		}, out); err != nil {
			log.Fatalf("Couldn't generate man pages for %s: %v", cmd.Name(), err)
		}
	}
}

func updateREADME(cmds []*cobra.Command) {
	_, current, _, ok := runtime.Caller(1)
	if !ok {
		log.Fatal("Couldn't find current file name")
	}

	readme := filepath.Join(filepath.Dir(current), "..", "..", "README.md")

	in, err := os.Open(readme)
	if err != nil {
		log.Fatalf("Couldn't open source readme file: %v", err)
	}
	defer in.Close()

	tmp, err := os.Create(readme + ".new")
	if err != nil {
		log.Fatalf("Couldn't create temporary readme file: %v", err)
	}
	defer tmp.Close()

	src := bufio.NewScanner(in)
	// Write header
	for src.Scan() {
		mustWriteLine(tmp, src.Text())
		if src.Text() == "## Usage" {
			mustWriteLine(tmp, "")
			break
		}
	}
	if err := src.Err(); err != nil {
		log.Fatalf("Error when scanning source readme file: %v", err)
	}

	// Write markdown
	user, system := getCmdsAndSystems(cmds)
	mustWriteLine(tmp, "### User commands\n")
	filterCommandMarkdown(user, tmp)
	mustWriteLine(tmp, "### System commands\n")
	mustWriteLine(tmp, "Those commands are hidden from help and should primarily be used by the system itself.\n")
	filterCommandMarkdown(system, tmp)

	// Write footer (skip previously generated content)
	skip := true
	for src.Scan() {
		if strings.HasPrefix(src.Text(), "## ") {
			skip = false
		}
		if skip {
			continue
		}

		mustWriteLine(tmp, src.Text())
	}
	if err := src.Err(); err != nil {
		log.Fatalf("Error when scanning source readme file: %v", err)
	}

	if err := in.Close(); err != nil {
		log.Fatalf("Couldn't close source Rreadme file: %v", err)
	}
	if err := tmp.Close(); err != nil {
		log.Fatalf("Couldn't close temporary readme file: %v", err)
	}
	if err := os.Rename(readme+".new", readme); err != nil {
		log.Fatalf("Couldn't rename to destination readme file: %v", err)
	}
}

func mustWriteLine(w io.Writer, msg string) {
	if _, err := w.Write([]byte(msg + "\n")); err != nil {
		log.Fatal("Couldn't write %s: %v", msg, err)
	}
}

// genManTreeFromOpts generates a man page for the command and all descendants.
// The pages are written to the opts.Path directory.
// This is a copy from cobra, but it will include Hidden commands.
func genManTreeFromOpts(cmd *cobra.Command, header doc.GenManHeader, dir string) error {
	for _, c := range cmd.Commands() {
		if (!c.IsAvailableCommand() && !c.Hidden) || c.IsAdditionalHelpTopicCommand() {
			continue
		}
		if err := genManTreeFromOpts(c, header, dir); err != nil {
			return err
		}
	}

	section := "1"
	basename := strings.ReplaceAll(cmd.CommandPath(), " ", "-")
	filename := filepath.Join(dir, basename+"."+section)
	f, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer f.Close()

	return doc.GenMan(cmd, &header, f)
}

func getCmdsAndSystems(cmds []*cobra.Command) (user []*cobra.Command, system []*cobra.Command) {
	for _, cmd := range cmds {
		user = append(user, cmd)
		user = append(user, collectSubCmds(cmd, false /* selectHidden */, false /* parentWasHidden */)...)
	}
	for _, cmd := range cmds {
		system = append(system, collectSubCmds(cmd, true /* selectHidden */, false /* parentWasHidden */)...)
	}

	return user, system
}

// collectSubCmds get recursiverly commands from a root one.
// It will filter hidden commands if selected, but will present children if needed.
func collectSubCmds(cmd *cobra.Command, selectHidden, parentWasHidden bool) (cmds []*cobra.Command) {
	for _, c := range cmd.Commands() {
		if c.Name() == "help" {
			continue
		}
		// Only continue selecting non hidden child of hidden commands
		if (c.Hidden && !selectHidden) || (!c.Hidden && selectHidden && !parentWasHidden) {
			continue
		}
		// Flip that we have a hidden parent
		currentOrParentHidden := parentWasHidden
		if c.Hidden {
			currentOrParentHidden = true
		}
		cmds = append(cmds, c)
		cmds = append(cmds, collectSubCmds(c, selectHidden, currentOrParentHidden)...)
	}
	return cmds
}

// filterCommandMarkdown filters SEE ALSO and add subindentation for commands
// before writing to the writer.
func filterCommandMarkdown(cmds []*cobra.Command, w io.Writer) {
	pr, pw := io.Pipe()

	go func() {
		for _, cmd := range cmds {
			if err := doc.GenMarkdown(cmd, pw); err != nil {
				pw.CloseWithError(fmt.Errorf("Couldn't generate markdown for %s: %v", cmd.Name(), err))
				return
			}
		}
		pw.Close()
	}()
	scanner := bufio.NewScanner(pr)
	var skip bool
	for scanner.Scan() {
		l := scanner.Text()
		if strings.HasPrefix(l, "### SEE ALSO") || strings.Contains(l, "Auto generated by") {
			skip = true
		}
		if strings.HasPrefix(l, "## ") {
			skip = false
		}
		if skip {
			continue
		}
		// Add 2 levels of subindentation
		if strings.HasPrefix(l, "##") {
			l = "##" + l
		}
		mustWriteLine(w, l)
	}
	if err := scanner.Err(); err != nil {
		log.Fatalf("Couldn't write generated markdown: %v", err)
	}
}
