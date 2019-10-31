package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

func installCompletionCmd(rootCmd *cobra.Command) {
	prog := os.Args[0]
	var completionCmd = &cobra.Command{
		Use:   "completion",
		Short: "Generates bash completion scripts",
		Long: fmt.Sprintf(`To load completion run

. <(%s completion)

To configure your bash shell to load completions for each session add to your bashrc

# ~/.bashrc or ~/.profile
. <(%s completion)
`, prog, prog),
		Run: func(cmd *cobra.Command, args []string) {
			genBashCompletion(rootCmd, os.Stdout)
		},
	}
	rootCmd.AddCommand(completionCmd)
}
