package daemon

import (
	"context"
	"os"
	"os/signal"

	"github.com/spf13/cobra"

	"github.com/ubuntu/zsys"
	"github.com/ubuntu/zsys/internal/config"
	"github.com/ubuntu/zsys/internal/daemon"
)

var (
	cmdErr        error
	flagVerbosity int
	rootCmd       = &cobra.Command{
		Use:   "zsysd",
		Short: "ZFS SYStem integration daemon",
		Long: `Zfs SYStem daemon targetting an enhanced ZOL experience.
 It allows running multiple ZFS system in parallels on the same machine,
 get automated snapshots, managing complex zfs dataset layouts separating
 user data from system and persistent data, and more.`,
		Args: cobra.ExactArgs(0),
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			config.SetVerboseMode(flagVerbosity)
		},
		Run: func(cmd *cobra.Command, args []string) {
			// TODO: timeout on idling

			// trap Ctrl+C and call cancel on the context
			ctx, cancel := context.WithCancel(context.Background())
			c := make(chan os.Signal, 1)
			signal.Notify(c, os.Interrupt)
			defer func() {
				signal.Stop(c)
				cancel()
			}()
			go func() {
				select {
				case <-c:
					cancel()
				case <-ctx.Done():
				}
			}()

			cmdErr = zsys.RegisterAndListenZsysUnixSocketServer(ctx, zsys.DefaultSocket, &daemon.Server{})
		},
		// We display usage error ourselves
		SilenceErrors: true,
	}
)

func init() {
	rootCmd.PersistentFlags().CountVarP(&flagVerbosity, "verbose", "v", "issue INFO (-v) and DEBUG (-vv) output")
}

// Cmd returns the zsysd command and options
func Cmd() *cobra.Command {
	return rootCmd
}

// Error returns the zsysd command error
func Error() error {
	return cmdErr
}
