# ⚡ tmux-shunpo

Harpoon2-style navigation for tmux, revamped for speed and simplicity. 

`tmux-shunpo` (named after Bleach's "flash step") provides zero-thought, muscle-memory navigation for your terminal. It slims down your workflow by delegating session management to [sesh](https://github.com/joshmedeski/sesh) while providing persistent bookmarks and per-session tool windows.

## ✨ Features

- **Marks (1-9)**: Instant jump to bookmarked sessions or directories.
- **Tools (@1-@9)**: Persistent window slots for each session (e.g., `@1` is always your editor, `@2` is always your server).
- **sesh Integration**: Full delegation of session creation, discovery (zoxide), and previews to `sesh`.
- **TUI Editors**: Sleek interactive editors powered by [gum](https://github.com/charmbracelet/gum).
- **Auto-Populate**: Automatically configures tools for new sessions based on your `sesh.toml` definitions.
- **Security First**: Strict command validation, atomic file writes, and no `eval`.

## 📋 Prerequisites

- **bash** >= 4.0
- **tmux** >= 3.2
- **[sesh](https://github.com/joshmedeski/sesh)**: Session management backend.
- **[yq](https://github.com/mikefarah/yq)**: TOML processing (Go version).
- **[gum](https://github.com/charmbracelet/gum)**: Interactive TUI components.
- **[fzf](https://github.com/junegunn/fzf)** or **[sk](https://github.com/lotabout/skim)**: Fuzzy finder for search.

## 🚀 Installation

Clone the repository and run the install script:

```bash
git clone https://github.com/zzdif/tmux-shunpo.git
cd tmux-shunpo
./install.sh
```

### Custom Prefix
To install to a custom directory (e.g., `~/bin`):
```bash
./install.sh --prefix ~/bin
```

## ⌨️ Recommended Keybindings

Add these to your `~/.tmux.conf` for the best experience:

```tmux
# Session navigation
bind-key "T" run-shell "tmux-shunpo --search"       # Fuzzy search all sessions
bind-key "L" run-shell "sesh last"                   # Switch to last session

# Marks (Alt+number for instant jump)
bind-key -n M-1 run-shell "tmux-shunpo --goto 1"
bind-key -n M-2 run-shell "tmux-shunpo --goto 2"
bind-key -n M-3 run-shell "tmux-shunpo --goto 3"
bind-key -n M-4 run-shell "tmux-shunpo --goto 4"
bind-key "m"   run-shell "tmux-shunpo --marks"       # Mark editor
bind-key "M"   run-shell "tmux-shunpo --add"         # Quick-mark current session

# Tools (prefix + key)
bind-key "u" run-shell "tmux-shunpo --goto @1"       # Tool 1
bind-key "i" run-shell "tmux-shunpo --goto @2"       # Tool 2
bind-key "E" run-shell "tmux-shunpo --tools"         # Tool editor
```

## ⚙️ Configuration

### `tmux-shunpo` Settings
Located at `~/.config/tmux-shunpo/config.toml`. You can configure window offsets and popup dimensions:

```toml
tool_window_base = 88

[ui]
popup_width = "80%"
popup_height = "70%"
```

### Tool Templates
`tmux-shunpo` reads `[[window]]` definitions from your `sesh.toml` (`~/.config/sesh/sesh.toml`). If you define a window named `editor`, you can set a tool to `@editor` to automatically use that configuration.

## 🛠️ Usage

| Command | Description |
| :--- | :--- |
| `--search` | Fuzzy search sessions via `sesh` + `fzf/sk`. |
| `--goto N` | Jump to mark slot `N` (1-9). |
| `--goto @N` | Navigate to tool window slot `N` (1-9). |
| `--add` | Mark the current session (inside tmux) or directory. |
| `--marks` | Open the interactive mark editor. |
| `--tools` | Open the interactive tool editor for the current session. |
| `--reset session` | Reset tools for the current session. |
| `--reset all` | Clear all marks and session tools. |

## 📄 License

MIT
