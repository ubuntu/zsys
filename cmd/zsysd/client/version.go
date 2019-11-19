package client

import (
	"fmt"
	"github.com/spf13/cobra"
	"github.com/ubuntu/zsys/internal/config"
	"github.com/ubuntu/zsys/internal/i18n"
)

var (
	versionCmd = &cobra.Command{
		Use:   "version",
		Short: i18n.G("Returns version of client and server"),
		Args:  cobra.NoArgs,
		Run:   func(cmd *cobra.Command, args []string) { cmdErr = getVersion() },
	}
)

func init() {
	rootCmd.AddCommand(versionCmd)
}

// getVersion returns the current server and client versions.
func getVersion() (err error) {
	fmt.Println(config.Version)

	return nil
}
