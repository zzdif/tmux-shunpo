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

@test "--help prints usage" {
    run main --help
    assert_success
    assert_output --partial "--search"
    assert_output --partial "--goto"
    assert_output --partial "--marks"
}

@test "--version prints version" {
    run main --version
    assert_success
    assert_output --partial "0.1.0"
}

@test "--goto without argument shows error" {
    run main --goto
    assert_failure
    assert_output --partial "requires a target"
}

@test "unknown flag shows error" {
    run main --unknown
    assert_failure
    assert_output --partial "Unknown option"
}

@test "no arguments shows usage" {
    run main
    assert_success
    assert_output --partial "Usage"
}
