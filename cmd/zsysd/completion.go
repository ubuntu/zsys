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
		Short: i18n.G("Generates completion scripts"),
		Long: strings.ReplaceAll(i18n.G(`To load completions:

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
			tmpShell := os.Getenv("SHELL")
			shell := "unknown"
			if len(args) > 0 && args[0] != "" {
				shell = args[0]
			} else {
				tmpShell = filepath.Base(tmpShell)
				if tmpShell != "." {
					shell = tmpShell
				}
			}
			switch shell {
			case "bash":
				genBashCompletion(cmd.Root(), os.Stdout)
			case "zsh":
				cmd.Root().GenZshCompletion(os.Stdout)
			case "powershell":
				cmd.Root().GenPowerShellCompletion(os.Stdout)
			default:
				cmd.SilenceUsage = false
				cmd.PrintErrf("Shell preset unkown: %-36s\n", shell)
				cmd.Usage()
				os.Exit(1)
			}
		},
	}
	rootCmd.AddCommand(completionCmd)
}
