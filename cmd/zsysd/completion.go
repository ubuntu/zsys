package main

import (
	"fmt"
	"os"
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

  $ source <(#prog# completion bash)

  # To load completions for each session, execute once:
  # Linux:
  $ #prog# completion bash > /etc/bash_completion.d/#prog#
  # macOS:
  $ #prog# completion bash > /usr/local/etc/bash_completion.d/#prog#

Zsh:

  # If shell completion is not already enabled in your environment,
  # you will need to enable it.  You can execute the following once:

  $ echo "autoload -U compinit; compinit" >> ~/.zshrc

  # To load completions for each session, execute once:
  $ #prog# completion zsh > "${fpath[1]}/_#prog#"

  # You will need to start a new shell for this setup to take effect.

PowerShell:

  PS> #prog# completion powershell | Out-String | Invoke-Expression

  # To load completions for every new session, run:
  PS> #prog# completion powershell > #prog#.ps1
  # and source this file from your PowerShell profile.
`), "#prog#", prog),
		DisableFlagsInUseLine: true,
		ValidArgs:             []string{"bash", "zsh", "powershell"},
		Args:                  cobra.MaximumNArgs(1), //cobra.ExactValidArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			var arg = "bash"
			if len(args) > 0 && args[0] != "" {
				arg = args[0]
			}
			switch arg {
			case "bash":
				genBashCompletion(cmd.Root(), os.Stdout)
			case "zsh":
				cmd.Root().GenZshCompletion(os.Stdout)
			case "powershell":
				cmd.Root().GenPowerShellCompletion(os.Stdout)
			default:
				os.Stdout.WriteString(fmt.Sprintf("Shell preset unkown: %-36s\n", arg))
			}
		},
	}
	rootCmd.AddCommand(completionCmd)
}
