package cmdhandler

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/ubuntu/zsys/internal/i18n"
)

// NoCmd is a no-op command to just make it valid
func NoCmd(cmd *cobra.Command, args []string) {
}

// RegisterAlias allows to decorelate the alias from the main command when alias have different command level (different parents)
// README and manpage refers to them in each subsection (parents are differents, but only one is kept if we use the same object)
func RegisterAlias(cmd, parent *cobra.Command) {
	alias := *cmd
	t := fmt.Sprintf(i18n.G("Alias of %s"), cmd.CommandPath())
	if alias.Long != "" {
		t = fmt.Sprintf("%s (%s)", alias.Long, t)
	}
	alias.Long = t
	parent.AddCommand(&alias)
}
