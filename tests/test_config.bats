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

    source "${PROJECT_ROOT}/tmux-shunpo.sh"
}

teardown() {
    rm -rf "${TEST_TEMP_DIR}"
}

_mock_yq() {
    cat > "${MOCK_BIN}/yq" <<'MOCK'
#!/usr/bin/env bash
case "$*" in
    *".tool_window_base // 88"*) echo "77" ;;
    *".shell_init_delay // 0.2"*) echo "0.5" ;;
    *".window_name_max_length // 20"*) echo "15" ;;
    *".ui.popup_width"*) echo "70%" ;;
    *".ui.popup_height"*) echo "60%" ;;
    *".finder.height_percent"*) echo "40" ;;
    *".finder.preview_percent"*) echo "45" ;;
    *) echo "" ;;
esac
MOCK
    chmod +x "${MOCK_BIN}/yq"
}

@test "cfg_load with no config file sets defaults" {
    cfg_load
    [[ "${CFG_TOOL_WINDOW_BASE}" == "88" ]]
    [[ "${CFG_SHELL_INIT_DELAY}" == "0.2" ]]
}

@test "cfg_load reads valid config.toml" {
    _mock_yq
    touch "${CONFIG_FILE}"
    cfg_load
    [[ "${CFG_TOOL_WINDOW_BASE}" == "77" ]]
    [[ "${CFG_SHELL_INIT_DELAY}" == "0.5" ]]
    [[ "${CFG_WINDOW_NAME_MAX_LENGTH}" == "15" ]]
}

@test "sesh_resolve_window_template resolves known template" {
    cp "${PROJECT_ROOT}/tests/fixtures/sesh_config.toml" "${SESH_CONFIG}"
    run sesh_resolve_window_template "editor"
    assert_output "nvim ."
}

@test "sesh_resolve_window_template returns empty for unknown template" {
    cp "${PROJECT_ROOT}/tests/fixtures/sesh_config.toml" "${SESH_CONFIG}"
    run sesh_resolve_window_template "nonexistent"
    assert_output ""
}

@test "config_load_tool_templates reads sesh.toml windows" {
    cp "${PROJECT_ROOT}/tests/fixtures/sesh_config.toml" "${SESH_CONFIG}"
    run config_load_tool_templates
    assert_output --partial "editor:::nvim ."
    assert_output --partial "tests:::bats tests/"
}

@test "config_load_tool_templates with no sesh.toml returns empty" {
    run config_load_tool_templates
    assert_output ""
}

@test "cfg_check_file_safety rejects world-writable config" {
    local bad_config="${TEST_TEMP_DIR}/world-writable.toml"
    echo 'tool_window_base = 88' > "${bad_config}"
    chmod 666 "${bad_config}"
    run cfg_check_file_safety "${bad_config}"
    assert_failure
    assert_output --partial "world-writable"
}

@test "cfg_check_file_safety accepts normal permissions" {
    local good_config="${TEST_TEMP_DIR}/normal.toml"
    echo 'tool_window_base = 88' > "${good_config}"
    chmod 644 "${good_config}"
    run cfg_check_file_safety "${good_config}"
    assert_success
}

@test "cfg_check_file_safety accepts missing file" {
    run cfg_check_file_safety "${TEST_TEMP_DIR}/nonexistent.toml"
    assert_success
}
