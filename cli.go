package main

import (
	"fmt"
	"strings"
)

const (
	exitNoMatch  = 1
	exitError    = 2
	exitCanceled = 130
)

type cliOptions struct {
	force, simulate, output, help bool
	histFile                      string
	args                          []string
}

// parseCLI extracts global options on either side of the command. Values of
// command options are opaque: a regex or path may itself look like a flag.
func parseCLI(args []string) (cliOptions, error) {
	opts := cliOptions{histFile: defaultHistoryFilePath()}
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			if len(opts.args) == 0 && i+1 < len(args) {
				i++
				opts.args = append(opts.args, args[i])
			}
			if len(opts.args) > 0 && commandHasOptions(opts.args[0]) {
				opts.args = append(opts.args, "--")
			}
			opts.args = append(opts.args, args[i+1:]...)
			break
		}
		if len(opts.args) > 0 && commandOptionTakesValue(opts.args[0], a) {
			if i+1 == len(args) {
				return opts, fmt.Errorf("%s requires a value argument", a)
			}
			opts.args = append(opts.args, a, args[i+1])
			i++
			continue
		}
		switch a {
		case "-f":
			opts.force = true
		case "-s", "--dry-run":
			opts.simulate = true
		case "-o":
			opts.output = true
		case "-h", "--help":
			opts.help = true
		case "--histfile":
			if i+1 == len(args) {
				return opts, fmt.Errorf("--histfile requires a path argument")
			}
			i++
			opts.histFile = args[i]
		default:
			if path, ok := strings.CutPrefix(a, "--histfile="); ok {
				opts.histFile = path
			} else {
				opts.args = append(opts.args, a)
			}
		}
	}
	return opts, nil
}

func commandHasOptions(command string) bool {
	return command == "del" || command == "clean" || command == "pick"
}

func commandOptionTakesValue(command, option string) bool {
	return command == "del" && option == "--match" ||
		(command == "del" || command == "pick") && option == "--sort"
}
