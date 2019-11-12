package client

import (
	"github.com/spf13/cobra"
	"github.com/ubuntu/zsys/cmd/zsysd/cmdhandler"
	"github.com/ubuntu/zsys/internal/config"
	"github.com/ubuntu/zsys/internal/i18n"
)

var (
	cmdErr        error
	flagVerbosity int
	rootCmd       = &cobra.Command{
		Use:   "zsysctl COMMAND",
		Short: i18n.G("ZFS SYStem integration control zsys daemon"),
		Long: i18n.G(`Zfs SYStem tool targetting an enhanced ZOL experience.
 It allows running multiple ZFS system in parallels on the same machine,
 get automated snapshots, managing complex zfs dataset layouts separating
 user data from system and persistent data, and more.`),
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			config.SetVerboseMode(flagVerbosity)
		},
		Args: cmdhandler.SubcommandsRequiredWithSuggestions,
		Run:  cmdhandler.NoCmd,
		// We display usage error ourselves
		SilenceErrors: true,
	}
)

func init() {
	rootCmd.PersistentFlags().CountVarP(&flagVerbosity, "verbose", "v", i18n.G("issue INFO (-v) and DEBUG (-vv) output"))
}

// Cmd returns the zsysctl command and options
func Cmd() *cobra.Command {
	return rootCmd
}

// Error returns the zsysctl command error
func Error() error {
	return cmdErr
}
