package main

import (
	"fmt"
	"os"
)

func runCompletion(args []string) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "error: completion requires a shell argument: zsh, bash, or fish")
		os.Exit(1)
	}
	script, err := completionScript(args[0])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Print(script)
}

func completionScript(shell string) (string, error) {
	switch shell {
	case "zsh":
		return zshCompletion, nil
	case "bash":
		return bashCompletion, nil
	case "fish":
		return fishCompletion, nil
	default:
		return "", fmt.Errorf("error: unknown shell %q (supported: zsh, bash, fish)", shell)
	}
}

const zshCompletion = `#compdef shlog

_shlog() {
  local state

  _arguments \
    '-f[force: skip confirmation prompt]' \
    '-s[simulate: show what would change without writing]' \
    '--dry-run[simulate: show what would change without writing]' \
    '-o[output: print resulting file content without writing]' \
    '--histfile[use custom history file instead of ~/.zsh_history]:file:_files' \
    '1:command:->command' \
    '*::args:->args'

  case $state in
    command)
      local commands=(
        'del:delete entries matching a selection or pattern'
        'clean:remove duplicate entries'
        'list:print entries with index and timestamp'
        'grep:search entries with a regex pattern'
        'stats:show history statistics'
        'pick:interactively pick entries and print their commands (requires fzf)'
        'undo:restore history from last backup'
        'completion:print shell completion script'
        'version:print the current version'
      )
      _describe 'command' commands
      ;;
    args)
      case $words[1] in
        del)
          _arguments \
            '--match[regex pattern to match against command text]:pattern' \
            '--invert[delete entries that do NOT match (requires --match)]' \
            '--pick[interactively select entries to delete using fzf]'
          ;;
        clean)
          _arguments \
            '--keep-oldest[keep the first occurrence instead of the most recent]'
          ;;
        pick)
          _arguments \
            '--multi[select multiple entries]'
          ;;
        completion)
          local shells=(zsh bash fish)
          _describe 'shell' shells
          ;;
      esac
      ;;
  esac
}

_shlog "$@"
`

const bashCompletion = `_shlog() {
  local cur prev words cword
  _init_completion 2>/dev/null || {
    COMPREPLY=()
    cur="${COMP_WORDS[COMP_CWORD]}"
    prev="${COMP_WORDS[COMP_CWORD-1]}"
    words=("${COMP_WORDS[@]}")
    cword=$COMP_CWORD
  }

  local commands="del clean list grep stats pick undo completion version"
  local global_opts="-f -s --dry-run -o --histfile"

  # Complete --histfile argument with file paths
  if [[ "$prev" == "--histfile" ]]; then
    COMPREPLY=($(compgen -f -- "$cur"))
    return
  fi

  # Find the subcommand (first non-flag argument after position 0)
  local cmd=""
  local i
  for ((i=1; i < ${#words[@]}; i++)); do
    case "${words[$i]}" in
      --histfile) ((i++)) ;;
      -*) ;;
      *) cmd="${words[$i]}"; break ;;
    esac
  done

  case "$cmd" in
    del)
      if [[ "$cur" == -* ]]; then
        COMPREPLY=($(compgen -W "--match --invert --pick $global_opts" -- "$cur"))
      fi
      ;;
    clean)
      if [[ "$cur" == -* ]]; then
        COMPREPLY=($(compgen -W "--keep-oldest $global_opts" -- "$cur"))
      fi
      ;;
    pick)
      if [[ "$cur" == -* ]]; then
        COMPREPLY=($(compgen -W "--multi $global_opts" -- "$cur"))
      fi
      ;;
    completion)
      COMPREPLY=($(compgen -W "zsh bash fish" -- "$cur"))
      ;;
    "")
      if [[ "$cur" == -* ]]; then
        COMPREPLY=($(compgen -W "$global_opts" -- "$cur"))
      else
        COMPREPLY=($(compgen -W "$commands" -- "$cur"))
      fi
      ;;
  esac
}

complete -F _shlog shlog
`

const fishCompletion = `# Fish completion for shlog
complete -c shlog -f

# Global options
complete -c shlog -s f -d 'force: skip confirmation prompt'
complete -c shlog -s s -d 'simulate: show what would change without writing'
complete -c shlog -l dry-run -d 'simulate: show what would change without writing'
complete -c shlog -s o -d 'output: print resulting file content without writing'
complete -c shlog -l histfile -r -d 'use custom history file instead of default'

# Commands (only when no subcommand has been given yet)
set -l cmds del clean list grep stats pick undo completion version
complete -c shlog -n "not __fish_seen_subcommand_from $cmds" -a del        -d 'delete entries matching a selection or pattern'
complete -c shlog -n "not __fish_seen_subcommand_from $cmds" -a clean      -d 'remove duplicate entries'
complete -c shlog -n "not __fish_seen_subcommand_from $cmds" -a list       -d 'print entries with index and timestamp'
complete -c shlog -n "not __fish_seen_subcommand_from $cmds" -a grep       -d 'search entries with a regex pattern'
complete -c shlog -n "not __fish_seen_subcommand_from $cmds" -a stats      -d 'show history statistics'
complete -c shlog -n "not __fish_seen_subcommand_from $cmds" -a pick       -d 'interactively pick entries using fzf'
complete -c shlog -n "not __fish_seen_subcommand_from $cmds" -a undo       -d 'restore history from last backup'
complete -c shlog -n "not __fish_seen_subcommand_from $cmds" -a completion -d 'print shell completion script'
complete -c shlog -n "not __fish_seen_subcommand_from $cmds" -a version    -d 'print the current version'

# del subcommand options
complete -c shlog -n '__fish_seen_subcommand_from del'   -l match  -d 'regex pattern to match against command text'
complete -c shlog -n '__fish_seen_subcommand_from del'   -l invert -d 'delete entries that do NOT match (requires --match)'
complete -c shlog -n '__fish_seen_subcommand_from del'   -l pick   -d 'interactively select entries to delete using fzf'

# clean subcommand options
complete -c shlog -n '__fish_seen_subcommand_from clean' -l keep-oldest -d 'keep the first occurrence instead of the most recent'

# pick subcommand options
complete -c shlog -n '__fish_seen_subcommand_from pick'  -l multi -d 'select multiple entries'

# completion subcommand: suggest shell names
complete -c shlog -n '__fish_seen_subcommand_from completion' -a 'zsh bash fish' -d 'shell'
`
