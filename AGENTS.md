# tmux-shunpo — Architecture Guide

This document describes the Go implementation of tmux-shunpo v0.2.0.

---

## Overview

Harpoon2-style tmux navigation with two core features:
1. **Marks (1-9)** — instant jump to bookmarked sessions
2. **Tools (@1-@9)** — per-session persistent window slots

Session management is delegated to [sesh](https://github.com/joshmedeski/sesh). tmux-shunpo provides the marks and tools layer on top.

---

## Architecture

```
tmux-shunpo (~3800 lines Go)
├── main.go          — CLI entry point, dependency checking, command dispatch
├── config.go        — TOML config parsing (go-toml/v2)
├── marks.go         — Mark management (parse, add, remove, rearrange)
├── tools.go         — Tool management (get, set, remove, compact, explicit initialization)
├── tmux.go          — tmux operations (navigate, session connect, window state)
├── window_labels.go — physical-key/index labels and session-wide reconciliation
├── ui.go            — gum-based TUI editors
├── completion.go    — Shell completions (bash, zsh, fish)
└── utils.go         — Validation, atomic writes, path utilities
```

### Key Design Decisions

1. **Slot Range**: Fixed at 1-9 (constants `MinSlot`/`MaxSlot`) for keyboard-driven UX
2. **Window Base**: Configurable via `tool_window_base` (default: 88)
3. **Atomic Writes**: All file mutations use write-to-temp + rename pattern
4. **Validation**: Whitelist-based command validation prevents injection
5. **Explicit initialization**: New sessions start empty; first tool access asks whether to load sesh.toml defaults, load slots from a preset, or start empty

---

## Dependencies

| Tool | Required | Purpose |
|---|---|---|
| tmux >= 3.2 | Yes | Multiplexer with popup support |
| sesh | Yes | Session management backend |
| gum | Yes | TUI widgets for editors |
| zoxide | Optional | Frecency-based directory ranking (via sesh) |

**Runtime**: Go 1.25+ (compile only, not needed after build)

**Go packages**:
- `github.com/pelletier/go-toml/v2` — TOML parsing
- `golang.org/x/term` — TTY detection

---

## Configuration

### shunpo config (`~/.config/tmux-shunpo/config.toml`)

UI and behavior settings only:
- `tool_window_base` — Window index for slot 1 (default: 88)
- `tool_window_prefix` — Tool label prefix (default: "@")
- `normal_window_prefix` — Normal-window label prefix (default: "#")
- Prefixes are non-empty/distinct; surrounding separator whitespace is normalized
- `shell_init_delay` — Wait for shell prompts before sending commands
- `ui.*` — Popup dimensions and styling
- `presets` — Named tool configurations for `--bootstrap`

### sesh config (`~/.config/sesh/sesh.toml`)

tmux-shunpo reads sesh.toml for:
- `[[window]]` — Tool templates (referenced as `@name`)
- `[[session]]` — Per-session tool initialization defaults
- `[[wildcard]]` — Pattern-based tool initialization defaults
- `[default_session]` — Fallback tool initialization defaults

---

## Data Storage

```
~/.local/share/tmux-shunpo/
├── marks                    — Global mark bookmarks (slot: session/path)
├── session_state            — Last window per session (save/restore)
└── tools/
    ├── <session-1>          — Per-session tool configurations
    ├── <session-2>
    └── ...
```

**File format**: Plain text, one entry per line (`SLOT: VALUE`)

**Window metadata**: tmux window option `@shunpo_label` records the exact label
last applied to a live window. It is ephemeral tmux state, not persisted on disk.

**Atomic writes**: All mutations use temp file + rename to prevent corruption

---

## Security Model

Tool commands are typed into the user's own interactive shell via `tmux
send-keys`. There is **no privilege boundary** — the user can already run
anything as themselves — so shell metacharacters (`|`, `;`, `$`, `&`, …) in tool
commands are allowed by design. The validators below are input hygiene and
identifier safety, not an injection sandbox.

### Input Validation

| Function | Validates | Notes |
|---|---|---|
| `validateCommand` | Tool commands | Loose charset (alphanumerics, spaces, common shell metachars); rejects empty and `../..` traversal |
| `validateSessionName` | Session names | `^[a-zA-Z0-9._-]+$`; rejects path traversal |
| `validateTemplateName` | Template refs | `^[a-zA-Z0-9_-]+$` |
| `validateMarkEntry` | Mark values | `^[a-zA-Z0-9._/~-]+$` |

### Guardrails (not a security boundary)

`isDestructive` / `[guardrails]` show a one-time "this looks destructive — save
it anyway?" confirmation when a catastrophic command (`rm`, `dd`, `mkfs`, …) is
**set interactively**, or when a preset containing one is applied. It is a
convenience to catch typos/bad pastes — best-effort and bypassable. It does
**not** fire on the run path (`Alt-@n` stays instant), and commands defined in
`[presets]` or `sesh.toml` are not checked.

### Config File Safety

Before parsing, config files are checked for:
- **Ownership**: Must be owned by current user (except `/nix/store/`)
- **Permissions**: Must not be world-writable (mode & 0002 == 0)

### Command Execution

- Tool commands are charset-validated before storage and execution
- Paths sent to tmux are escaped using a whitelist approach
- Template names are validated before sesh lookups (prevents injection)

---

## Tool Slot Behaviors

| Type | Syntax | Behavior |
|---|---|---|
| **Template** | `@editor` | Resolves to sesh.toml `startup_script`. Persistent. |
| **Shell** | `@shell` | Opens default shell. Persistent. |
| **Attached** | `@attached` | Links to existing window. Removed if window closes. |
| **Raw command** | `cargo test` | Runs directly. Persistent. |

**Persistence**: Persistent tools are re-created with their command if the window is closed. Attached tools are removed.

---

## Window Label Policy

Shunpo owns only a leading navigation label; the remaining live window name is
user-owned and is never truncated or restored from the configured command.

| Window | Default label | Example |
|---|---|---|
| Tool | Physical tmux binding; logical slot fallback | `@u opus auth`, `@7 logs` |
| Normal | Current tmux window index | `#2 server` |

Physical mappings are discovered from unique direct tmux bindings whose command
is `run-shell "tmux-shunpo --goto @N"`. Missing, ambiguous, or unavailable
bindings fall back to `@N` with a warning. `M-u` is displayed as `@u`.

`@shunpo_label` records the exact applied label. Shunpo removes a recorded label
only at a complete token boundary, then preserves the descriptor verbatim. With
no metadata, only the independently calculated current label is adopted; there
is no legacy-name migration. Linked windows are skipped because their indexes
may differ across sessions.

The `--tools` overview renders `SLOT`, `WINDOW`, and `SOURCE` columns. Live
windows show their tmux name; configured persistent tools without a live window
show `@key —`. Template and shell sources show their resolution, attached tools
show `@attached`, and raw commands show the stored command literally. The action
menu exposes `Label windows`, which reuses the session-wide labeling command.

---

## Testing

Functions have unit tests in `tmux_shunpo_test.go` and `window_labels_test.go`:

```bash
go test -v ./...
```

**Mock pattern**: `MockRunner` interface allows testing tmux/sesh interactions without real processes.

**Coverage**:
- Validation functions
- Mark operations (parse, set, add, remove, rearrange)
- Tool operations (get, set, remove, compact, initialize)
- Navigation (navigateToTool, sessionConnectWithState)
- Bootstrap (error cases)
- Config loading
- Session window state (save/restore)

---

## CLI Reference

```
Session Navigation:
  --goto N               Jump to mark slot N (1-9)
  --goto @N              Navigate to tool window slot N (1-9)
  --connect <session>    Connect to session by name

Mark Management:
  --add-mark             Mark current session/directory
  --remove N             Remove mark slot N
  --remove all           Remove all marks
  --marks                Interactive mark editor
  --compact-marks        Remove gaps in mark slots

Tool Management:
  --tools                Interactive tool editor
  --add-tool             Append current window to next empty tool slot as @attached
  --compact-tools        Remove gaps in tool slots
  --bootstrap [preset]   Setup tools from preset or sesh.toml

Maintenance:
  --label-windows        Label current-session windows with @key or #index
  --reset [session|all]  Reset tools/data

Info:
  -h, --help             Show usage
  -v, --version          Show version
  --completion [shell]   Print shell completion (bash, zsh, fish)
```

---

## Keybindings

Recommended tmux.conf bindings:

```tmux
# Session search (using television)
bind-key "T" display-popup -E -w 80% -h 70% -d '#{pane_current_path}' -T 'Sesh' tv sesh

# Marks (Alt+number)
bind-key -n M-1 run-shell "tmux-shunpo --goto 1"
# ... M-2 through M-9

# Tools (prefix + letter)
bind-key "u" run-shell "tmux-shunpo --goto @1"
bind-key "i" run-shell "tmux-shunpo --goto @2"
# ... etc

# Editors / quick add
bind-key "m" run-shell "tmux-shunpo --marks"
bind-key "a" run-shell "tmux-shunpo --add-tool"
bind-key "E" run-shell "tmux-shunpo --tools"
bind-key "R" run-shell "tmux-shunpo --label-windows"
```

---

## Development

### Build

```bash
make              # Build binary to bin/tmux-shunpo
make install      # Install to ~/bin/
make test         # Run tests
make vet          # Run go vet
```

### Code Style

- All errors checked and handled explicitly
- `local` equivalent: function-scoped variables with `:=`
- Constants for magic numbers (`MinSlot`, `MaxSlot`, UI delays)
- Context-aware error display (TTY vs tmux popup)
- Atomic file operations via `atomicWrite()`

### Testing Guidelines

- Mock all external commands (tmux, sesh) via `MockRunner`
- Use `setupTestPaths()` for isolated temp directories
- Test both success and error paths
- Verify file contents after mutations
- Check that tmux/sesh are called with correct arguments

---

## Future Enhancements

Potential improvements (not currently planned):
- Configurable slot range (currently fixed 1-9)
- Session groups / workspaces
- Tool templates with arguments
- Import/export marks and tools
- Sync across machines

---

## License

MIT
