package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

func (app *App) handleComplete(completeType string) {
	switch completeType {
	case "marks":
		if _, err := os.Stat(app.Paths.MarksFile); err == nil {
			data, err := os.ReadFile(app.Paths.MarksFile)
			if err == nil {
				fmt.Fprint(app.Stdout, string(data))
			}
		}
	case "marks-slots":
		if _, err := os.Stat(app.Paths.MarksFile); err == nil {
			file, err := os.Open(app.Paths.MarksFile)
			if err == nil {
				defer file.Close()
				scanner := bufio.NewScanner(file)
				for scanner.Scan() {
					line := strings.TrimSpace(scanner.Text())
					if line == "" || strings.HasPrefix(line, "#") {
						continue
					}
					parts := strings.SplitN(line, ":", 2)
					if len(parts) > 0 {
						if _, err := strconv.Atoi(parts[0]); err == nil {
							fmt.Fprintln(app.Stdout, parts[0])
						}
					}
				}
			}
		}
	case "sessions":
		// sesh list
		out, err := app.Runner.Run("sesh", []string{"list"}, nil)
		if err == nil {
			fmt.Fprint(app.Stdout, string(out))
		}
	case "presets":
		for k := range app.Config.Presets {
			fmt.Fprintln(app.Stdout, k)
		}
	case "tools":
		sessionNameBytes, err := app.Runner.Run("tmux", []string{"display-message", "-p", "#S"}, nil)
		if err == nil {
			sessionName := strings.TrimSpace(string(sessionNameBytes))
			toolsFile := filepath.Join(app.Paths.DataDir, "tools", sessionName)
			if _, err := os.Stat(toolsFile); err == nil {
				data, err := os.ReadFile(toolsFile)
				if err == nil {
					fmt.Fprint(app.Stdout, string(data))
				}
			}
		}
	}
}

func completionBash() string {
	return `_tmux_shunpo() {
    local cur="${COMP_WORDS[COMP_CWORD]}"
    local prev="${COMP_WORDS[COMP_CWORD-1]}"

    case "$prev" in
        --goto)
            local marks slots sessions
            marks=$(tmux-shunpo --_complete marks 2>/dev/null | sed 's/^[0-9]*:[[:space:]]*//' || true)
            slots="1 2 3 4 5 6 7 8 9 @1 @2 @3 @4 @5 @6 @7 @8 @9"
            sessions=$(tmux-shunpo --_complete sessions 2>/dev/null || true)
            COMPREPLY=($(compgen -W "$slots $marks $sessions" -- "$cur"))
            return
            ;;
        --remove)
            local slots
            slots=$(tmux-shunpo --_complete marks-slots 2>/dev/null || true)
            COMPREPLY=($(compgen -W "all $slots" -- "$cur"))
            return
            ;;
        --connect)
            local sessions
            sessions=$(tmux-shunpo --_complete sessions 2>/dev/null || true)
            COMPREPLY=($(compgen -W "$sessions" -- "$cur"))
            return
            ;;
        --reset)
            COMPREPLY=($(compgen -W "session all" -- "$cur"))
            return
            ;;
        --completion)
            COMPREPLY=($(compgen -W "bash zsh fish" -- "$cur"))
            return
            ;;
        --_complete)
            COMPREPLY=($(compgen -W "marks marks-slots sessions presets tools" -- "$cur"))
            return
            ;;
    esac

    local exclusive_actions='--add-mark --marks --compact-marks --tools --add-tool --compact-tools --label-windows doctor --doctor --help --version -h -v - --completion --_complete'
    local arg_actions='--goto --connect --remove --reset'
    local i
    for ((i = 1; i < COMP_CWORD; i++)); do
        local word="${COMP_WORDS[i]}"
        if [[ " ${exclusive_actions} " == *" ${word} "* ]]; then
            return
        fi
        if [[ " ${arg_actions} " == *" ${word} "* ]]; then
            if [[ $((i + 1)) -lt COMP_CWORD ]]; then
                return
            fi
        fi
        if [[ "$word" == "--bootstrap" ]]; then
            local presets
            presets=$(tmux-shunpo --_complete presets 2>/dev/null || true)
            COMPREPLY=($(compgen -W "--force $presets" -- "$cur"))
            return
        fi
        if [[ "$word" == "--force" && "${COMP_WORDS[i-1]}" == "--bootstrap" ]]; then
            local presets
            presets=$(tmux-shunpo --_complete presets 2>/dev/null || true)
            COMPREPLY=($(compgen -W "$presets" -- "$cur"))
            return
        fi
    done

    if [[ "$cur" == -* ]]; then
        COMPREPLY=($(compgen -W '--goto --connect --add-mark --remove --marks --compact-marks --tools --add-tool --compact-tools --bootstrap --label-windows --reset --doctor --help --version --completion' -- "$cur"))
    else
        COMPREPLY=($(compgen -W 'doctor' -- "$cur"))
    fi
}
complete -F _tmux_shunpo tmux-shunpo
`
}

func completionZsh() string {
	return `#compdef tmux-shunpo

_tmux_shunpo() {
    local curcontext="$curcontext"

    local -a exclusive_actions
    exclusive_actions=(--add-mark --marks --compact-marks --tools --add-tool --compact-tools --label-windows doctor --doctor --help --version -h -v --completion --_complete)
    local -a arg_actions
    arg_actions=(--goto --connect --remove --reset)

    local i word bootstrap_found=false
    for ((i = 2; i < CURRENT; i++)); do
        word="${words[$i]}"
        if (( ${exclusive_actions[(Ie)$word]} )); then
            return
        fi
        if (( ${arg_actions[(Ie)$word]} )); then
            if (( i + 1 < CURRENT )); then
                return
            fi
        fi
        if [[ "$word" == "--bootstrap" ]]; then
            bootstrap_found=true
        fi
    done

    if [[ "$bootstrap_found" == true ]]; then
        local -a comps
        comps=(--force)
        local -a presets
        presets=($(tmux-shunpo --_complete presets 2>/dev/null))
        comps+=(${presets[@]})
        _describe -t bootstrap 'bootstrap option' comps
        return
    fi

    _arguments -C \
        '(* -)'{-h,--help}'[Show usage]' \
        '(* -)'{-v,--version}'[Show version]' \
        '--goto[Jump to mark slot or @tool]:target:_tmux_shunpo_goto' \
        '--connect[Connect to session by name]:session:_tmux_shunpo_sessions' \
        '--add-mark[Mark current session or directory]' \
        '--remove[Remove mark slot]:slot:_tmux_shunpo_marks' \
        '--marks[Interactive mark editor]' \
        '--compact-marks[Compact mark slots]' \
        '--tools[Interactive tool editor]' \
        '--add-tool[Append current window to next empty tool slot]' \
        '--compact-tools[Compact tool slots]' \
        '--bootstrap[Bootstrap session tools]' \
        '--label-windows[Label current-session windows]' \
        '--reset[Reset tools]:scope:(session all)' \
        '(-)--doctor[Diagnose dependencies and config parsing]' \
        '1:command:(doctor)' \
        '--completion[Print shell completion script]:shell:(bash zsh fish)'
}

_tmux_shunpo_goto() {
    local -a targets
    targets=($(tmux-shunpo --_complete sessions 2>/dev/null))
    targets+=(1 2 3 4 5 6 7 8 9 @1 @2 @3 @4 @5 @6 @7 @8 @9)
    local line
    while IFS= read -r line; do
        [[ -z "$line" || "$line" == \#* ]] && continue
        if [[ "$line" =~ '^([0-9]+):[[:space:]]*(.+)$' ]]; then
            targets+=("${match[1]}:${match[2]}")
        fi
    done < <(tmux-shunpo --_complete marks 2>/dev/null)
    _describe -t targets 'target' targets
}

_tmux_shunpo_sessions() {
    local -a sessions
    sessions=($(tmux-shunpo --_complete sessions 2>/dev/null))
    _describe -t sessions 'session' sessions
}

_tmux_shunpo_marks() {
    local -a marks
    marks=(all)
    marks+=($(tmux-shunpo --_complete marks-slots 2>/dev/null))
    _describe -t marks 'slot' marks
}

_tmux_shunpo_presets() {
    local -a presets
    presets=($(tmux-shunpo --_complete presets 2>/dev/null))
    _describe -t presets 'preset' presets
}

compdef _tmux_shunpo tmux-shunpo
`
}

func completionFish() string {
	return `function __tmux_shunpo_goto
    tmux-shunpo --_complete marks 2>/dev/null | while read -l line
        if string match -qr '^[0-9]+:' "$line"
            set -l parts (string split -m1 ':' "$line")
            set -l slot (string trim "$parts[1]")
            set -l val (string trim "$parts[2]")
            printf "%s\tmark: %s\n" "$slot" "$val"
        end
    end
    for i in (seq 1 9)
        printf "@%s\ttool slot %s\n" "$i" "$i"
    end
    tmux-shunpo --_complete sessions 2>/dev/null
end

function __tmux_shunpo_marks
    echo all
    tmux-shunpo --_complete marks-slots 2>/dev/null
end

function __tmux_shunpo_sessions
    tmux-shunpo --_complete sessions 2>/dev/null
end

function __tmux_shunpo_presets
    tmux-shunpo --_complete presets 2>/dev/null
end

function __tmux_shunpo_no_action
    set -l all_actions --goto --connect --add-mark --remove --marks --compact-marks --tools --add-tool --compact-tools --bootstrap --label-windows --reset doctor --doctor --help --version --completion -h -v
    for arg in (commandline -opc)
        if contains -- $arg $all_actions
            return 1
        end
    end
    return 0
end

function __tmux_shunpo_seen
    set -l target $argv[1]
    set -l all_actions --goto --connect --add-mark --remove --marks --compact-marks --tools --add-tool --compact-tools --bootstrap --label-windows --reset doctor --doctor --help --version --completion -h -v
    set -l found_target false
    for arg in (commandline -opc)
        if test "$arg" = "$target"
            set found_target true
        else if contains -- $arg $all_actions
            return 1
        end
    end
    test "$found_target" = true
    return $status
end

function __tmux_shunpo_bootstrap_active
    __tmux_shunpo_seen --bootstrap
    return $status
end

complete -c tmux-shunpo -f
complete -c tmux-shunpo -l add-mark -d 'Mark current session' -n '__tmux_shunpo_no_action'
complete -c tmux-shunpo -l marks -d 'Interactive mark editor' -n '__tmux_shunpo_no_action'
complete -c tmux-shunpo -l compact-marks -d 'Compact mark slots' -n '__tmux_shunpo_no_action'
complete -c tmux-shunpo -l tools -d 'Interactive tool editor' -n '__tmux_shunpo_no_action'
complete -c tmux-shunpo -l add-tool -d 'Append current window to next empty tool slot' -n '__tmux_shunpo_no_action'
complete -c tmux-shunpo -l compact-tools -d 'Compact tool slots' -n '__tmux_shunpo_no_action'
complete -c tmux-shunpo -l label-windows -d 'Label current-session windows' -n '__tmux_shunpo_no_action'
complete -c tmux-shunpo -s h -l help -d 'Show usage' -n '__tmux_shunpo_no_action'
complete -c tmux-shunpo -s v -l version -d 'Show version' -n '__tmux_shunpo_no_action'
complete -c tmux-shunpo -l doctor -d 'Diagnose dependencies and config parsing' -n '__tmux_shunpo_no_action'
complete -c tmux-shunpo -a 'doctor' -d 'Diagnose dependencies and config parsing' -n '__tmux_shunpo_no_action'

complete -c tmux-shunpo -l goto -d 'Jump to mark or tool slot' -n '__tmux_shunpo_no_action'
complete -c tmux-shunpo -l goto -a '(__tmux_shunpo_goto)' -d 'Target' -n '__tmux_shunpo_seen --goto'

complete -c tmux-shunpo -l connect -d 'Connect to session by name' -n '__tmux_shunpo_no_action'
complete -c tmux-shunpo -l connect -a '(__tmux_shunpo_sessions)' -d 'Session' -n '__tmux_shunpo_seen --connect'

complete -c tmux-shunpo -l remove -d 'Remove mark slot' -n '__tmux_shunpo_no_action'
complete -c tmux-shunpo -l remove -a '(__tmux_shunpo_marks)' -d 'Slot' -n '__tmux_shunpo_seen --remove'

complete -c tmux-shunpo -l reset -d 'Reset tools' -n '__tmux_shunpo_no_action'
complete -c tmux-shunpo -l reset -a 'session all' -d 'Scope' -n '__tmux_shunpo_seen --reset'

complete -c tmux-shunpo -l completion -d 'Print shell completion script' -n '__tmux_shunpo_no_action'
complete -c tmux-shunpo -l completion -a 'bash zsh fish' -d 'Shell' -n '__tmux_shunpo_seen --completion'

complete -c tmux-shunpo -l bootstrap -d 'Bootstrap session tools' -n '__tmux_shunpo_no_action'
complete -c tmux-shunpo -l force -d 'Force bootstrap even if windows exist' -n '__tmux_shunpo_bootstrap_active'
complete -c tmux-shunpo -a '(__tmux_shunpo_presets)' -d 'Preset' -n '__tmux_shunpo_bootstrap_active'
`
}
