.PHONY: all build test install uninstall clean

# Installation prefix
PREFIX ?= $(HOME)/.local
BINDIR = $(PREFIX)/bin
CONFIG_DIR = $(HOME)/.config/tmux-shunpo
DATA_DIR = $(HOME)/.local/share/tmux-shunpo

# Completion directories
BASH_COMP_DIR = $(PREFIX)/share/bash-completion/completions
ZSH_COMP_DIR = $(PREFIX)/share/zsh/site-functions
FISH_COMP_DIR = $(HOME)/.config/fish/completions

all: build

build:
	@echo "Building tmux-shunpo..."
	go build -ldflags="-s -w" -o bin/tmux-shunpo .

test:
	@echo "Running tests..."
	go test -v ./...

install: build
	@echo "Installing binary to $(BINDIR)..."
	mkdir -p $(BINDIR)
	cp bin/tmux-shunpo $(BINDIR)/tmux-shunpo
	chmod +x $(BINDIR)/tmux-shunpo

	@echo "Setting up configuration in $(CONFIG_DIR)..."
	mkdir -p $(CONFIG_DIR)
	@if [ ! -f $(CONFIG_DIR)/config.toml ]; then \
		cp config.toml.example $(CONFIG_DIR)/config.toml; \
		echo "Created default config: $(CONFIG_DIR)/config.toml"; \
	else \
		echo "Config already exists: $(CONFIG_DIR)/config.toml"; \
	fi

	@echo "Setting up data directory in $(DATA_DIR)..."
	mkdir -p $(DATA_DIR)/tools

	@echo "Installing shell completions..."
	@# Bash completion
	@mkdir -p $(BASH_COMP_DIR)
	@bin/tmux-shunpo --completion bash > $(BASH_COMP_DIR)/tmux-shunpo
	@echo "Bash completion installed: $(BASH_COMP_DIR)/tmux-shunpo"
	@# Zsh completion
	@mkdir -p $(ZSH_COMP_DIR)
	@bin/tmux-shunpo --completion zsh > $(ZSH_COMP_DIR)/_tmux-shunpo
	@echo "Zsh completion installed: $(ZSH_COMP_DIR)/_tmux-shunpo"
	@# Fish completion
	@mkdir -p $(FISH_COMP_DIR)
	@bin/tmux-shunpo --completion fish > $(FISH_COMP_DIR)/tmux-shunpo.fish
	@echo "Fish completion installed: $(FISH_COMP_DIR)/tmux-shunpo.fish"

uninstall:
	@echo "Removing binary..."
	rm -f $(BINDIR)/tmux-shunpo
	@echo "Removing completions..."
	rm -f $(BASH_COMP_DIR)/tmux-shunpo
	rm -f $(ZSH_COMP_DIR)/_tmux-shunpo
	rm -f $(FISH_COMP_DIR)/tmux-shunpo.fish
	@echo "Checking config directory..."
	@if [ -L "$(CONFIG_DIR)" ]; then \
		echo "Skipping $(CONFIG_DIR) (symlink to $$(readlink "$(CONFIG_DIR)"))"; \
		echo "Config directory is managed externally (nix, stow, chezmoi, etc.)"; \
	elif [ -d "$(CONFIG_DIR)" ]; then \
		symlinks=$$(find "$(CONFIG_DIR)" -maxdepth 1 -type l 2>/dev/null); \
		if [ -n "$$symlinks" ]; then \
			echo "Skipping $(CONFIG_DIR) — contains externally managed files:"; \
			for f in $$symlinks; do \
				echo "  $$f -> $$(readlink "$$f")"; \
			done; \
			echo "Remove them manually if you want to purge these."; \
		else \
			rm -rf "$(CONFIG_DIR)"; \
			echo "Removed $(CONFIG_DIR)"; \
		fi; \
	fi
	@echo "Checking data directory..."
	@if [ -L "$(DATA_DIR)" ]; then \
		echo "Skipping $(DATA_DIR) (symlink to $$(readlink "$(DATA_DIR)"))"; \
		echo "Data directory is managed externally"; \
	elif [ -d "$(DATA_DIR)" ]; then \
		symlinks=$$(find "$(DATA_DIR)" -maxdepth 1 -type l 2>/dev/null); \
		if [ -n "$$symlinks" ]; then \
			echo "Skipping $(DATA_DIR) — contains externally managed files:"; \
			for f in $$symlinks; do \
				echo "  $$f -> $$(readlink "$$f")"; \
			done; \
			echo "Remove them manually if you want to purge these."; \
		else \
			rm -rf "$(DATA_DIR)"; \
			echo "Removed $(DATA_DIR)"; \
		fi; \
	fi
	@echo "Uninstall completed."

clean:
	@echo "Cleaning up..."
	rm -rf bin/
	rm -f tmux-shunpo
