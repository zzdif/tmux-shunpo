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

@test "parse_marks returns slot:value pairs" {
    printf '%s\n' \
        "# Marks" \
        "1: ~/Code/aggen" \
        "2: dotfiles" > "${MARKS_FILE}"

    run parse_marks
    assert_output --partial "1:~/Code/aggen"
    assert_output --partial "2:dotfiles"
}

@test "parse_marks with specific slot returns value" {
    printf '%s\n' \
        "# Marks" \
        "1: ~/Code/aggen" > "${MARKS_FILE}"

    run parse_marks 1
    assert_output "~/Code/aggen"
}

@test "parse_marks with empty file returns nothing" {
    touch "${MARKS_FILE}"
    run parse_marks
    assert_output ""
}

@test "mark_set writes slot to marks file" {
    mark_set 1 "test-session"
    run parse_marks 1
    assert_output "test-session"
}

@test "mark_set overwrites existing slot" {
    mark_set 1 "old-session"
    mark_set 1 "new-session"
    run parse_marks 1
    assert_output "new-session"
}

@test "mark_add stores current directory when outside tmux" {
    unset TMUX
    local before
    before=$(pwd)
    mark_add
    run parse_marks 1
    assert_output "${before}"
}

@test "mark_add finds next empty slot" {
    mark_set 1 "session-a"
    mark_set 3 "session-b"
    mark_add
    run parse_marks 2
    assert_output "$(pwd)"
}

@test "mark_add fails when all 9 slots full" {
    local i
    for i in $(seq 1 9); do
        mark_set "${i}" "session-${i}"
    done
    run mark_add
    assert_failure
    assert_output --partial "full"
}

@test "mark_remove deletes slot" {
    mark_set 1 "session-a"
    mark_remove 1
    run parse_marks 1
    assert_failure
}

@test "mark_remove nonexistent slot fails" {
    run mark_remove 99
    assert_failure
}

@test "mark_rearrange compacts gaps" {
    mark_set 1 "a"
    mark_set 3 "b"
    mark_set 7 "c"
    mark_rearrange

    run parse_marks 1
    assert_output "a"
    run parse_marks 2
    assert_output "b"
    run parse_marks 3
    assert_output "c"
}

@test "mark_remove rearranges after removal" {
    mark_set 1 "a"
    mark_set 2 "b"
    mark_set 3 "c"
    mark_remove 2

    run parse_marks 1
    assert_output "a"
    run parse_marks 2
    assert_output "c"
}

@test "mark_set uses atomic write (no tmp files remain)" {
    mark_set 1 "test-session"
    local tmp_files
    tmp_files=$(find "${DATA_DIR}" -name '*.tmp.*' 2>/dev/null | wc -l | tr -d ' ')
    [[ "${tmp_files}" -eq 0 ]]
}
