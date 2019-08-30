package main

import (
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"

	"github.com/spf13/cobra"
	"github.com/ubuntu/zsys/internal/config"
	"github.com/ubuntu/zsys/internal/machines"
	"github.com/ubuntu/zsys/internal/zfs"
	"golang.org/x/xerrors"
)

var (
	printModifiedBoot bool

	bootCmd = &cobra.Command{
		Use:       "boot prepare|commit",
		Short:     "Ensure that the right datasets are ready to be mounted and committed during early boot",
		Hidden:    true,
		Args:      cobra.ExactValidArgs(1),
		ValidArgs: []string{"prepare", "commit"},
		Run: func(cmd *cobra.Command, args []string) {
			switch args[0] {
			case "prepare":
				cmdErr = bootPrepare(printModifiedBoot)
			case "commit":
				cmdErr = bootCommit(printModifiedBoot)
			}
		},
	}
)

func init() {
	bootCmd.Flags().BoolVarP(&printModifiedBoot, "print-changes", "p", false, "Display if any zfs datasets have been modified to boot")
	rootCmd.AddCommand(bootCmd)
}

func bootPrepare(printModifiedBoot bool) (err error) {
	z := zfs.New(zfs.WithTransactions())

	defer func() {
		if err != nil {
			z.Cancel()
			err = xerrors.Errorf("couldn't ensure boot: "+config.ErrorFormat, err)
		} else {
			z.Done()
		}
	}()

	ds, err := z.Scan()
	if err != nil {
		return err
	}
	cmdline, err := procCmdline()
	if err != nil {
		return err
	}
	ms := machines.New(ds, cmdline)

	changed, err := ms.EnsureBoot(z)
	if err != nil {
		return err
	}
	if printModifiedBoot && changed {
		fmt.Println(modifiedBoot)
	} else if printModifiedBoot && !changed {
		fmt.Println(noModifiedBoot)
	}

	return nil
}

func bootCommit(printModifiedBoot bool) (err error) {
	z := zfs.New(zfs.WithTransactions())

	defer func() {
		if err != nil {
			z.Cancel()
			err = xerrors.Errorf("couldn't commit: "+config.ErrorFormat, err)
		} else {
			z.Done()
		}
	}()

	ds, err := z.Scan()
	if err != nil {
		return err
	}
	cmdline, err := procCmdline()
	if err != nil {
		return err
	}
	ms := machines.New(ds, cmdline)

	changed, err := ms.Commit(z)
	if err != nil {
		return err
	}
	if printModifiedBoot && changed {
		fmt.Println(modifiedBoot)
	} else if printModifiedBoot && !changed {
		fmt.Println(noModifiedBoot)
	}

	if changed {
		cmd := exec.Command(updateGrubCmd)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return xerrors.Errorf("%q returns an error:"+config.ErrorFormat, updateGrubCmd, err)
		}
	}

	return nil
}

func procCmdline() (string, error) {
	content, err := ioutil.ReadFile("/proc/cmdline")
	if err != nil {
		return "", err
	}

	return string(content), nil
}
