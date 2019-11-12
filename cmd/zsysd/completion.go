package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/ubuntu/zsys/internal/i18n"
)

func installCompletionCmd(rootCmd *cobra.Command) {
	prog := rootCmd.Name()
	var completionCmd = &cobra.Command{
		Use:   "completion",
		Short: i18n.G("Generates bash completion scripts"),
		Long: fmt.Sprintf(i18n.G(`To load completion run

. <(%s completion)

To configure your bash shell to load completions for each session add to your ~/.bashrc or ~/.profile:

. <(%s completion)
`), prog, prog),
		Run: func(cmd *cobra.Command, args []string) {
			genBashCompletion(rootCmd, os.Stdout)
		},
	}
	rootCmd.AddCommand(completionCmd)
}
