package cmdhandler

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

// SubcommandsRequiredWithSuggestions will ensure we have a subcommand provided by the user and augments it with
// suggestion for commands, alias and help on root command.
func SubcommandsRequiredWithSuggestions(cmd *cobra.Command, args []string) error {
	requireMsg := "%s requires a valid subcommand"
	// This will be triggered if cobra didn't find any subcommands.
	// Find some suggestions.
	var suggestions []string

	if len(args) != 0 && !cmd.DisableSuggestions {
		typedName := args[0]
		if cmd.SuggestionsMinimumDistance <= 0 {
			cmd.SuggestionsMinimumDistance = 2
		}
		// subcommand suggestions
		suggestions = append(cmd.SuggestionsFor(args[0]))

		// subcommand alias suggestions (with distance, not exact)
		for _, c := range cmd.Commands() {
			if c.IsAvailableCommand() {
				for _, alias := range c.Aliases {
					candidate := suggestsByPrefixOrLd(typedName, alias, cmd.SuggestionsMinimumDistance)
					if candidate == "" {
						continue
					}
					suggestions = append(suggestions, candidate)
				}
			}
		}

		// help for root command
		if !cmd.HasParent() {
			candidate := suggestsByPrefixOrLd(typedName, "help", cmd.SuggestionsMinimumDistance)
			if candidate != "" {
				suggestions = append(suggestions, candidate)
			}
		}
	}

	var suggestionsMsg string
	if len(suggestions) > 0 {
		suggestionsMsg += "Did you mean this?\n"
		for _, s := range suggestions {
			suggestionsMsg += fmt.Sprintf("\t%v\n", s)
		}
	}

	if suggestionsMsg != "" {
		requireMsg = fmt.Sprintf("%s. %s", requireMsg, suggestionsMsg)
	}

	return fmt.Errorf(requireMsg, cmd.Name())
}

// suggestsByPrefixOrLd suggests a command by levenshtein distance or by prefix.
// It returns an empty string if nothing was found
func suggestsByPrefixOrLd(typedName, candidate string, minDistance int) string {
	levenshteinDistance := ld(typedName, candidate, true)
	suggestByLevenshtein := levenshteinDistance <= minDistance
	suggestByPrefix := strings.HasPrefix(strings.ToLower(candidate), strings.ToLower(typedName))
	if !suggestByLevenshtein && !suggestByPrefix {
		return ""
	}
	return candidate
}

// ld compares two strings and returns the levenshtein distance between them.
func ld(s, t string, ignoreCase bool) int {
	if ignoreCase {
		s = strings.ToLower(s)
		t = strings.ToLower(t)
	}
	d := make([][]int, len(s)+1)
	for i := range d {
		d[i] = make([]int, len(t)+1)
	}
	for i := range d {
		d[i][0] = i
	}
	for j := range d[0] {
		d[0][j] = j
	}
	for j := 1; j <= len(t); j++ {
		for i := 1; i <= len(s); i++ {
			if s[i-1] == t[j-1] {
				d[i][j] = d[i-1][j-1]
			} else {
				min := d[i-1][j]
				if d[i][j-1] < min {
					min = d[i][j-1]
				}
				if d[i-1][j-1] < min {
					min = d[i-1][j-1]
				}
				d[i][j] = min + 1
			}
		}

	}
	return d[len(s)][len(t)]
}
