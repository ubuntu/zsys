package main

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/ubuntu/zsys/internal/i18n"
)

func installCompletionCmd(rootCmd *cobra.Command) {
	prog := rootCmd.Name()
	var completionCmd = &cobra.Command{
		Use:   "completion [bash|zsh|powershell]",
		Short: i18n.G("Generates completion scripts (will attempt to automatically detect shell)"),
		Long: strings.ReplaceAll(i18n.G(`To load completions:
NOTE: When shell type isn't defined shell will be automatically identified based on the $SHELL environment vairable

Bash:

  $ source <(%s completion bash)

  # To load completions for each session, execute once:
  # Linux:
  $ %s completion bash > /etc/bash_completion.d/%s
  # macOS:
  $ %s completion bash > /usr/local/etc/bash_completion.d/%s

Zsh:

  # If shell completion is not already enabled in your environment,
  # you will need to enable it.  You can execute the following once:

  $ echo "autoload -U compinit; compinit" >> ~/.zshrc

  # To load completions for each session, execute once:
  $ %s completion zsh > "${fpath[1]}/_%s"

  # You will need to start a new shell for this setup to take effect.

PowerShell:

  PS> %s completion powershell | Out-String | Invoke-Expression

  # To load completions for every new session, run:
  PS> %s completion powershell > %s.ps1
  # and source this file from your PowerShell profile.
`), "%s", prog),
		ValidArgs: []string{"bash", "zsh", "powershell"},
		Args:      cobra.MaximumNArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			var shell string
			var suppliedArg string
			if len(args) > 0 && args[0] != "" {
				shell = args[0]
				suppliedArg = shell
			} else {
				shell = os.Getenv("SHELL")
				suppliedArg = "Autodetect " + shell
			}

			if len(shell) > 0 {
				shell = filepath.Base(shell)
			}

			switch shell {
			case "bash":
				genBashCompletion(cmd.Root(), os.Stdout)
			case "zsh":
				cmd.Root().GenZshCompletion(os.Stdout)
			case "powershell":
				cmd.Root().GenPowerShellCompletion(os.Stdout)
			default:
				cmd.PrintErrf("Shell preset unkown: %s\n", suppliedArg)
				cmd.Usage()
				os.Exit(1)
			}
		},
	}
	rootCmd.AddCommand(completionCmd)
}
