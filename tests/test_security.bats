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

@test "validate_command rejects all adversarial inputs" {
    while IFS= read -r payload; do
        [[ -z "${payload}" || "${payload}" == \#* ]] && continue
        # -rf / is valid from whitelist perspective (no shell metacharacters)
        [[ "${payload}" == "-rf /" ]] && continue
        run validate_command "${payload}"
        assert_failure
    done < "${PROJECT_ROOT}/tests/fixtures/adversarial_inputs.txt"
}

@test "validate_session_name rejects all adversarial inputs" {
    while IFS= read -r payload; do
        [[ -z "${payload}" || "${payload}" == \#* ]] && continue
        run validate_session_name "${payload}"
        assert_failure
    done < "${PROJECT_ROOT}/tests/fixtures/adversarial_inputs.txt"
}

@test "validate_template_name rejects all adversarial inputs" {
    while IFS= read -r payload; do
        [[ -z "${payload}" || "${payload}" == \#* ]] && continue
        run validate_template_name "${payload}"
        assert_failure
    done < "${PROJECT_ROOT}/tests/fixtures/adversarial_inputs.txt"
}

@test "validate_command does not execute embedded commands" {
    local canary="${TEST_TEMP_DIR}/pwned"

    run validate_command "; touch ${canary}"
    assert_failure
    assert_file_not_exist "${canary}"

    run validate_command '\$(touch "${canary}")'
    assert_failure
    assert_file_not_exist "${canary}"

    run validate_command '\`touch "${canary}"\`'
    assert_failure
    assert_file_not_exist "${canary}"
}

@test "mark_set does not execute injected session names" {
    local canary="${TEST_TEMP_DIR}/pwned"

    # mark_set now validates mark entries — these should be rejected
    run mark_set 1 "; touch ${canary}"
    assert_failure
    assert_file_not_exist "${canary}"

    run mark_set 1 '$(touch canary)'
    assert_failure
    assert_file_not_exist "${canary}"
}

@test "get_tool rejects path traversal in session name" {
    run get_tool "../../etc/passwd" 1
    assert_failure
}

@test "tool_set rejects invalid session names" {
    run tool_set "../../etc/passwd" 1 "nvim ."
    assert_failure
}

@test "mark_set rejects adversarial mark values" {
    run mark_set 1 '; rm -rf /'
    assert_failure
    run mark_set 1 '$(whoami)'
    assert_failure
    run mark_set 1 '| cat /etc/passwd'
    assert_failure
}

@test "sesh_resolve_window_template validates template name internally" {
    cp "${PROJECT_ROOT}/tests/fixtures/sesh_config.toml" "${SESH_CONFIG}"
    run sesh_resolve_window_template 'editor;rm'
    assert_failure
    run sesh_resolve_window_template '../etc'
    assert_failure
}

@test "tool_auto_populate validates session name internally" {
    cp "${PROJECT_ROOT}/tests/fixtures/sesh_config.toml" "${SESH_CONFIG}"
    run tool_auto_populate '../../etc/passwd'
    assert_failure
}

@test "atomic_write leaves no temp files on failure" {
    local target="${TEST_TEMP_DIR}/readonly/file.txt"
    mkdir -p "$(dirname "${target}")"
    chmod 000 "$(dirname "${target}")"

    run atomic_write "${target}" "content"
    assert_failure

    chmod 755 "$(dirname "${target}")"
    local tmp_files
    tmp_files=$(find "${TEST_TEMP_DIR}" -name '*.tmp.*' 2>/dev/null | wc -l | tr -d ' ')
    [[ "${tmp_files}" -eq 0 ]]
}
