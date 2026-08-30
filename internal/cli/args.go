package cli

import (
	"strconv"
	"strings"
)

// flagsTakingValues lists every option that consumes a following argument;
// all other dash-options are boolean switches.
var flagsTakingValues = map[string]bool{
	"folderoutput": true, "fo": true,
	"output": true, "o": true,
	"site":  true,
	"proxy": true, "p": true,
	"json": true, "j": true,
	"timeout": true,
}

// combinedShorts are the single-letter boolean flags; argparse allows
// combining them (e.g. -vd), the stdlib flag package does not.
var combinedShorts = map[rune]bool{'v': true, 'd': true, 'b': true, 'l': true}

// ReorderArgs moves flags (and their values) ahead of positional arguments
// and expands combined short booleans, so that invocations like
// `sherlock-go torvalds -vd --site GitHub` parse the way argparse would.
func ReorderArgs(args []string) []string {
	var flags, positionals []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if len(arg) < 2 || arg[0] != '-' {
			positionals = append(positionals, arg)
			continue
		}
		name := strings.TrimLeft(arg, "-")

		// Combined short booleans: -vd -> -v -d
		if len(arg) > 2 && arg[1] != '-' && name != "" {
			allShort := true
			for _, r := range name {
				if !combinedShorts[r] {
					allShort = false
					break
				}
			}
			if allShort {
				for _, r := range name {
					flags = append(flags, "-"+string(r))
				}
				continue
			}
		}

		if strings.IndexByte(name, '=') >= 0 {
			flags = append(flags, arg)
			continue
		}
		flags = append(flags, arg)
		if flagsTakingValues[name] && i+1 < len(args) {
			next := args[i+1]
			// Consume the value even when it looks like a negative number.
			if _, err := strconv.ParseFloat(next, 64); err == nil || next[0] != '-' {
				flags = append(flags, next)
				i++
			}
		}
	}
	return append(flags, positionals...)
}
