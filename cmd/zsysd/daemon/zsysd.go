package daemon

import (
	"fmt"
	"os"
	"os/signal"

	"github.com/spf13/cobra"

	"github.com/ubuntu/zsys/internal/config"
	"github.com/ubuntu/zsys/internal/daemon"
	"github.com/ubuntu/zsys/internal/i18n"
)

var (
	cmdErr        error
	flagVerbosity int
	rootCmd       = &cobra.Command{
		Use:   "zsysd",
		Short: i18n.G("ZFS SYStem integration daemon"),
		Long: i18n.G(`Zfs SYStem daemon for an enhanced ZFS on Linux experience.
 It allows running multiple ZFS system in parallels on the same machine,
 get automated snapshots, managing complex zfs dataset layouts separating
 user data from system and persistent data, and more.`),
		Args: cobra.ExactArgs(0),
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			config.SetVerboseMode(flagVerbosity)
		},
		Run: func(cmd *cobra.Command, args []string) {
			s, err := daemon.New(config.DefaultSocket)
			if err != nil {
				cmdErr = fmt.Errorf(i18n.G("Couldn't register grpc server: %v"), err)
				return
			}

			// trap Ctrl+C and shutdown the server
			c := make(chan os.Signal, 1)
			signal.Notify(c, os.Interrupt)
			go func() {
				<-c
				s.Stop()
			}()

			cmdErr = s.Listen()
		},
		// We display usage error ourselves
		SilenceErrors: true,
	}

	bootPrepareCmd = &cobra.Command{
		Use:    "boot-prepare",
		Short:  i18n.G("Prepare boot by ensuring correct system and user datasets are switched on and off, synchronously"),
		Args:   cobra.NoArgs,
		Hidden: true,
		Run:    func(cmd *cobra.Command, args []string) { cmdErr = syncBootPrepare() },
	}
)

func init() {
	rootCmd.PersistentFlags().CountVarP(&flagVerbosity, "verbose", "v", i18n.G("issue INFO (-v) and DEBUG (-vv) output"))
	rootCmd.AddCommand(bootPrepareCmd)
}

// Cmd returns the zsysd command and options
func Cmd() *cobra.Command {
	return rootCmd
}

// Error returns the zsysd command error
func Error() error {
	return cmdErr
}
