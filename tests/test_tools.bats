#!/usr/bin/env bats

setup() {
    load 'test_helper/common-setup'
    _common_setup
    TEST_TEMP_DIR="$(mktemp -d)"
    MOCK_BIN="${TEST_TEMP_DIR}/bin"
    mkdir -p "${MOCK_BIN}"
    export PATH="${MOCK_BIN}:${PATH}"
    _mock_tmux

    export CONFIG_DIR="${TEST_TEMP_DIR}/config"
    export CONFIG_FILE="${CONFIG_DIR}/config.toml"
    export DATA_DIR="${TEST_TEMP_DIR}/data"
    export MARKS_FILE="${DATA_DIR}/marks"
    export SESH_CONFIG="${TEST_TEMP_DIR}/sesh.toml"
    mkdir -p "${CONFIG_DIR}" "${DATA_DIR}" "${DATA_DIR}/tools"

    source "${PROJECT_ROOT}/tmux-shunpo"
}

teardown() {
    rm -rf "${TEST_TEMP_DIR}"
}

_mock_yq() {
    cat > "${MOCK_BIN}/yq" <<'MOCK'
#!/usr/bin/env bash
case "$*" in
    *".session[] | select(.name == \"test-project\") | .windows"*) echo '["editor","tests"]' ;;
    *".session[] | select(.name == \"test-project\") | .path"*) echo '~/Code/test' ;;
    *".wildcard"*) echo '2' ;;
    *".wildcard[0].pattern"*) echo '~/Code/*' ;;
    *".wildcard[0].windows"*) echo '["editor","devserver","shell"]' ;;
    *".wildcard[1].pattern"*) echo '' ;;
    *".default_session.windows"*) echo '["editor","shell"]' ;;
    *".window[] | select(.name == \"editor\") | .startup_script"*) echo 'nvim .' ;;
    *"-o tsv .[]"*) echo -e "editor\nshell" ;;
    *) echo "" ;;
esac
MOCK
    chmod +x "${MOCK_BIN}/yq"
}

@test "get_tool returns name:::command for filled slot" {
    tool_set "test-session" 1 "nvim ."
    run get_tool "test-session" 1
    assert_output --partial ":::nvim ."
}

@test "get_tool returns 1 for empty slot" {
    run get_tool "test-session" 1
    assert_failure
}

@test "get_tool returns 1 for missing tools file" {
    run get_tool "nonexistent-session" 1
    assert_failure
}

@test "get_tool preserves @template reference" {
    cp "${PROJECT_ROOT}/tests/fixtures/sesh_config.toml" "${SESH_CONFIG}"
    tool_set "test-session" 1 "@editor"
    run get_tool "test-session" 1
    assert_output --partial ":::@editor"
}

@test "tool_set writes to tools file" {
    tool_set "test-session" 1 "nvim ."
    assert_file_exist "${DATA_DIR}/tools/test-session"
}

@test "tool_set creates tools file if missing" {
    tool_set "new-session" 2 "cargo test"
    assert_file_exist "${DATA_DIR}/tools/new-session"
}

@test "tool_remove clears slot" {
    tool_set "test-session" 1 "nvim ."
    tool_remove "test-session" 1
    run get_tool "test-session" 1
    assert_failure
}

@test "generate_window_name uses first word of command" {
    run generate_window_name "nvim ."
    assert_output "nvim"
}

@test "generate_window_name truncates long names" {
    CFG_WINDOW_NAME_MAX_LENGTH=5
    run generate_window_name "verylongcommand"
    assert_output "veryl"
}

@test "generate_window_name uses custom name when provided" {
    run generate_window_name "some command" "my-window"
    assert_output "my-window"
}

@test "tool_auto_populate from default_session fallback" {
    _mock_yq
    {
        echo '[default_session]'
        echo 'windows = ["editor", "shell"]'
    } > "${SESH_CONFIG}"

    run tool_auto_populate "unknown-session"
    assert_file_exist "${DATA_DIR}/tools/unknown-session"
    run grep "1: @editor" "${DATA_DIR}/tools/unknown-session"
    assert_success
}

@test "tool_auto_populate skips when tools file exists" {
    tool_set "test-session" 1 "custom command"
    cp "${PROJECT_ROOT}/tests/fixtures/sesh_config.toml" "${SESH_CONFIG}"
    tool_auto_populate "test-session"
    run get_tool "test-session" 1
    assert_output --partial "custom command"
}

@test "tool_auto_populate caps at 9 slots" {
    cat > "${MOCK_BIN}/yq" <<'MOCK'
#!/usr/bin/env bash
case "$*" in
    *".default_session.windows"*) echo '["a","b","c","d","e","f","g","h","i","j"]' ;;
    *"-o tsv .[]"*) echo -e "a\nb\nc\nd\ne\nf\ng\nh\ni\nj" ;;
    *) echo "" ;;
esac
MOCK
    chmod +x "${MOCK_BIN}/yq"

    touch "${SESH_CONFIG}"

    tool_auto_populate "many-windows"
    local lines
    lines=$(grep -c '^[0-9]:' "${DATA_DIR}/tools/many-windows")
    [[ "${lines}" -eq 9 ]]
}

@test "tool_auto_populate validates window names from sesh.toml" {
    # Mock yq to return a malicious window name
    cat > "${MOCK_BIN}/yq" <<'MOCK'
#!/usr/bin/env bash
case "$*" in
    *".default_session.windows"*) echo '["valid","evil;rm","ok"]' ;;
    *"-o tsv .[]"*) printf 'valid\nevil;rm\nok\n' ;;
    *) echo "" ;;
esac
MOCK
    chmod +x "${MOCK_BIN}/yq"
    touch "${SESH_CONFIG}"

    tool_auto_populate "test-session"
    # evil;rm should be skipped (fails validate_template_name)
    run grep 'evil' "${DATA_DIR}/tools/test-session"
    assert_failure
    # valid and ok should be present
    run grep '@valid' "${DATA_DIR}/tools/test-session"
    assert_success
    run grep '@ok' "${DATA_DIR}/tools/test-session"
    assert_success
}

@test "tool_auto_populate uses atomic write" {
    cat > "${MOCK_BIN}/yq" <<'MOCK'
#!/usr/bin/env bash
case "$*" in
    *".default_session.windows"*) echo '["editor"]' ;;
    *"-o tsv .[]"*) echo 'editor' ;;
    *) echo "" ;;
esac
MOCK
    chmod +x "${MOCK_BIN}/yq"
    touch "${SESH_CONFIG}"

    tool_auto_populate "atomic-test"
    local tmp_files
    tmp_files=$(find "${DATA_DIR}" -name '*.tmp.*' 2>/dev/null | wc -l | tr -d ' ')
    [[ "${tmp_files}" -eq 0 ]]
}

@test "tool_set uses atomic write" {
    tool_set "test-session" 1 "nvim ."
    local tmp_files
    tmp_files=$(find "${DATA_DIR}" -name '*.tmp.*' 2>/dev/null | wc -l | tr -d ' ')
    [[ "${tmp_files}" -eq 0 ]]
}
