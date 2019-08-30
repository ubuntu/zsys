package main

import (
	"os"

	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"

	"github.com/ubuntu/zsys/internal/config"
)

const (
	updateGrubCmd  = "update-grub"
	modifiedBoot   = "zsys-meta:modified-boot"
	noModifiedBoot = "zsys-meta:no-modified-boot"
)

var (
	cmdErr        error
	flagVerbosity int
	rootCmd       = &cobra.Command{
		Use:   "zsys",
		Short: "ZFS SYStem integration control zsys ",
		Long: `Zfs SYStem tool targeting an enhanced ZOL experience.
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

func main() {
	cmd := generateCommands()

	if err := cmd.Execute(); err != nil {
		// This is a usage Error (we don't use postfix E commands other than usage)
		// Usage error should be the same format than other errors
		log.SetFormatter(&log.TextFormatter{
			DisableLevelTruncation: true,
			DisableTimestamp:       true,
		})
		log.Error(err)
		os.Exit(2)
	}
	if cmdErr != nil {
		log.Error(cmdErr)
		os.Exit(1)
	}
}

func generateCommands() *cobra.Command {
	rootCmd.PersistentFlags().CountVarP(&flagVerbosity, "verbose", "v", "issue INFO (-v) and DEBUG (-vv) output")

	return rootCmd
}
