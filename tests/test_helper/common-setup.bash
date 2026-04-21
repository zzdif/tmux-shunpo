_common_setup() {
    load 'test_helper/bats-support/load'
    load 'test_helper/bats-assert/load'
    load 'test_helper/bats-file/load'

    PROJECT_ROOT="$(cd "$(dirname "${BATS_TEST_FILENAME}")/.." && pwd)"

    unset TMUX TMUX_PANE
}

_mock_tmux() {
    cat > "${MOCK_BIN}/tmux" <<'MOCK'
#!/usr/bin/env bash
case "$1" in
    display-message) echo "tmux: $*" ;;
    has-session) exit 1 ;;
    new-session) exit 0 ;;
    switch-client) exit 0 ;;
    attach-session) exit 0 ;;
    display-popup|popup) exit 0 ;;
    list-windows) exit 0 ;;
    new-window) exit 0 ;;
    select-window) exit 0 ;;
    kill-window) exit 0 ;;
    send-keys) exit 0 ;;
    show-option) echo "bash" ;;
    *) exit 0 ;;
esac
MOCK
    chmod +x "${MOCK_BIN}/tmux"
}
