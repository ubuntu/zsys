package main

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/spf13/cobra"
	"github.com/ubuntu/zsys/internal/config"
	"github.com/ubuntu/zsys/internal/zfs"
	"golang.org/x/xerrors"
)

var (
	printModifiedBoot bool

	bootCmd = &cobra.Command{
		Use:    "boot COMMAND",
		Short:  "Ensure that the right datasets are ready to be mounted and committed during early boot",
		Hidden: true,
		RunE:   requireSubcommand,
	}
	bootPrepareCmd = &cobra.Command{
		Use:   "prepare",
		Short: "Prepare boot by ensuring correct system and user datasets are switched on and off",
		Args:  cobra.NoArgs,
		Run:   func(cmd *cobra.Command, args []string) { cmdErr = bootPrepare(printModifiedBoot) },
	}
	bootCommitCmd = &cobra.Command{
		Use:   "commit",
		Short: "Commit system and user datasets states as a successful boot",
		Args:  cobra.NoArgs,
		Run:   func(cmd *cobra.Command, args []string) { cmdErr = bootCommit(printModifiedBoot) },
	}
)

const (
	updateGrubCmd  = "update-grub"
	modifiedBoot   = "zsys-meta:modified-boot"
	noModifiedBoot = "zsys-meta:no-modified-boot"
)

func init() {
	bootCmd.PersistentFlags().BoolVarP(&printModifiedBoot, "print-changes", "p", false, "Display if any zfs datasets have been modified to boot")
	rootCmd.AddCommand(bootCmd)
	bootCmd.AddCommand(bootPrepareCmd)
	bootCmd.AddCommand(bootCommitCmd)
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

	ms, err := getMachines(z)
	if err != nil {
		return err
	}

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

	ms, err := getMachines(z)
	if err != nil {
		return err
	}

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
