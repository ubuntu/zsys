package client

import (
	"context"
	"io/ioutil"

	"github.com/spf13/cobra"
	"github.com/ubuntu/zsys/internal/machines"
	"github.com/ubuntu/zsys/internal/zfs"
	"golang.org/x/xerrors"
)

// requireSubcommand is a no-op command which return an error message to trigger
// a command usage error.
func requireSubcommand(cmd *cobra.Command, args []string) error {
	return xerrors.Errorf("%s requires a subcommand", cmd.Name())
}

// getMachines returns all scanned machines on the current system
func getMachines(ctx context.Context, z *zfs.Zfs) (*machines.Machines, error) {
	ds, err := z.Scan()
	if err != nil {
		return nil, err
	}
	cmdline, err := procCmdline()
	if err != nil {
		return nil, err
	}
	ms := machines.New(ctx, ds, cmdline)

	return &ms, nil
}

// procCmdline returns kernel command line
func procCmdline() (string, error) {
	content, err := ioutil.ReadFile("/proc/cmdline")
	if err != nil {
		return "", err
	}

	return string(content), nil
}
