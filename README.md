# ⚡ tmux-shunpo

[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)
[![Powered by sesh](https://img.shields.io/badge/Powered%20by-sesh-blue)](https://github.com/joshmedeski/sesh)

[Harpoon2](https://github.com/ThePrimeagen/harpoon/tree/harpoon2)-inspired navigation for tmux. Instant marks for sessions and per-session tool slots for your workflows.

Named after Bleach's "shunpo" (flash step). Zero-thought, muscle-memory navigation. Session management is delegated to [sesh](https://github.com/joshmedeski/sesh); shunpo provides the marks and tools layer on top.

---

## ✨ Features

- **📍 Marks (1–9)** — Bookmark sessions for instant jumps. `Alt-1` through `Alt-9` switch projects without thinking.
- **🛠️ Tools (@1–@9)** — Persistent window slots per session. Slot 1 is always your editor, slot 2 is always your server, etc.
- **🎨 TUI Editors** — Interactive mark and tool editors powered by [gum](https://github.com/charmbracelet/gum).
- **⚙️ Tool Init** — New sessions start with no tools; the first time you open a session's tools, load `sesh.toml` defaults, load slots from a preset, or start empty.
- **🚀 Bootstrap Presets** — Spin up a whole session's tools from a predefined preset.
- **🛟 Guardrails** — Optional one-time confirmation before saving an accidental destructive command (`rm`, `dd`, `diskutil`, …).

---

## 🚀 Installation

### Dependencies

| Tool | Required | Purpose |
|---|---|---|
| `go` >= 1.25 | Yes | Compile from source |
| `tmux` >= 3.2 | Yes | Multiplexer |
| [sesh](https://github.com/joshmedeski/sesh) | Yes | Session creation, naming, zoxide integration |
| [gum](https://github.com/charmbracelet/gum) | Yes | TUI editors |
| [zoxide](https://github.com/ajeetdsouza/zoxide) | Optional | Auto-index directories (used by sesh) |

**Optional but recommended:**
- [television](https://github.com/alexpasmantier/television) - For session search via `tv sesh` (bind to `prefix+T`)

### Setup

```bash
git clone https://github.com/zzdif/tmux-shunpo.git
cd tmux-shunpo
make
make install   # Installs to ~/.local/bin by default
```

`make install` also writes a starter `~/.config/tmux-shunpo/config.toml`, creates the data directory, and installs shell completions. Override the location with `PREFIX`:

```bash
make install PREFIX=/usr/local   # Installs to /usr/local/bin
```

Make sure the install `bin` directory is on your `PATH`.

### Shell Completions

tmux-shunpo provides dynamic completions for bash, zsh, and fish. They update automatically as your marks, sessions, and presets change — no regeneration needed.

#### Quick install

```bash
make install           # Automatically compiles, configures config/data directories, and installs shell completions
```

#### Persistent files

Install once — no `eval` overhead on shell start.

**Bash** → `~/.local/share/bash-completion/completions/tmux-shunpo`

```bash
mkdir -p ~/.local/share/bash-completion/completions
tmux-shunpo --completion bash > ~/.local/share/bash-completion/completions/tmux-shunpo
```

**Zsh** → `~/.local/share/zsh/site-functions/_tmux-shunpo`

```bash
mkdir -p ~/.local/share/zsh/site-functions
tmux-shunpo --completion zsh > ~/.local/share/zsh/site-functions/_tmux-shunpo
```

Ensure the directory is in your `fpath` before `compinit`:

```zsh
# ~/.zshrc
fpath+=(~/.local/share/zsh/site-functions)
autoload -U compinit && compinit
```

**Fish** → `~/.config/fish/completions/tmux-shunpo.fish`

```bash
mkdir -p ~/.config/fish/completions
tmux-shunpo --completion fish > ~/.config/fish/completions/tmux-shunpo.fish
```

No extra config — fish auto-loads from this directory.

#### Eval (quick test)

```bash
# ~/.bashrc
eval "$(tmux-shunpo --completion bash)"

# ~/.zshrc
eval "$(tmux-shunpo --completion zsh)"

# ~/.config/fish/config.fish
tmux-shunpo --completion fish | source
```

#### What completes

| Flag | Completions |
|---|---|
| `--goto` | Marks 1–9, tools @1–@9, saved mark names, sesh sessions |
| `--remove` | `all`, filled mark slots |
| `--connect` | sesh sessions |
| `--bootstrap` | Preset names from config |
| `--reset` | `session`, `all` |
| *(no flag)* | All flags |

Restart your shell or run `exec $SHELL` after installing.

---

## ⚙️ Configuration

### tmux-shunpo config (`~/.config/tmux-shunpo/config.toml`)

UI and behavior settings only. Session and window templates live in `sesh.toml`.

> **Note:** TOML keys without a section header must come *before* any `[table]`.
> The example below keeps that order — copy it as-is.

```toml
# Tool window base index. Slot N → window (base + N - 1).
# Default: 88 → tools use windows 88–96
tool_window_base = 88

# Status-bar label prefixes. Values must be non-empty and distinct, with no
# internal whitespace or control characters. Surrounding separator whitespace
# from older configs is ignored with a warning because labels add their own space.
# Tool labels use the discovered physical binding (for example @u); normal labels
# use the live tmux index (for example #2).
tool_window_prefix = "@"
normal_window_prefix = "#"

# Shell init delay (seconds) — wait for prompt themes before sending commands
shell_init_delay = 0.2

# Maximum length of the initial command-derived descriptor. Live/manual names
# are never truncated.
window_name_max_length = 20

# UI — tmux popups styling and layout
[ui]
popup_width = "80%"
popup_height = "26"          # Default height, optimized to tightly fit TUI screens
popup_min_width = 80         # Fallback width floor for small screens
popup_min_height = 26        # Fallback height floor for small screens
popup_border_lines = "rounded" # border style: single|rounded|double|heavy|simple|padded|none
popup_border_style = "fg=default" # tmux style format for the border color
popup_style = "bg=default,fg=default" # tmux style format for the popup window body
use_nerd_fonts = false       # Enable Nerd Font icons in list editors and custom cursors

# Presets for `--bootstrap [name]`
[presets]
web = ["@editor", "npm run dev", "tail -f logs/dev.log"]
ops = ["@htop", "ssh admin@prod", "@logs"]

# Guardrails — a convenience to catch typos, NOT a security boundary.
# Confirms once before SAVING a catastrophic command (rm, dd, mkfs, diskutil, …).
# Never prompts on Alt-@N at run time; doesn't touch presets or sesh.toml commands.
[guardrails]
confirm_destructive = true
# also_confirm = ["curl", "kubectl"]   # add command names to confirm
# skip_confirm  = ["diskutil"]         # drop built-ins you don't want
```

See [`config.toml.example`](config.toml.example) for every option, including popup borders, minimum sizes, and the full guardrails reference.

### sesh config (`~/.config/sesh/sesh.toml`)

shunpo reads `sesh.toml` for tool templates and first-time initialization rules.

```toml
# Window templates — referenced as @name in tools
[[window]]
name = "editor"
startup_script = "nvim ."

[[window]]
name = "tests"
startup_script = "cargo test"

[[window]]
name = "shell"
# No startup_script → plain shell

# Initialization defaults for a specific session
[[session]]
name = "myproject"
path = "~/Code/myproject"
windows = ["editor", "tests", "shell"]

# Initialization defaults for any project under ~/Code
[[wildcard]]
pattern = "~/Code/*"
windows = ["editor", "shell"]

# Fallback when no session or wildcard matches
[default_session]
windows = ["editor", "shell"]
```

The first time you open a session's tools, shunpo asks how to initialize them — load these `sesh.toml` rules (first match wins), load slots from a preset, or start empty. New sessions are never filled automatically. Once created, the tool file is never overwritten — your edits always win.

---

## 🛠️ Tool Slot Types

| Type | Syntax | Behavior |
|---|---|---|
| **Template** | `@editor` | Resolves to `sesh.toml` `startup_script`. Window re-created with command if closed. |
| **Shell** | `@shell` | Opens your default shell. Persistent — re-created if closed. |
| **Attached** | `@attached` | Links to an existing window. Removed from tools if that window is closed. |
| **Raw command** | `cargo test` | Runs the command directly. Persistent — re-created if closed. |

### Window labels

Tool windows are labeled with their unique physical tmux binding, while normal
windows are labeled with their current tmux index:

```text
@u opus auth   # tool @1 bound to u or M-u
#2 server      # normal tmux window 2
```

Shunpo owns only the leading label. Manual window names are preserved across
navigation, attachment, compaction, and removal. `--label-windows` repairs every
eligible window in the current session; linked windows are skipped because a
single name cannot represent indexes from multiple sessions. Applying a label
uses `rename-window`, which disables tmux `automatic-rename` for that window.
Shunpo installs no hooks; rerun `--label-windows` after creating or moving normal
windows when you want to refresh the status bar.

The `--tools` overview separates logical slot, live window, and configured source.
A configured persistent tool without a live window is shown as `@key —`; raw
commands are shown literally in `SOURCE`, while templates show their resolution.
Use the popup's **Label windows** action to refresh the current session without
leaving the editor.

---

## ⌨️ Recommended Keybindings

Add to `~/.tmux.conf`:

```tmux
# Session navigation
bind-key "T" display-popup -E -w 80% -h 70% -d '#{pane_current_path}' -T 'Sesh' tv sesh
bind-key "L" run-shell "sesh last"                  # Previous session

# Marks — Alt+number for instant jump
bind-key -n M-1 run-shell "tmux-shunpo --goto 1"
bind-key -n M-2 run-shell "tmux-shunpo --goto 2"
bind-key -n M-3 run-shell "tmux-shunpo --goto 3"
bind-key -n M-4 run-shell "tmux-shunpo --goto 4"
bind-key -n M-5 run-shell "tmux-shunpo --goto 5"
# … repeat through M-9

# Mark management
bind-key "m" run-shell "tmux-shunpo --marks"        # Interactive editor
bind-key "M" run-shell "tmux-shunpo --add-mark"     # Quick-add current session

# Tools — prefix + letter for per-session windows
bind-key "u" run-shell "tmux-shunpo --goto @1"      # Tool 1
bind-key "i" run-shell "tmux-shunpo --goto @2"      # Tool 2
bind-key "o" run-shell "tmux-shunpo --goto @3"      # Tool 3
bind-key "p" run-shell "tmux-shunpo --goto @4"      # Tool 4
# … bind more keys through @9 as needed
bind-key "a" run-shell "tmux-shunpo --add-tool"     # Add current window to tools
bind-key "E" run-shell "tmux-shunpo --tools"        # Interactive tool editor
bind-key "R" run-shell "tmux-shunpo --label-windows" # Refresh @key/#index labels

# Session setup
bind-key "B" run-shell "tmux-shunpo --bootstrap"    # Bootstrap from preset
```

When called from a keybinding (no TTY), `--marks` and `--tools` automatically run inside a tmux popup so everything stays keyboard-driven.

---

## 📖 CLI Reference

```
Usage: tmux-shunpo [OPTION]

Session Navigation:
  --goto N               Jump to mark slot N (1–9)
  --goto @N              Navigate to tool window slot N (1–9)
  --connect <session>    Connect to session by name (saves/restores window state)

Mark Management:
  --add-mark             Mark current session (tmux) or directory (outside tmux)
  --remove N             Remove mark slot N
  --remove all           Remove all marks
  --marks                Interactive mark editor (gum TUI, popup from keybinding)
  --compact-marks        Compact mark slots (remove gaps)

Tool Management:
  --tools                Interactive tool editor for current session (gum TUI, popup)
  --add-tool             Append current window to next empty tool slot as @attached
  --compact-tools        Compact tool slots for current session (remove gaps)
  --bootstrap [preset]   Rebuild a session's tool windows from [preset] or an
                         interactive list (closes and recreates the tool windows;
                         confirms first if a preset contains destructive commands)

Maintenance:
  --label-windows        Label current-session windows with @key or #index
  --reset [session|all]  Reset tools for current session, or all data
  doctor, --doctor       Diagnose dependencies and config parsing

Info:
  -h, --help             Show usage
  -v, --version          Show version
  --completion [shell]   Print shell completion script (bash, zsh, fish)
```

### Marks vs Tools

**Marks** are global bookmarks — they live across all sessions. A mark stores a session name (when set from inside tmux) or a directory path (when set from outside tmux).

**Tools** are per-session window slots. Each session has its own tool configuration. Slot `@1` in session `alpha` can be `nvim .` while slot `@1` in session `beta` can be `cargo watch`.

---

## 📄 License

MIT
