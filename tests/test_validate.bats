#!/usr/bin/env bats

setup() {
    load 'test_helper/common-setup'
    _common_setup
    TEST_TEMP_DIR="$(mktemp -d)"
    source "${PROJECT_ROOT}/tmux-shunpo.sh"
}

teardown() {
    rm -rf "${TEST_TEMP_DIR}"
}

@test "validate_command accepts simple commands" {
    run validate_command "nvim ."
    assert_success
    run validate_command "cargo test"
    assert_success
}

@test "validate_command accepts paths with slashes" {
    run validate_command "/usr/bin/thing"
    assert_success
}

@test "validate_command accepts env vars in command" {
    run validate_command "PORT=3000 npm start"
    assert_success
}

@test "validate_command rejects semicolons" {
    run validate_command "; rm -rf /"
    assert_failure
}

@test "validate_command rejects pipes" {
    run validate_command "cmd | evil"
    assert_failure
}

@test "validate_command rejects command substitution" {
    run validate_command '$(whoami)'
    assert_failure
}

@test "validate_command rejects backticks" {
    run validate_command '`id`'
    assert_failure
}

@test "validate_command rejects ampersands" {
    run validate_command "&& curl evil"
    assert_failure
}

@test "validate_command rejects directory traversal" {
    run validate_command "../../etc/passwd"
    assert_failure
}

@test "validate_command rejects empty command" {
    run validate_command ""
    assert_failure
}

@test "validate_range accepts in-range value" {
    run validate_range 5 1 9 "out of range"
    assert_success
}

@test "validate_range rejects below minimum" {
    run validate_range 0 1 9 "out of range"
    assert_failure
}

@test "validate_range rejects above maximum" {
    run validate_range 10 1 9 "out of range"
    assert_failure
}

@test "validate_range rejects non-numeric" {
    run validate_range "abc" 1 9 "out of range"
    assert_failure
}

@test "validate_session_name accepts valid names" {
    run validate_session_name "my-project"
    assert_success
    run validate_session_name "project_2"
    assert_success
}

@test "validate_session_name rejects path traversal" {
    run validate_session_name "../../etc"
    assert_failure
    run validate_session_name "/absolute"
    assert_failure
}

@test "validate_session_name rejects invalid characters" {
    run validate_session_name "project;rm"
    assert_failure
    run validate_session_name 'project$(id)'
    assert_failure
}

@test "validate_template_name accepts valid names" {
    run validate_template_name "editor"
    assert_success
    run validate_template_name "dev-server"
    assert_success
}

@test "validate_template_name rejects invalid characters" {
    run validate_template_name 'editor;rm'
    assert_failure
}
