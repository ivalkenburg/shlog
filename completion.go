package main

import (
	"fmt"
	"os"
)

func runCompletion(args []string) {
	if len(args) != 1 {
		fmt.Fprintln(os.Stderr, "error: completion requires exactly one shell argument: zsh, bash, or fish")
		os.Exit(exitError)
	}
	script, err := completionScript(args[0])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(exitError)
	}
	printOutput("%s", script)
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
  local -a global_opts
  global_opts=(
    '-f[force: skip confirmation prompt]'
    '-s[simulate: show what would change without writing]'
    '--dry-run[simulate: show what would change without writing]'
    '-o[output: print resulting file content without writing]'
    '--histfile[use custom history file instead of default]:file:_files'
    '(- *)'{-h,--help}'[print help]'
  )

  _arguments "${global_opts[@]}" \
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
        'pick:interactively pick entries and print their commands'
        'undo:restore history from last backup'
        'completion:print shell completion script'
        'version:print the current version'
        'help:print help'
      )
      _describe 'command' commands
      ;;
    args)
      case $words[1] in
        del)
          _arguments "${global_opts[@]}" \
            '--match[regex pattern to match against command text]:pattern' \
            '--invert[delete entries that do NOT match (requires --match)]' \
            '--pick[interactively select entries to delete]' \
            '--sort[result ordering (requires --pick)]:order:(newest relevance frequency)'
          ;;
        clean)
          _arguments "${global_opts[@]}" \
            '--keep-oldest[keep the first occurrence instead of the most recent]'
          ;;
        pick)
          _arguments "${global_opts[@]}" \
            '--multi[select multiple entries]' \
            '--unique[group identical commands]' \
            '--sort[result ordering]:order:(newest relevance frequency)'
          ;;
        list|grep|stats|undo|version|help)
          _arguments "${global_opts[@]}"
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

  local commands="del clean list grep stats pick undo completion version help"
  local global_opts="-f -s --dry-run -o --histfile -h --help"

  # Track literal arguments and opaque values before choosing completions.
  local cmd="" expecting="" literal=0
  local i word
  for ((i=1; i < cword; i++)); do
    word="${words[$i]}"
    if [[ -n "$expecting" ]]; then
      expecting=""
      continue
    fi
    if (( literal )); then
      continue
    fi
    case "$word" in
      --) literal=1 ;;
      --histfile) expecting=file ;;
      --histfile=*) ;;
      -f|-s|--dry-run|-o|-h|--help) ;;
      *)
        if [[ -z "$cmd" ]]; then
          cmd="$word"
        elif [[ "$cmd" == "del" && "$word" == "--match" ]]; then
          expecting=pattern
        elif [[ ( "$cmd" == "pick" || "$cmd" == "del" ) && "$word" == "--sort" ]]; then
          expecting=sort
        fi
        ;;
    esac
  done
  COMPREPLY=()
  if (( literal )); then
    return
  fi
  case "$expecting" in
    file)
      while IFS= read -r; do
        COMPREPLY+=("$REPLY")
      done < <(compgen -f -- "$cur")
      return
      ;;
    sort)
      COMPREPLY=($(compgen -W "newest relevance frequency" -- "$cur"))
      return
      ;;
    pattern) return ;;
  esac

  case "$cmd" in
    del)
      if [[ "$cur" == -* ]]; then
        COMPREPLY=($(compgen -W "$global_opts --match --invert --pick --sort" -- "$cur"))
      fi
      ;;
    clean)
      if [[ "$cur" == -* ]]; then
        COMPREPLY=($(compgen -W "$global_opts --keep-oldest" -- "$cur"))
      fi
      ;;
    pick)
      if [[ "$cur" == -* ]]; then
        COMPREPLY=($(compgen -W "$global_opts --multi --unique --sort" -- "$cur"))
      fi
      ;;
    list|grep|stats|undo|version|help)
      if [[ "$cur" == -* ]]; then
        COMPREPLY=($(compgen -W "$global_opts" -- "$cur"))
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
complete -c shlog -s h -l help -d 'print help'

# Commands (only when no subcommand has been given yet)
set -l cmds del clean list grep stats pick undo completion version help
complete -c shlog -n "not __fish_seen_subcommand_from $cmds" -a del        -d 'delete entries matching a selection or pattern'
complete -c shlog -n "not __fish_seen_subcommand_from $cmds" -a clean      -d 'remove duplicate entries'
complete -c shlog -n "not __fish_seen_subcommand_from $cmds" -a list       -d 'print entries with index and timestamp'
complete -c shlog -n "not __fish_seen_subcommand_from $cmds" -a grep       -d 'search entries with a regex pattern'
complete -c shlog -n "not __fish_seen_subcommand_from $cmds" -a stats      -d 'show history statistics'
complete -c shlog -n "not __fish_seen_subcommand_from $cmds" -a pick       -d 'interactively pick entries and print their commands'
complete -c shlog -n "not __fish_seen_subcommand_from $cmds" -a undo       -d 'restore history from last backup'
complete -c shlog -n "not __fish_seen_subcommand_from $cmds" -a completion -d 'print shell completion script'
complete -c shlog -n "not __fish_seen_subcommand_from $cmds" -a version    -d 'print the current version'
complete -c shlog -n "not __fish_seen_subcommand_from $cmds" -a help       -d 'print help'

# del subcommand options
complete -c shlog -n '__fish_seen_subcommand_from del'   -l match -r -d 'regex pattern to match against command text'
complete -c shlog -n '__fish_seen_subcommand_from del'   -l invert -d 'delete entries that do NOT match (requires --match)'
complete -c shlog -n '__fish_seen_subcommand_from del'   -l pick   -d 'interactively select entries to delete'

complete -c shlog -n '__fish_seen_subcommand_from del'   -l sort -x -a 'newest relevance frequency' -d 'result ordering (requires --pick)'

# clean subcommand options
complete -c shlog -n '__fish_seen_subcommand_from clean' -l keep-oldest -d 'keep the first occurrence instead of the most recent'

# pick subcommand options
complete -c shlog -n '__fish_seen_subcommand_from pick'  -l multi -d 'select multiple entries'

complete -c shlog -n '__fish_seen_subcommand_from pick' -l unique -d 'group identical commands'
complete -c shlog -n '__fish_seen_subcommand_from pick' -l sort -x -a 'newest relevance frequency' -d 'result ordering'

# completion subcommand: suggest shell names
complete -c shlog -n '__fish_seen_subcommand_from completion' -a 'zsh bash fish' -d 'shell'
`
