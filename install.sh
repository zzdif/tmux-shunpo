#!/usr/bin/env bash

# =============================================================================
# tmux-shunpo — Install/Uninstall Script
# =============================================================================

set -euo pipefail

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Default installation directory
INSTALL_DIR="${HOME}/.local/bin"
CONFIG_DIR="${HOME}/.config/tmux-shunpo"
MODE="install"

show_help() {
    cat <<EOF
Usage: ./install.sh [OPTION]

Options:
  -p, --prefix DIR    Set installation directory (default: ${HOME}/.local/bin)
  --uninstall         Remove tmux-shunpo and optionally its configuration
  -h, --help          Show this help message
EOF
}

# Parse arguments
while [[ $# -gt 0 ]]; do
    case "$1" in
        -p|--prefix)
            if [[ -z "${2:-}" ]]; then
                echo -e "${RED}Error: --prefix requires a directory argument${NC}"
                exit 1
            fi
            INSTALL_DIR="$2"
            shift 2
            ;;
        --uninstall)
            MODE="uninstall"
            shift
            ;;
        -h|--help)
            show_help
            exit 0
            ;;
        *)
            echo -e "${RED}Error: Unknown option $1${NC}"
            show_help
            exit 1
            ;;
    esac
done

if [[ "${MODE}" == "uninstall" ]]; then
    echo -e "${YELLOW}⚡ Uninstalling tmux-shunpo...${NC}"
    
    # Remove binary
    if [[ -f "${INSTALL_DIR}/tmux-shunpo" ]]; then
        rm "${INSTALL_DIR}/tmux-shunpo"
        echo -e "${GREEN}✓ Removed ${INSTALL_DIR}/tmux-shunpo${NC}"
    else
        echo -e "${YELLOW}! Binary not found at ${INSTALL_DIR}/tmux-shunpo${NC}"
    fi

    # Optional config removal
    if [[ -d "${CONFIG_DIR}" ]]; then
        echo -e "\n${YELLOW}Found configuration directory at ${CONFIG_DIR}${NC}"
        echo -n "Do you want to remove it and all saved data? (y/N): "
        read -r REPLY
        if [[ ${REPLY} =~ ^[Yy]$ ]]; then
            rm -rf "${CONFIG_DIR}"
            # Also remove data dir if it's the default one
            DATA_DIR="${HOME}/.local/share/tmux-shunpo"
            if [[ -d "${DATA_DIR}" ]]; then
                rm -rf "${DATA_DIR}"
            fi
            echo -e "${GREEN}✓ Removed configuration and data directories${NC}"
        else
            echo -e "${YELLOW}! Configuration and data directories preserved${NC}"
        fi
    fi

    echo -e "\n${GREEN}✨ Uninstallation complete!${NC}"
    exit 0
fi

echo -e "${GREEN}⚡ Installing tmux-shunpo...${NC}"

# 1. Dependency Checks
echo -e "\n${YELLOW}Checking dependencies...${NC}"
MISSING_DEPS=()

check_dep() {
    if ! command -v "$1" &> /dev/null; then
        echo -e "${RED}✗ $1 is missing${NC}"
        MISSING_DEPS+=("$1")
    else
        echo -e "${GREEN}✓ $1 is installed${NC}"
    fi
}

check_dep "bash"
check_dep "tmux"
check_dep "sesh"
check_dep "yq"
check_dep "gum"

# fzf or sk
if ! command -v fzf &> /dev/null && ! command -v sk &> /dev/null; then
    echo -e "${RED}✗ fzf or sk is missing (need at least one)${NC}"
    MISSING_DEPS+=("fzf or sk")
else
    echo -e "${GREEN}✓ fuzzy finder found${NC}"
fi

if [[ ${#MISSING_DEPS[@]} -gt 0 ]]; then
    echo -e "\n${RED}Error: Missing required dependencies: ${MISSING_DEPS[*]}${NC}"
    echo "Please install them before continuing."
    exit 1
fi

# 2. Installation
echo -e "\n${YELLOW}Installing binary...${NC}"
mkdir -p "${INSTALL_DIR}"

if [[ -f "tmux-shunpo" ]]; then
    cp tmux-shunpo "${INSTALL_DIR}/tmux-shunpo"
    chmod +x "${INSTALL_DIR}/tmux-shunpo"
    echo -e "${GREEN}✓ Installed to ${INSTALL_DIR}/tmux-shunpo${NC}"
else
    echo -e "${RED}Error: tmux-shunpo source file not found in current directory.${NC}"
    exit 1
fi

# 3. Configuration Setup
echo -e "\n${YELLOW}Setting up configuration...${NC}"
mkdir -p "${CONFIG_DIR}"

if [[ -f "config.toml.example" ]]; then
    if [[ ! -f "${CONFIG_DIR}/config.toml" ]]; then
        cp config.toml.example "${CONFIG_DIR}/config.toml"
        echo -e "${GREEN}✓ Created default config at ${CONFIG_DIR}/config.toml${NC}"
    else
        echo -e "${YELLOW}! Config already exists at ${CONFIG_DIR}/config.toml (skipping)${NC}"
    fi
fi

# 4. Final Instructions
echo -e "\n${GREEN}✨ Installation complete!${NC}"
echo -e "Make sure ${YELLOW}${INSTALL_DIR}${NC} is in your ${YELLOW}\$PATH${NC}."
echo -e "\nAdd these recommended bindings to your ${YELLOW}~/.tmux.conf${NC}:"
cat <<EOF
# Session navigation
bind-key "T" run-shell "tmux-shunpo --search"
bind-key "L" run-shell "sesh last"

# Marks (Alt+number for instant jump)
bind-key -n M-1 run-shell "tmux-shunpo --goto 1"
bind-key -n M-2 run-shell "tmux-shunpo --goto 2"
bind-key -n M-3 run-shell "tmux-shunpo --goto 3"
bind-key -n M-4 run-shell "tmux-shunpo --goto 4"
bind-key "m"   run-shell "tmux-shunpo --marks"
bind-key "M"   run-shell "tmux-shunpo --add"

# Tools (prefix + key)
bind-key "u" run-shell "tmux-shunpo --goto @1"
bind-key "i" run-shell "tmux-shunpo --goto @2"
bind-key "E" run-shell "tmux-shunpo --tools"
EOF
