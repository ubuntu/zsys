package client

import (
	"github.com/spf13/cobra"

	"github.com/ubuntu/zsys/internal/config"
)

var (
	cmdErr        error
	flagVerbosity int
	rootCmd       = &cobra.Command{
		Use:   "zsysctl",
		Short: "ZFS SYStem integration control zsys ",
		Long: `Zfs SYStem tool targetting an enhanced ZOL experience.
 It allows running multiple ZFS system in parallels on the same machine,
 get automated snapshots, managing complex zfs dataset layouts separating
 user data from system and persistent data, and more.`,
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			config.SetVerboseMode(flagVerbosity)
		},
		// We display usage error ourselves
		SilenceErrors: true,
	}
)

func init() {
	rootCmd.PersistentFlags().CountVarP(&flagVerbosity, "verbose", "v", "issue INFO (-v) and DEBUG (-vv) output")
}

// Cmd returns the zsysctl command and options
func Cmd() *cobra.Command {
	return rootCmd
}

// Error returns the zsysctl command error
func Error() error {
	return cmdErr
}
