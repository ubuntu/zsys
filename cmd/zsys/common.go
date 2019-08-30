package main

import (
	"github.com/spf13/cobra"
	"golang.org/x/xerrors"
)

// requireSubcommand is a no-op command which return an error message to trigger
// a command usage error.
func requireSubcommand(cmd *cobra.Command, args []string) error {
	return xerrors.Errorf("%s requires a subcommand", cmd.Name())
}
