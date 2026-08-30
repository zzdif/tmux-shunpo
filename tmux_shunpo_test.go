package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

type MockRunner struct {
	LookPathFunc       func(file string) (string, error)
	RunFunc            func(name string, args []string, stdin io.Reader) ([]byte, error)
	RunCombinedFunc    func(name string, args []string, stdin io.Reader) ([]byte, error)
	RunInteractiveFunc func(name string, args []string, stdin io.Reader, stdout io.Writer, stderr io.Writer) error
}

func (m *MockRunner) LookPath(file string) (string, error) {
	if m.LookPathFunc != nil {
		return m.LookPathFunc(file)
	}
	return file, nil
}

func (m *MockRunner) Run(name string, args []string, stdin io.Reader) ([]byte, error) {
	if m.RunFunc != nil {
		return m.RunFunc(name, args, stdin)
	}
	return nil, nil
}

func (m *MockRunner) RunCombined(name string, args []string, stdin io.Reader) ([]byte, error) {
	if m.RunCombinedFunc != nil {
		return m.RunCombinedFunc(name, args, stdin)
	}
	return nil, nil
}

func (m *MockRunner) RunInteractive(name string, args []string, stdin io.Reader, stdout io.Writer, stderr io.Writer) error {
	if m.RunInteractiveFunc != nil {
		return m.RunInteractiveFunc(name, args, stdin, stdout, stderr)
	}
	return nil
}

func setupTestPaths(t *testing.T) (Paths, string) {
	t.Helper()
	t.Setenv("TMUX", "")
	tmpDir, err := os.MkdirTemp("", "shunpo_test_*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	return Paths{
		ConfigDir:        filepath.Join(tmpDir, "config"),
		ConfigFile:       filepath.Join(tmpDir, "config", "config.toml"),
		DataDir:          filepath.Join(tmpDir, "data"),
		MarksFile:        filepath.Join(tmpDir, "data", "marks"),
		SessionStateFile: filepath.Join(tmpDir, "data", "session_state"),
		SeshConfigFile:   filepath.Join(tmpDir, "sesh.toml"),
	}, tmpDir
}

func TestValidateRange(t *testing.T) {
	tests := []struct {
		val, min, max int
		wantErr       bool
	}{
		{5, 1, 9, false},
		{1, 1, 9, false},
		{9, 1, 9, false},
		{0, 1, 9, true},
		{10, 1, 9, true},
	}
	for _, tc := range tests {
		err := validateRange(tc.val, tc.min, tc.max, "out of range")
		if (err != nil) != tc.wantErr {
			t.Errorf("validateRange(%d, %d, %d) error = %v, wantErr = %v", tc.val, tc.min, tc.max, err, tc.wantErr)
		}
	}
}

func TestValidateCommand(t *testing.T) {
	tests := []struct {
		cmd     string
		wantErr bool
	}{
		{"nvim .", false},
		{"cargo test", false},
		{"/usr/bin/thing", false},
		{"PORT=3000 npm start", false},
		{"; rm -rf /", false},
		{"cmd | evil", false},
		{"$(whoami)", false},
		{"`id`", false},
		{"&& curl evil", false},
		{"../../etc/passwd", true},
		{"", true},
		{"   ", true},
	}
	for _, tc := range tests {
		err := validateCommand(tc.cmd)
		if (err != nil) != tc.wantErr {
			t.Errorf("validateCommand(%q) error = %v, wantErr = %v", tc.cmd, err, tc.wantErr)
		}
	}
}

func TestIsDestructive(t *testing.T) {
	app := &App{
		Config: defaultConfig(),
	}

	tests := []struct {
		cmd  string
		want bool
	}{
		// Routine dev commands are no longer guarded.
		{"nvim .", false},
		{"cargo test | grep error", false},
		{"PORT=3000 npm start", false},
		{"mv file destination", false},
		{"cp file destination", false},
		{"sed 's/foo/bar/g' file", false},
		{"chmod +x script.sh", false},
		{"sudo systemctl restart x", false},
		// Unambiguously catastrophic commands are guarded (Linux).
		{"rm -rf /", true},
		{"PORT=3000 rm -rf /", true},
		{"cmd | rm -rf", true},
		{"dd if=/dev/zero of=/dev/null", true},
		{"mkfs.ext4 /dev/sdb1", true},
		{"shred -u file", true},
		{"parted /dev/sda", true},
		{"wipefs -a /dev/sda", true},
		{"shutdown -h now", true},
		{"reboot", true},
		// macOS destroyers.
		{"diskutil eraseDisk APFS Blank /dev/disk2", true},
		{"diskutil list", true},              // read-only, but guarded by name (one-time, cheap)
		{"newfs_apfs -v Data disk3s2", true}, // matched via the `_` separator
		{"asr restore --source x --target /Volumes/Y --erase", true},
		{"srm -rf secret", true},
	}
	for _, tc := range tests {
		if _, got := app.isDestructive(tc.cmd); got != tc.want {
			t.Errorf("isDestructive(%q) = %v, want %v", tc.cmd, got, tc.want)
		}
	}

	// also_confirm adds, skip_confirm removes.
	app.Config.Guardrails.AlsoConfirm = []string{"curl"}
	app.Config.Guardrails.SkipConfirm = []string{"rm"}

	if _, ok := app.isDestructive("curl google.com"); !ok {
		t.Error("expected curl to be guarded via also_confirm")
	}
	if _, ok := app.isDestructive("rm -rf /"); ok {
		t.Error("expected rm to be unguarded via skip_confirm")
	}

	// confirm_destructive = false disables the guardrail entirely.
	app.Config.Guardrails.ConfirmDestructive = false
	if _, ok := app.isDestructive("dd if=/dev/zero of=/dev/sda"); ok {
		t.Error("expected guardrail disabled when confirm_destructive is false")
	}
}

func TestValidateSessionName(t *testing.T) {
	tests := []struct {
		name    string
		wantErr bool
	}{
		{"my-project", false},
		{"my_project.1", false},
		{"../project", true},
		{"/project", true},
		{"project..dir", true},
		{"project$dir", true},
		{"", true},
	}
	for _, tc := range tests {
		err := validateSessionName(tc.name)
		if (err != nil) != tc.wantErr {
			t.Errorf("validateSessionName(%q) error = %v, wantErr = %v", tc.name, err, tc.wantErr)
		}
	}
}

func TestValidateTemplateName(t *testing.T) {
	tests := []struct {
		name    string
		wantErr bool
	}{
		{"editor", false},
		{"dev-server", false},
		{"dev_server", false},
		{"editor$1", true},
		{"", true},
	}
	for _, tc := range tests {
		err := validateTemplateName(tc.name)
		if (err != nil) != tc.wantErr {
			t.Errorf("validateTemplateName(%q) error = %v, wantErr = %v", tc.name, err, tc.wantErr)
		}
	}
}

func TestMarks(t *testing.T) {
	paths, tmpDir := setupTestPaths(t)
	defer os.RemoveAll(tmpDir)

	// Test empty
	marks, err := parseMarks(paths.MarksFile)
	if err != nil {
		t.Fatalf("parseMarks failed: %v", err)
	}
	if len(marks) != 0 {
		t.Errorf("expected 0 marks, got %v", marks)
	}

	// Test markSet
	err = markSet(paths.MarksFile, 1, "project-a")
	if err != nil {
		t.Fatalf("markSet failed: %v", err)
	}

	marks, err = parseMarks(paths.MarksFile)
	if err != nil {
		t.Fatalf("parseMarks failed: %v", err)
	}
	if marks[1] != "project-a" {
		t.Errorf("expected slot 1 to be project-a, got %q", marks[1])
	}

	// Test parseMarksSlot
	val, err := parseMarksSlot(paths.MarksFile, 1)
	if err != nil || val != "project-a" {
		t.Errorf("parseMarksSlot failed: val = %q, err = %v", val, err)
	}

	// Test markAdd
	slot, ref, err := markAdd(paths.MarksFile, false, "", "/path/to/my-project")
	if err != nil {
		t.Fatalf("markAdd failed: %v", err)
	}
	if slot != 2 {
		t.Errorf("expected new mark in slot 2, got %d", slot)
	}
	if !strings.HasSuffix(ref, "my-project") {
		t.Errorf("expected ref to end with my-project, got %q", ref)
	}

	// Test duplicate check
	_, _, err = markAdd(paths.MarksFile, false, "", "/path/to/my-project")
	if err == nil || err.Error() != "already marked" {
		t.Errorf("expected duplicate error, got %v", err)
	}

	// Test markRemove
	err = markRemove(paths.MarksFile, "1")
	if err != nil {
		t.Fatalf("markRemove failed: %v", err)
	}
	marks, _ = parseMarks(paths.MarksFile)
	if _, ok := marks[1]; ok {
		t.Errorf("slot 1 should be removed")
	}

	// Test markRearrange
	err = markRearrange(paths.MarksFile)
	if err != nil {
		t.Fatalf("markRearrange failed: %v", err)
	}
	marks, _ = parseMarks(paths.MarksFile)
	// Slot 2 should have been shifted to slot 1
	if _, ok := marks[1]; !ok {
		t.Errorf("slot 2 should have shifted to slot 1")
	}
}

func TestAddCurrentMarkInsideTmuxDetectsExistingPathMark(t *testing.T) {
	paths, tmpDir := setupTestPaths(t)
	defer os.RemoveAll(tmpDir)
	t.Setenv("TMUX", "1")

	sessionPath := filepath.Join(tmpDir, "tmux-shunpo")
	if err := os.MkdirAll(sessionPath, 0755); err != nil {
		t.Fatalf("failed to create session path: %v", err)
	}
	if _, _, err := markAdd(paths.MarksFile, false, "", sessionPath); err != nil {
		t.Fatalf("failed to seed path mark: %v", err)
	}

	cwd := filepath.Join(tmpDir, "elsewhere")
	if err := os.MkdirAll(cwd, 0755); err != nil {
		t.Fatalf("failed to create cwd: %v", err)
	}
	oldCwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get cwd: %v", err)
	}
	if err := os.Chdir(cwd); err != nil {
		t.Fatalf("failed to chdir: %v", err)
	}
	defer os.Chdir(oldCwd)

	app := &App{
		Paths: paths,
		Runner: &MockRunner{RunFunc: func(name string, args []string, stdin io.Reader) ([]byte, error) {
			if name == "tmux" && len(args) == 3 && args[0] == "display-message" && args[1] == "-p" {
				switch args[2] {
				case "#S":
					return []byte("tmux-shunpo\n"), nil
				case "#{session_path}":
					return []byte(sessionPath + "\n"), nil
				}
			}
			return []byte(""), nil
		}},
		Config: defaultConfig(),
		Stdout: io.Discard,
		Stderr: io.Discard,
	}

	slot, ref, err := app.markAddCurrent()
	if err == nil || err.Error() != "already marked" {
		t.Fatalf("expected existing path mark to be detected as duplicate, slot=%d ref=%q err=%v", slot, ref, err)
	}
	if slot != 1 {
		t.Fatalf("expected duplicate to point at existing slot 1, got %d", slot)
	}

	marks, err := parseMarks(paths.MarksFile)
	if err != nil {
		t.Fatalf("failed to parse marks: %v", err)
	}
	if len(marks) != 1 {
		t.Fatalf("duplicate session name should not create a second mark, got marks: %v", marks)
	}
	if marks[1] != normalizePath(sessionPath) {
		t.Fatalf("existing path mark should remain unchanged, got %q", marks[1])
	}
}

func TestAddCurrentMarkInsideTmuxDetectsExistingTildePathMark(t *testing.T) {
	paths, tmpDir := setupTestPaths(t)
	defer os.RemoveAll(tmpDir)
	t.Setenv("TMUX", "1")

	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("failed to get home dir: %v", err)
	}
	sessionPath := filepath.Join(home, "Code", "tmux-shunpo")
	if err := markSet(paths.MarksFile, 1, "~/Code/tmux-shunpo"); err != nil {
		t.Fatalf("failed to seed tilde path mark: %v", err)
	}

	app := &App{
		Paths: paths,
		Runner: &MockRunner{RunFunc: func(name string, args []string, stdin io.Reader) ([]byte, error) {
			if name == "tmux" && len(args) == 3 && args[0] == "display-message" && args[1] == "-p" {
				switch args[2] {
				case "#S":
					return []byte("tmux-shunpo\n"), nil
				case "#{session_path}":
					return []byte(sessionPath + "\n"), nil
				}
			}
			return []byte(""), nil
		}},
		Config: defaultConfig(),
		Stdout: io.Discard,
		Stderr: io.Discard,
	}

	slot, ref, err := app.markAddCurrent()
	if err == nil || err.Error() != "already marked" {
		t.Fatalf("expected tilde path mark to be detected as duplicate, slot=%d ref=%q err=%v", slot, ref, err)
	}
	if slot != 1 {
		t.Fatalf("expected duplicate to point at existing slot 1, got %d", slot)
	}
	marks, err := parseMarks(paths.MarksFile)
	if err != nil {
		t.Fatalf("failed to parse marks: %v", err)
	}
	if len(marks) != 1 {
		t.Fatalf("duplicate tilde path should not create a second mark, got marks: %v", marks)
	}
}

func TestAddCurrentMarkInsideTmuxStoresSessionName(t *testing.T) {
	paths, tmpDir := setupTestPaths(t)
	defer os.RemoveAll(tmpDir)
	t.Setenv("TMUX", "1")

	sessionPath := filepath.Join(tmpDir, "tmux-shunpo")
	if err := os.MkdirAll(sessionPath, 0755); err != nil {
		t.Fatalf("failed to create session path: %v", err)
	}

	app := &App{
		Paths: paths,
		Runner: &MockRunner{RunFunc: func(name string, args []string, stdin io.Reader) ([]byte, error) {
			if name == "tmux" && len(args) == 3 && args[0] == "display-message" && args[1] == "-p" {
				switch args[2] {
				case "#S":
					return []byte("tmux-shunpo\n"), nil
				case "#{session_path}":
					return []byte(sessionPath + "\n"), nil
				}
			}
			return []byte(""), nil
		}},
		Config: defaultConfig(),
		Stdout: io.Discard,
		Stderr: io.Discard,
	}

	slot, ref, err := app.markAddCurrent()
	if err != nil {
		t.Fatalf("markAddCurrent failed: %v", err)
	}
	if slot != 1 || ref != "tmux-shunpo" {
		t.Fatalf("expected slot 1 to store session name, got slot=%d ref=%q", slot, ref)
	}
	marks, err := parseMarks(paths.MarksFile)
	if err != nil {
		t.Fatalf("failed to parse marks: %v", err)
	}
	if marks[1] != "tmux-shunpo" {
		t.Fatalf("expected mark to store session name, got %q", marks[1])
	}
}

func TestLoadConfig(t *testing.T) {
	paths, tmpDir := setupTestPaths(t)
	defer os.RemoveAll(tmpDir)

	if err := os.MkdirAll(paths.ConfigDir, 0755); err != nil {
		t.Fatalf("failed to create config dir: %v", err)
	}

	// Write a test config file
	configContent := `
tool_window_base = 99
shell_init_delay = 0.5
window_name_max_length = 15

[ui]
popup_width = "90%"
popup_height = "80%"
popup_border_lines = "double"

[presets]
web = ["@editor", "npm run dev"]
`
	if err := os.WriteFile(paths.ConfigFile, []byte(configContent), 0600); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	var stderr bytes.Buffer
	cfg, err := loadConfig(paths.ConfigFile, &stderr)
	if err != nil {
		t.Fatalf("loadConfig failed: %v", err)
	}

	if cfg.ToolWindowBase != 99 {
		t.Errorf("expected ToolWindowBase 99, got %d", cfg.ToolWindowBase)
	}
	if cfg.ShellInitDelay != 0.5 {
		t.Errorf("expected ShellInitDelay 0.5, got %f", cfg.ShellInitDelay)
	}
	if cfg.UI.PopupWidth != "90%" {
		t.Errorf("expected PopupWidth '90%%', got %q", cfg.UI.PopupWidth)
	}
	if !reflect.DeepEqual(cfg.Presets["web"], []string{"@editor", "npm run dev"}) {
		t.Errorf("presets mismatch: %v", cfg.Presets["web"])
	}
}

func TestDoctorWarnsAboutMisplacedAndUnknownConfigKeys(t *testing.T) {
	paths, tmpDir := setupTestPaths(t)
	defer os.RemoveAll(tmpDir)

	if err := os.MkdirAll(paths.ConfigDir, 0755); err != nil {
		t.Fatalf("failed to create config dir: %v", err)
	}
	configContent := `
tool_window_base = 88
use_nerd_fonts = true
confirm_destructive = false
surprise = true

[ui]
popup_width = "80%"
tool_window_prefix = "@"
unknown_ui = "x"
`
	if err := os.WriteFile(paths.ConfigFile, []byte(configContent), 0600); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	var stdout, stderr bytes.Buffer
	app := &App{
		Paths:  paths,
		Runner: &MockRunner{},
		Stdout: &stdout,
		Stderr: &stderr,
	}

	exitCode := app.cmdDoctor()
	if exitCode != 0 {
		t.Fatalf("doctor should treat config key diagnostics as warnings, got exit code %d; output: %s", exitCode, stdout.String())
	}
	out := stdout.String()
	for _, want := range []string{
		"OK config parsed",
		`WARN top-level "use_nerd_fonts" is ignored; move it under [ui]`,
		`WARN top-level "confirm_destructive" is ignored; move it under [guardrails]`,
		`WARN unknown top-level key "surprise" is ignored`,
		`WARN [ui].tool_window_prefix is ignored; move "tool_window_prefix" to top level`,
		`WARN unknown [ui].unknown_ui key is ignored`,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("doctor output missing %q; output: %s", want, out)
		}
	}
}

func TestDoctorAcceptsKnownConfigKeys(t *testing.T) {
	paths, tmpDir := setupTestPaths(t)
	defer os.RemoveAll(tmpDir)

	if err := os.MkdirAll(paths.ConfigDir, 0755); err != nil {
		t.Fatalf("failed to create config dir: %v", err)
	}
	configContent := `
tool_window_base = 88
tool_window_prefix = "@"
normal_window_prefix = "#"

[ui]
use_nerd_fonts = true

[guardrails]
confirm_destructive = false
`
	if err := os.WriteFile(paths.ConfigFile, []byte(configContent), 0600); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	var stdout, stderr bytes.Buffer
	app := &App{
		Paths:  paths,
		Runner: &MockRunner{},
		Stdout: &stdout,
		Stderr: &stderr,
	}

	exitCode := app.cmdDoctor()
	if exitCode != 0 {
		t.Fatalf("doctor failed with exit code %d; output: %s", exitCode, stdout.String())
	}
	out := stdout.String()
	if strings.Contains(out, "is ignored") {
		t.Fatalf("doctor should not warn about known config keys in valid sections; output: %s", out)
	}
}

func TestDoctorFailsOnMalformedConfig(t *testing.T) {
	paths, tmpDir := setupTestPaths(t)
	defer os.RemoveAll(tmpDir)

	if err := os.MkdirAll(paths.ConfigDir, 0755); err != nil {
		t.Fatalf("failed to create config dir: %v", err)
	}
	if err := os.WriteFile(paths.ConfigFile, []byte("[ui\nuse_nerd_fonts = true\n"), 0600); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	var stdout, stderr bytes.Buffer
	app := &App{
		Paths:  paths,
		Runner: &MockRunner{},
		Stdout: &stdout,
		Stderr: &stderr,
	}

	exitCode := app.cmdDoctor()
	if exitCode == 0 {
		t.Fatalf("doctor should fail on malformed config; output: %s", stdout.String())
	}
	if !strings.Contains(stdout.String(), "FAIL config parse") {
		t.Fatalf("doctor output missing config parse failure; output: %s", stdout.String())
	}
}

func TestValidateDimension(t *testing.T) {
	tests := []struct {
		spec string
		want bool
	}{
		// Valid percentages
		{"80%", true},
		{"10%", true},
		{"100%", true},
		{"50%", true},
		// Invalid percentages
		{"5%", false},
		{"9%", false},
		{"101%", false},
		{"abc%", false},
		{"%", false},
		// Valid absolute numbers
		{"80", true},
		{"20", true},
		{"100", true},
		{"1920", true},
		// Invalid absolute numbers
		{"19", false},
		{"0", false},
		{"-5", false},
		// Invalid formats
		{"hello", false},
		{"", false},
		{"abc", false},
		{"80px", false},
		{"80.5", false},
	}
	for _, tc := range tests {
		if got := validateDimension(tc.spec); got != tc.want {
			t.Errorf("validateDimension(%q) = %v, want %v", tc.spec, got, tc.want)
		}
	}
}

func TestLoadConfigInvalidDimensions(t *testing.T) {
	paths, tmpDir := setupTestPaths(t)
	defer os.RemoveAll(tmpDir)

	if err := os.MkdirAll(paths.ConfigDir, 0755); err != nil {
		t.Fatalf("failed to create config dir: %v", err)
	}

	// Write a config with invalid dimensions
	configContent := `
[ui]
popup_width = "hello"
popup_height = "5%"
`
	if err := os.WriteFile(paths.ConfigFile, []byte(configContent), 0600); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	var stderr bytes.Buffer
	cfg, err := loadConfig(paths.ConfigFile, &stderr)
	if err != nil {
		t.Fatalf("loadConfig failed: %v", err)
	}

	// Invalid values should be replaced with defaults
	if cfg.UI.PopupWidth != "80%" {
		t.Errorf("expected PopupWidth to be reset to '80%%', got %q", cfg.UI.PopupWidth)
	}
	if cfg.UI.PopupHeight != "70%" {
		t.Errorf("expected PopupHeight to be reset to '70%%', got %q", cfg.UI.PopupHeight)
	}

	// Warnings should be written to stderr
	stderrStr := stderr.String()
	if !strings.Contains(stderrStr, "invalid popup_width") {
		t.Errorf("expected warning about invalid popup_width, got: %q", stderrStr)
	}
	if !strings.Contains(stderrStr, "invalid popup_height") {
		t.Errorf("expected warning about invalid popup_height, got: %q", stderrStr)
	}
}

func TestToolInitFromDefaults(t *testing.T) {
	paths, tmpDir := setupTestPaths(t)
	defer os.RemoveAll(tmpDir)

	app := &App{
		Paths:  paths,
		Runner: &MockRunner{},
		Config: defaultConfig(),
		Stdout: io.Discard,
		Stderr: io.Discard,
	}

	// Test SeshConfig wildcard and defaults resolution
	app.SeshConfig = SeshConfig{
		DefaultSession: DefaultSessionConfig{
			Windows: []string{"editor", "shell"},
		},
		Session: []SessionConfig{
			{
				Name:    "project-a",
				Path:    "/path/to/project-a",
				Windows: []string{"editor", "tests"},
			},
		},
	}

	err := app.toolInitFromDefaults("project-a")
	if err != nil {
		t.Fatalf("toolInitFromDefaults failed: %v", err)
	}

	// Read initialized file
	toolsFile := filepath.Join(paths.DataDir, "tools", "project-a")
	data, err := os.ReadFile(toolsFile)
	if err != nil {
		t.Fatalf("failed to read tools file: %v", err)
	}

	content := string(data)
	if !strings.Contains(content, "1: @editor") || !strings.Contains(content, "2: @tests") {
		t.Errorf("unexpected initialized content: %q", content)
	}
}

func TestGetTool(t *testing.T) {
	paths, tmpDir := setupTestPaths(t)
	defer os.RemoveAll(tmpDir)

	app := &App{
		Paths:  paths,
		Runner: &MockRunner{},
		Stdout: io.Discard,
		Stderr: io.Discard,
	}
	app.Config = defaultConfig()

	app.SeshConfig = SeshConfig{
		Window: []WindowConfig{
			{Name: "editor", StartupScript: "nvim ."},
		},
	}

	// Write tools file manually
	toolsFile := filepath.Join(paths.DataDir, "tools", "my-session")
	os.MkdirAll(filepath.Dir(toolsFile), 0755)
	err := os.WriteFile(toolsFile, []byte("1: @editor\n2: make test\n3: @shell\n4: @attached\n"), 0600)
	if err != nil {
		t.Fatalf("failed to write tools file: %v", err)
	}

	// Test getTool slot 1 (template)
	info, err := app.getTool("my-session", 1)
	if err != nil {
		t.Fatalf("getTool slot 1 failed: %v", err)
	}
	if info.WindowName != "nvim" {
		t.Errorf("expected WindowName nvim, got %q", info.WindowName)
	}
	if info.RawCommand != "@editor" {
		t.Errorf("expected RawCommand @editor, got %q", info.RawCommand)
	}
	if info.ResolvedCommand != "nvim ." {
		t.Errorf("expected ResolvedCommand 'nvim .', got %q", info.ResolvedCommand)
	}

	// Test getTool slot 2 (inline command)
	info, err = app.getTool("my-session", 2)
	if err != nil {
		t.Fatalf("getTool slot 2 failed: %v", err)
	}
	if info.WindowName != "make" {
		t.Errorf("expected WindowName make, got %q", info.WindowName)
	}
	if info.ResolvedCommand != "make test" {
		t.Errorf("expected ResolvedCommand 'make test', got %q", info.ResolvedCommand)
	}

	// Test getTool slot 3 (@shell)
	info, err = app.getTool("my-session", 3)
	if err != nil {
		t.Fatalf("getTool slot 3 failed: %v", err)
	}
	if info.WindowName != "shell" {
		t.Errorf("expected WindowName shell, got %q", info.WindowName)
	}
	if info.ResolvedCommand != "@shell" {
		t.Errorf("expected ResolvedCommand '@shell', got %q", info.ResolvedCommand)
	}

	// Test getTool slot 4 (@attached)
	info, err = app.getTool("my-session", 4)
	if err != nil {
		t.Fatalf("getTool slot 4 failed: %v", err)
	}
	if info.WindowName != "attached" {
		t.Errorf("expected WindowName attached, got %q", info.WindowName)
	}
	if info.ResolvedCommand != "@attached" {
		t.Errorf("expected ResolvedCommand '@attached', got %q", info.ResolvedCommand)
	}
}

func TestSaveRestoreSessionWindow(t *testing.T) {
	paths, tmpDir := setupTestPaths(t)
	defer os.RemoveAll(tmpDir)

	runner := &MockRunner{
		RunFunc: func(name string, args []string, stdin io.Reader) ([]byte, error) {
			if name == "tmux" && args[0] == "list-windows" {
				return []byte("1\n2\n3\n"), nil
			}
			return nil, nil
		},
	}

	app := &App{
		Paths:  paths,
		Runner: runner,
		Stdout: io.Discard,
		Stderr: io.Discard,
	}

	err := app.saveSessionWindow("project-a", "2")
	if err != nil {
		t.Fatalf("saveSessionWindow failed: %v", err)
	}

	// Check if state is saved
	data, err := os.ReadFile(paths.SessionStateFile)
	if err != nil {
		t.Fatalf("failed to read state file: %v", err)
	}
	if !strings.Contains(string(data), "project-a:2") {
		t.Errorf("expected state to contain project-a:2, got %q", string(data))
	}

	// Verify restoreSessionWindow calls select-window for the correct window
	selectCalled := false
	runner.RunFunc = func(name string, args []string, stdin io.Reader) ([]byte, error) {
		if name == "tmux" {
			if args[0] == "list-windows" {
				return []byte("1\n2\n3\n"), nil
			}
			if args[0] == "select-window" && args[2] == "project-a:2" {
				selectCalled = true
			}
		}
		return nil, nil
	}

	err = app.restoreSessionWindow("project-a")
	if err != nil {
		t.Fatalf("restoreSessionWindow failed: %v", err)
	}
	if !selectCalled {
		t.Errorf("expected tmux select-window to be called for project-a:2")
	}
}

func TestToolCompact(t *testing.T) {
	paths, tmpDir := setupTestPaths(t)
	defer os.RemoveAll(tmpDir)

	moveCalled := false
	swapCalled := false

	runner := &MockRunner{
		RunFunc: func(name string, args []string, stdin io.Reader) ([]byte, error) {
			if name == "tmux" {
				if args[0] == "list-windows" {
					// Window indexes 92 and 93 exist (slots 5 and 6)
					return []byte("92\n93\n"), nil
				}
				if args[0] == "move-window" {
					moveCalled = true
					if args[3] != "my-session:93" || args[5] != "my-session:88" {
						t.Errorf("unexpected move-window args: %v", args)
					}
				}
				if args[0] == "swap-window" {
					swapCalled = true
				}
			}
			return nil, nil
		},
	}

	app := &App{
		Paths:  paths,
		Runner: runner,
		Stdout: io.Discard,
		Stderr: io.Discard,
	}
	app.Config = defaultConfig() // Base is 88

	// Write tools file manually
	toolsFile := filepath.Join(paths.DataDir, "tools", "my-session")
	os.MkdirAll(filepath.Dir(toolsFile), 0755)
	// Slot 5 is empty/not present, slot 6 has @attached
	err := os.WriteFile(toolsFile, []byte("6: @attached\n"), 0600)
	if err != nil {
		t.Fatalf("failed to write tools file: %v", err)
	}

	err = app.toolCompact("my-session")
	if err != nil {
		t.Fatalf("toolCompact failed: %v", err)
	}

	if !moveCalled {
		t.Errorf("expected move-window to be called")
	}
	if swapCalled {
		t.Errorf("expected swap-window to NOT be called since slot 1 (index 88) was empty")
	}

	// Verify tools file content: should be 1: @attached
	data, err := os.ReadFile(toolsFile)
	if err != nil {
		t.Fatalf("failed to read tools file: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "1: @attached") || strings.Contains(content, "6:") {
		t.Errorf("unexpected tools file content: %q", content)
	}
}

func TestToolAddCurrentWindowAppendsToNextEmptySlot(t *testing.T) {
	paths, tmpDir := setupTestPaths(t)
	defer os.RemoveAll(tmpDir)

	toolsFile := filepath.Join(paths.DataDir, "tools", "my-session")
	if err := os.MkdirAll(filepath.Dir(toolsFile), 0755); err != nil {
		t.Fatalf("failed to create tools dir: %v", err)
	}
	if err := os.WriteFile(toolsFile, []byte("1: @editor\n"), 0600); err != nil {
		t.Fatalf("failed to seed tools file: %v", err)
	}

	var moveCalled, renameCalled, metadataCalled bool
	runner := &MockRunner{RunFunc: func(name string, args []string, stdin io.Reader) ([]byte, error) {
		if name != "tmux" {
			return nil, nil
		}
		switch args[0] {
		case "display-message":
			if len(args) == 3 && args[2] == "#S" {
				return []byte("my-session\n"), nil
			}
			if len(args) == 3 && args[2] == "#{window_index}" {
				return []byte("3\n"), nil
			}
			if len(args) == 5 && args[3] == "my-session:3" && args[4] == "#{window_name}" {
				return []byte("server\n"), nil
			}
			if len(args) == 5 && args[3] == "my-session:3" && args[4] == "#S:#I" {
				return []byte("my-session:3\n"), nil
			}
			if len(args) == 5 && args[3] == "my-session:89" {
				return []byte("@server\x1f89\x1fserver\x1f\x1f0\n"), nil
			}
		case "list-keys":
			return []byte("root:::M-i:::run-shell \"tmux-shunpo --goto @2\"\n"), nil
		case "list-windows":
			return []byte("1\n3\n88\n"), nil
		case "move-window":
			moveCalled = true
			if !reflect.DeepEqual(args, []string{"move-window", "-s", "my-session:3", "-t", "my-session:89"}) {
				t.Fatalf("unexpected move-window args: %v", args)
			}
		case "rename-window":
			renameCalled = true
			if !reflect.DeepEqual(args, []string{"rename-window", "-t", "@server", "@i server"}) {
				t.Fatalf("unexpected rename-window args: %v", args)
			}
		case "set-option":
			metadataCalled = true
			if !reflect.DeepEqual(args, []string{"set-option", "-w", "-t", "@server", "@shunpo_label", "@i"}) {
				t.Fatalf("unexpected set-option args: %v", args)
			}
		case "select-window":
			if !reflect.DeepEqual(args, []string{"select-window", "-t", "my-session:89"}) {
				t.Fatalf("unexpected select-window args: %v", args)
			}
		}
		return nil, nil
	}}

	app := &App{
		Paths:  paths,
		Runner: runner,
		Config: defaultConfig(),
		Stdout: io.Discard,
		Stderr: io.Discard,
	}

	slot, err := app.toolAddCurrentWindow()
	if err != nil {
		t.Fatalf("toolAddCurrentWindow failed: %v", err)
	}
	if slot != 2 {
		t.Fatalf("expected current window to append to slot 2, got %d", slot)
	}
	if !moveCalled {
		t.Fatal("expected current window to move into tool slot")
	}
	if !renameCalled || !metadataCalled {
		t.Fatal("expected attached tool window to receive its physical-key label and metadata")
	}
	data, err := os.ReadFile(toolsFile)
	if err != nil {
		t.Fatalf("failed to read tools file: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "1: @editor") || !strings.Contains(content, "2: @attached") {
		t.Fatalf("unexpected tools file content: %s", content)
	}
}

func TestToolAddCurrentWindowCreatesToolsFileWhenMissing(t *testing.T) {
	paths, tmpDir := setupTestPaths(t)
	defer os.RemoveAll(tmpDir)

	runner := &MockRunner{RunFunc: func(name string, args []string, stdin io.Reader) ([]byte, error) {
		if name != "tmux" {
			return nil, nil
		}
		switch args[0] {
		case "display-message":
			if len(args) == 3 && args[2] == "#S" {
				return []byte("my-session\n"), nil
			}
			if len(args) == 3 && args[2] == "#{window_index}" {
				return []byte("3\n"), nil
			}
			if len(args) == 5 && args[3] == "my-session:3" && args[4] == "#{window_name}" {
				return []byte("server\n"), nil
			}
			if len(args) == 5 && args[3] == "my-session:3" && args[4] == "#S:#I" {
				return []byte("my-session:3\n"), nil
			}
		case "list-windows":
			return []byte("1\n3\n"), nil
		}
		return nil, nil
	}}

	app := &App{
		Paths:  paths,
		Runner: runner,
		Config: defaultConfig(),
		Stdout: io.Discard,
		Stderr: io.Discard,
	}

	slot, err := app.toolAddCurrentWindow()
	if err != nil {
		t.Fatalf("toolAddCurrentWindow failed: %v", err)
	}
	if slot != 1 {
		t.Fatalf("expected current window to use slot 1, got %d", slot)
	}
	data, err := os.ReadFile(filepath.Join(paths.DataDir, "tools", "my-session"))
	if err != nil {
		t.Fatalf("failed to read tools file: %v", err)
	}
	if !strings.Contains(string(data), "1: @attached") {
		t.Fatalf("unexpected tools file content: %s", string(data))
	}
}

func TestToolAddCurrentWindowFailsWhenToolsFull(t *testing.T) {
	paths, tmpDir := setupTestPaths(t)
	defer os.RemoveAll(tmpDir)

	toolsFile := filepath.Join(paths.DataDir, "tools", "my-session")
	if err := os.MkdirAll(filepath.Dir(toolsFile), 0755); err != nil {
		t.Fatalf("failed to create tools dir: %v", err)
	}
	var content strings.Builder
	for i := MinSlot; i <= MaxSlot; i++ {
		fmt.Fprintf(&content, "%d: @shell\n", i)
	}
	if err := os.WriteFile(toolsFile, []byte(content.String()), 0600); err != nil {
		t.Fatalf("failed to seed tools file: %v", err)
	}

	app := &App{
		Paths: paths,
		Runner: &MockRunner{RunFunc: func(name string, args []string, stdin io.Reader) ([]byte, error) {
			if name == "tmux" && len(args) == 3 && args[0] == "display-message" && args[2] == "#S" {
				return []byte("my-session\n"), nil
			}
			return nil, nil
		}},
		Config: defaultConfig(),
		Stdout: io.Discard,
		Stderr: io.Discard,
	}

	_, err := app.toolAddCurrentWindow()
	if err == nil || !strings.Contains(err.Error(), "all tool slots") {
		t.Fatalf("expected full tools error, got %v", err)
	}
}

func TestToolAddCurrentWindowReclaimsStaleAttachedFirst(t *testing.T) {
	paths, tmpDir := setupTestPaths(t)
	defer os.RemoveAll(tmpDir)

	toolsFile := filepath.Join(paths.DataDir, "tools", "my-session")
	if err := os.MkdirAll(filepath.Dir(toolsFile), 0755); err != nil {
		t.Fatalf("failed to create tools dir: %v", err)
	}
	// Slot 1 holds @attached, but its window (index 88, base=88) was closed.
	if err := os.WriteFile(toolsFile, []byte("1: @attached\n"), 0600); err != nil {
		t.Fatalf("failed to seed tools file: %v", err)
	}

	runner := &MockRunner{RunFunc: func(name string, args []string, stdin io.Reader) ([]byte, error) {
		if name != "tmux" {
			return nil, nil
		}
		switch args[0] {
		case "display-message":
			if len(args) == 3 && args[2] == "#S" {
				return []byte("my-session\n"), nil
			}
			if len(args) == 3 && args[2] == "#{window_index}" {
				return []byte("3\n"), nil
			}
			if len(args) == 5 && args[3] == "my-session:3" && args[4] == "#{window_name}" {
				return []byte("server\n"), nil
			}
			if len(args) == 5 && args[3] == "my-session:3" && args[4] == "#S:#I" {
				return []byte("my-session:3\n"), nil
			}
		case "list-windows":
			// Only window 3 exists; tool window index 88 (slot 1) is gone.
			return []byte("3\n"), nil
		case "move-window":
			return []byte(""), nil
		case "rename-window":
			return []byte(""), nil
		case "select-window":
			return []byte(""), nil
		}
		return nil, nil
	}}

	app := &App{
		Paths:  paths,
		Runner: runner,
		Config: defaultConfig(),
		Stdout: io.Discard,
		Stderr: io.Discard,
	}

	slot, err := app.toolAddCurrentWindow()
	if err != nil {
		t.Fatalf("toolAddCurrentWindow failed: %v", err)
	}
	// Stale @attached at slot 1 should be reclaimed first, so the current
	// window fills slot 1 instead of being pushed to slot 2.
	if slot != 1 {
		t.Fatalf("expected current window to reclaim slot 1, got %d", slot)
	}
	data, err := os.ReadFile(toolsFile)
	if err != nil {
		t.Fatalf("failed to read tools file: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "1: @attached") {
		t.Fatalf("expected slot 1 to hold the new @attached, got: %s", content)
	}
	if strings.Contains(content, "2:") {
		t.Fatalf("expected slot 2 to remain empty, got: %s", content)
	}
}

func TestNavigateToTool(t *testing.T) {
	paths, tmpDir := setupTestPaths(t)
	defer os.RemoveAll(tmpDir)

	// Create a mock runner that simulates tmux behavior
	var tmuxCalls []string
	runner := &MockRunner{
		RunFunc: func(name string, args []string, stdin io.Reader) ([]byte, error) {
			if name == "tmux" {
				// Log all tmux calls for verification
				tmuxCalls = append(tmuxCalls, strings.Join(args, " "))

				switch args[0] {
				case "display-message":
					if args[2] == "#S" {
						return []byte("test-session\n"), nil
					}
					if args[2] == "#{session_path}" {
						return []byte("/tmp/test\n"), nil
					}
					if len(args) > 3 && args[3] == "#{window_name}" {
						return []byte("⚡nvim\n"), nil
					}
					if len(args) > 3 && args[3] == "#{pane_current_command} #{pane_pid}" {
						return []byte("bash 12345\n"), nil
					}
				case "list-windows":
					// Window 88 doesn't exist (only window 1 exists)
					return []byte("1 1\n"), nil
				case "pgrep":
					// No child processes (idle)
					return []byte(""), nil
				case "new-window", "send-keys", "select-window", "rename-window":
					return []byte(""), nil
				}
			}
			return []byte(""), nil
		},
	}

	app := &App{
		Paths:  paths,
		Runner: runner,
		Config: defaultConfig(),
		Stdout: io.Discard,
		Stderr: io.Discard,
	}

	// Setup: create a tools file with a command
	toolsFile := filepath.Join(paths.DataDir, "tools", "test-session")
	os.MkdirAll(filepath.Dir(toolsFile), 0755)
	err := os.WriteFile(toolsFile, []byte("1: nvim .\n"), 0600)
	if err != nil {
		t.Fatalf("failed to write tools file: %v", err)
	}

	// Test 1: Navigate to tool that doesn't have window yet
	err = app.navigateToTool(1)
	if err != nil {
		t.Errorf("navigateToTool failed: %v", err)
	}

	// Verify new-window was called
	foundNewWindow := false
	for _, call := range tmuxCalls {
		if strings.HasPrefix(call, "new-window") {
			foundNewWindow = true
			break
		}
	}
	if !foundNewWindow {
		t.Error("expected new-window to be called")
	}

	// Test 2: Invalid slot should fail
	err = app.navigateToTool(0)
	if err == nil {
		t.Error("expected error for invalid slot 0")
	}

	err = app.navigateToTool(10)
	if err == nil {
		t.Error("expected error for invalid slot 10")
	}
}

func TestReconcileAttachedToolsRemovesDeadSlots(t *testing.T) {
	paths, tmpDir := setupTestPaths(t)
	defer os.RemoveAll(tmpDir)

	toolsFile := filepath.Join(paths.DataDir, "tools", "my-session")
	if err := os.MkdirAll(filepath.Dir(toolsFile), 0755); err != nil {
		t.Fatalf("failed to create tools dir: %v", err)
	}
	// Slot 1: persistent @template. Slot 2: @attached (alive, index 89). Slot 3: @attached (dead, index 90 absent).
	if err := os.WriteFile(toolsFile, []byte("1: @editor\n2: @attached\n3: @attached\n"), 0600); err != nil {
		t.Fatalf("failed to seed tools file: %v", err)
	}

	runner := &MockRunner{RunFunc: func(name string, args []string, stdin io.Reader) ([]byte, error) {
		if name == "tmux" && args[0] == "list-windows" {
			return []byte("1\n89\n"), nil
		}
		return nil, nil
	}}
	app := &App{Paths: paths, Runner: runner, Config: defaultConfig(), Stdout: io.Discard, Stderr: io.Discard}

	reclaimed, err := app.reconcileAttachedTools("my-session")
	if err != nil {
		t.Fatalf("reconcile failed: %v", err)
	}
	if len(reclaimed) != 1 || reclaimed[0] != 3 {
		t.Fatalf("expected reclaimed [3], got %v", reclaimed)
	}
	data, _ := os.ReadFile(toolsFile)
	content := string(data)
	if !strings.Contains(content, "1: @editor") || !strings.Contains(content, "2: @attached") {
		t.Fatalf("expected slots 1 and 2 preserved, got: %s", content)
	}
	if strings.Contains(content, "3:") {
		t.Fatalf("expected slot 3 removed, got: %s", content)
	}
}

func TestReconcileKeepsPersistentEvenIfWindowGone(t *testing.T) {
	paths, tmpDir := setupTestPaths(t)
	defer os.RemoveAll(tmpDir)

	toolsFile := filepath.Join(paths.DataDir, "tools", "my-session")
	if err := os.MkdirAll(filepath.Dir(toolsFile), 0755); err != nil {
		t.Fatalf("failed to create tools dir: %v", err)
	}
	// Persistent raw command at slot 1; its window index 88 is absent (by design it gets re-created on navigate).
	if err := os.WriteFile(toolsFile, []byte("1: nvim .\n2: @shell\n"), 0600); err != nil {
		t.Fatalf("failed to seed tools file: %v", err)
	}

	runner := &MockRunner{RunFunc: func(name string, args []string, stdin io.Reader) ([]byte, error) {
		if name == "tmux" && args[0] == "list-windows" {
			return []byte("5\n"), nil
		}
		return nil, nil
	}}
	app := &App{Paths: paths, Runner: runner, Config: defaultConfig(), Stdout: io.Discard, Stderr: io.Discard}

	reclaimed, err := app.reconcileAttachedTools("my-session")
	if err != nil {
		t.Fatalf("reconcile failed: %v", err)
	}
	if len(reclaimed) != 0 {
		t.Fatalf("expected no reclaims for persistent tools, got %v", reclaimed)
	}
	data, _ := os.ReadFile(toolsFile)
	content := string(data)
	if !strings.Contains(content, "1: nvim .") || !strings.Contains(content, "2: @shell") {
		t.Fatalf("expected file unchanged, got: %s", content)
	}
}

func TestLoadToolsMapParsesFiltersAndDedups(t *testing.T) {
	paths, tmpDir := setupTestPaths(t)
	defer os.RemoveAll(tmpDir)

	toolsFile := filepath.Join(paths.DataDir, "tools", "my-session")
	if err := os.MkdirAll(filepath.Dir(toolsFile), 0755); err != nil {
		t.Fatalf("failed to create tools dir: %v", err)
	}
	content := strings.Join([]string{
		"# Tools for session: my-session",
		"",
		"2: @attached",
		"99: out-of-range", // dropped: outside MinSlot..MaxSlot
		"3:",               // dropped: empty command
		"x: unparseable",   // dropped: slot not an int
		"1: first",         // dup with next; last-wins
		"1: second",
	}, "\n")
	if err := os.WriteFile(toolsFile, []byte(content), 0600); err != nil {
		t.Fatalf("failed to seed tools file: %v", err)
	}

	app := &App{Paths: paths, Runner: &MockRunner{}, Config: defaultConfig(), Stdout: io.Discard, Stderr: io.Discard}
	tools, slots, err := app.loadToolsMap("my-session")
	if err != nil {
		t.Fatalf("loadToolsMap failed: %v", err)
	}
	if got, want := tools[1], "second"; got != want {
		t.Errorf("slot 1 = %q, want %q (last wins)", got, want)
	}
	if got, want := tools[2], "@attached"; got != want {
		t.Errorf("slot 2 = %q, want %q", got, want)
	}
	if _, ok := tools[3]; ok {
		t.Errorf("slot 3 should be dropped (empty command)")
	}
	if _, ok := tools[99]; ok {
		t.Errorf("slot 99 should be dropped (out of range)")
	}
	if !reflect.DeepEqual(slots, []int{1, 2}) {
		t.Errorf("slots = %v, want [1 2] (ascending, deduped)", slots)
	}
}

func TestLoadToolsMapMissingFileReturnsEmpty(t *testing.T) {
	paths, tmpDir := setupTestPaths(t)
	defer os.RemoveAll(tmpDir)

	app := &App{Paths: paths, Runner: &MockRunner{}, Config: defaultConfig(), Stdout: io.Discard, Stderr: io.Discard}
	tools, slots, err := app.loadToolsMap("no-such-session")
	if err != nil {
		t.Fatalf("missing file should not error, got: %v", err)
	}
	if len(tools) != 0 || len(slots) != 0 {
		t.Errorf("expected empty, got tools=%v slots=%v", tools, slots)
	}
}

func TestSaveToolsMapWritesAscendingWithHeader(t *testing.T) {
	paths, tmpDir := setupTestPaths(t)
	defer os.RemoveAll(tmpDir)

	tools := map[int]string{3: "c", 1: "a"}
	slots := []int{1, 3}

	app := &App{Paths: paths, Runner: &MockRunner{}, Config: defaultConfig(), Stdout: io.Discard, Stderr: io.Discard}
	if err := app.saveToolsMap("my-session", tools, slots); err != nil {
		t.Fatalf("saveToolsMap failed: %v", err)
	}
	data, _ := os.ReadFile(filepath.Join(paths.DataDir, "tools", "my-session"))
	content := string(data)
	want := strings.Join([]string{
		"# Tools for session: my-session",
		"1: a",
		"3: c",
		"",
	}, "\n")
	if content != want {
		t.Errorf("file content mismatch:\nwant %q\ngot  %q", want, content)
	}
}

func TestReconcileNoopOnTmuxError(t *testing.T) {
	paths, tmpDir := setupTestPaths(t)
	defer os.RemoveAll(tmpDir)

	toolsFile := filepath.Join(paths.DataDir, "tools", "my-session")
	if err := os.MkdirAll(filepath.Dir(toolsFile), 0755); err != nil {
		t.Fatalf("failed to create tools dir: %v", err)
	}
	// Dead @attached at slot 1 (index 88 absent), but tmux cannot be queried.
	if err := os.WriteFile(toolsFile, []byte("1: @attached\n2: @attached\n"), 0600); err != nil {
		t.Fatalf("failed to seed tools file: %v", err)
	}

	runner := &MockRunner{RunFunc: func(name string, args []string, stdin io.Reader) ([]byte, error) {
		if name == "tmux" && args[0] == "list-windows" {
			return nil, fmt.Errorf("tmux: no server")
		}
		return nil, nil
	}}
	app := &App{Paths: paths, Runner: runner, Config: defaultConfig(), Stdout: io.Discard, Stderr: io.Discard}

	reclaimed, err := app.reconcileAttachedTools("my-session")
	if err != nil {
		t.Fatalf("reconcile should swallow tmux error, got: %v", err)
	}
	if len(reclaimed) != 0 {
		t.Fatalf("expected no reclaims when tmux unavailable, got %v", reclaimed)
	}
	data, _ := os.ReadFile(toolsFile)
	content := string(data)
	if !strings.Contains(content, "1: @attached") || !strings.Contains(content, "2: @attached") {
		t.Fatalf("expected file left untouched on tmux error, got: %s", content)
	}
}

func TestNavigateToToolAttachedRemoved(t *testing.T) {
	paths, tmpDir := setupTestPaths(t)
	defer os.RemoveAll(tmpDir)

	runner := &MockRunner{
		RunFunc: func(name string, args []string, stdin io.Reader) ([]byte, error) {
			if name == "tmux" {
				if len(args) >= 3 && args[0] == "display-message" {
					if args[2] == "#S" {
						return []byte("test-session\n"), nil
					}
					if args[2] == "#{session_path}" {
						return []byte("/tmp/test\n"), nil
					}
				}
				if args[0] == "list-windows" {
					// Window doesn't exist (was closed)
					return []byte(""), nil
				}
			}
			return []byte(""), nil
		},
	}

	app := &App{
		Paths:  paths,
		Runner: runner,
		Config: defaultConfig(),
		Stdout: io.Discard,
		Stderr: io.Discard,
	}

	// Setup: create a tools file with @attached
	toolsFile := filepath.Join(paths.DataDir, "tools", "test-session")
	os.MkdirAll(filepath.Dir(toolsFile), 0755)
	err := os.WriteFile(toolsFile, []byte("1: @attached\n"), 0600)
	if err != nil {
		t.Fatalf("failed to write tools file: %v", err)
	}

	// Navigate to @attached tool whose window was closed
	// Should remove the tool and return nil (no error)
	err = app.navigateToTool(1)
	if err != nil {
		t.Errorf("navigateToTool with closed @attached should not error: %v", err)
	}

	// Verify tool was removed
	_, err = app.getTool("test-session", 1)
	if err == nil {
		t.Error("expected @attached tool to be removed after window closed")
	}
}

func TestToolCompactReconcilesBeforeCompacting(t *testing.T) {
	paths, tmpDir := setupTestPaths(t)
	defer os.RemoveAll(tmpDir)

	moveCalled := false
	runner := &MockRunner{
		RunFunc: func(name string, args []string, stdin io.Reader) ([]byte, error) {
			if name == "tmux" {
				if args[0] == "list-windows" {
					// Slot 5 (index 92) alive; slot 3 (index 90) dead.
					return []byte("92\n"), nil
				}
				if args[0] == "move-window" {
					moveCalled = true
					if !reflect.DeepEqual(args, []string{"move-window", "-d", "-s", "my-session:92", "-t", "my-session:88"}) {
						t.Errorf("unexpected move-window args: %v", args)
					}
				}
				if args[0] == "swap-window" {
					t.Errorf("did not expect swap-window: %v", args)
				}
			}
			return nil, nil
		},
	}
	app := &App{Paths: paths, Runner: runner, Config: defaultConfig(), Stdout: io.Discard, Stderr: io.Discard}

	toolsFile := filepath.Join(paths.DataDir, "tools", "my-session")
	os.MkdirAll(filepath.Dir(toolsFile), 0755)
	// Dead @attached at slot 3, live @attached at slot 5.
	if err := os.WriteFile(toolsFile, []byte("3: @attached\n5: @attached\n"), 0600); err != nil {
		t.Fatalf("failed to write tools file: %v", err)
	}

	if err := app.toolCompact("my-session"); err != nil {
		t.Fatalf("toolCompact failed: %v", err)
	}
	if !moveCalled {
		t.Errorf("expected move-window for live slot 5 -> slot 1")
	}
	data, _ := os.ReadFile(toolsFile)
	content := string(data)
	if !strings.Contains(content, "1: @attached") || strings.Contains(content, "3:") || strings.Contains(content, "5:") {
		t.Errorf("unexpected tools file content: %q", content)
	}
}

func TestSessionConnectWithState(t *testing.T) {
	paths, tmpDir := setupTestPaths(t)
	defer os.RemoveAll(tmpDir)

	var tmuxCalls []string
	var runInteractiveCalled bool
	runner := &MockRunner{
		RunFunc: func(name string, args []string, stdin io.Reader) ([]byte, error) {
			if name == "tmux" {
				tmuxCalls = append(tmuxCalls, strings.Join(args, " "))
				switch args[0] {
				case "display-message":
					if args[2] == "#S" {
						return []byte("current-session\n"), nil
					}
					if args[2] == "#I" {
						return []byte("2\n"), nil
					}
				case "list-windows":
					return []byte("1\n2\n3\n"), nil
				case "select-window":
					return []byte(""), nil
				}
			} else if name == "sesh" {
				// Simulate successful sesh connect
				return []byte(""), nil
			}
			return []byte(""), nil
		},
		RunCombinedFunc: func(name string, args []string, stdin io.Reader) ([]byte, error) {
			if name == "sesh" && args[0] == "connect" {
				return []byte(""), nil
			}
			return []byte(""), nil
		},
		RunInteractiveFunc: func(name string, args []string, stdin io.Reader, stdout io.Writer, stderr io.Writer) error {
			runInteractiveCalled = true
			return nil
		},
	}

	app := &App{
		Paths:  paths,
		Runner: runner,
		Config: defaultConfig(),
		Stdout: io.Discard,
		Stderr: io.Discard,
	}
	app.SeshConfig = SeshConfig{DefaultSession: DefaultSessionConfig{Windows: []string{"editor"}}}

	// Set TMUX environment to simulate being in tmux
	os.Setenv("TMUX", "/tmp/tmux-1000/default,12345,0")
	defer os.Unsetenv("TMUX")

	// Test: Connect to session
	err := app.sessionConnectWithState("target-session")
	if err != nil {
		t.Errorf("sessionConnectWithState failed: %v", err)
	}

	// Verify sesh connect was called
	// (This is tested via the mock runner)
	if runInteractiveCalled {
		t.Error("session navigation should not prompt for tool initialization")
	}
	for _, sessionName := range []string{"current-session", "target-session"} {
		toolsFile := filepath.Join(paths.DataDir, "tools", sessionName)
		if _, err := os.Stat(toolsFile); !os.IsNotExist(err) {
			t.Errorf("session navigation should not create tools file %s", toolsFile)
		}
	}

	// Test: Empty session name should fail
	err = app.sessionConnectWithState("")
	if err == nil {
		t.Error("expected error for empty session name")
	}
}

func TestCmdBootstrapNoPreset(t *testing.T) {
	paths, tmpDir := setupTestPaths(t)
	defer os.RemoveAll(tmpDir)

	runner := &MockRunner{
		RunFunc: func(name string, args []string, stdin io.Reader) ([]byte, error) {
			if name == "tmux" {
				switch args[0] {
				case "display-message":
					if args[2] == "#S" {
						return []byte("test-session\n"), nil
					}
				}
			}
			return []byte(""), nil
		},
	}

	app := &App{
		Paths:  paths,
		Runner: runner,
		Config: defaultConfig(),
		Stdout: io.Discard,
		Stderr: io.Discard,
	}

	// Test: Bootstrap with non-existent preset should fail
	err := app.cmdBootstrap("nonexistent-preset", false, false)
	if err == nil {
		t.Error("expected error for non-existent preset")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected 'not found' error, got: %v", err)
	}
}

func TestCmdBootstrapNoTools(t *testing.T) {
	paths, tmpDir := setupTestPaths(t)
	defer os.RemoveAll(tmpDir)

	runner := &MockRunner{
		RunFunc: func(name string, args []string, stdin io.Reader) ([]byte, error) {
			if name == "tmux" {
				switch args[0] {
				case "display-message":
					if args[2] == "#S" {
						return []byte("test-session\n"), nil
					}
				}
			}
			return []byte(""), nil
		},
	}

	app := &App{
		Paths:  paths,
		Runner: runner,
		Config: Config{
			Presets: map[string][]string{
				"empty": {},
			},
		},
		Stdout: io.Discard,
		Stderr: io.Discard,
	}

	// Test: Bootstrap with empty preset should fail
	err := app.cmdBootstrap("empty", false, false)
	if err == nil {
		t.Error("expected error for empty preset")
	}
	if !strings.Contains(err.Error(), "no tools") {
		t.Errorf("expected 'no tools' error, got: %v", err)
	}
}

func TestNavigateToToolPrompt(t *testing.T) {
	paths, tmpDir := setupTestPaths(t)
	defer os.RemoveAll(tmpDir)

	// uiInitTools escapes to a popup (os.Exit) when stdin is not a tty and TMUX
	// is set. Tests have no tty, so clear TMUX to exercise the gum-choose path
	// regardless of whether the suite is run from inside tmux.
	origTMUX, hadTMUX := os.LookupEnv("TMUX")
	os.Unsetenv("TMUX")
	defer func() {
		if hadTMUX {
			os.Setenv("TMUX", origTMUX)
		}
	}()

	var runInteractiveCalled bool
	runner := &MockRunner{
		RunFunc: func(name string, args []string, stdin io.Reader) ([]byte, error) {
			if name == "tmux" {
				switch args[0] {
				case "display-message":
					if args[2] == "#S" {
						return []byte("test-session\n"), nil
					}
					if args[2] == "#{session_path}" {
						return []byte("/tmp/test\n"), nil
					}
				case "list-windows":
					return []byte("1 1\n"), nil
				}
			}
			return []byte(""), nil
		},
		RunInteractiveFunc: func(name string, args []string, stdin io.Reader, stdout io.Writer, stderr io.Writer) error {
			if name == "gum" && args[0] == "choose" {
				runInteractiveCalled = true
				_, _ = stdout.Write([]byte("Start empty (manual setup)\n"))
			}
			return nil
		},
	}

	app := &App{
		Paths:  paths,
		Runner: runner,
		Config: defaultConfig(),
		Stdout: io.Discard,
		Stderr: io.Discard,
	}
	app.SeshConfig = SeshConfig{DefaultSession: DefaultSessionConfig{Windows: []string{"editor"}}}

	// Trigger navigation. Since tools file does not exist, it should trigger uiInitTools
	// which will call runInteractive and choose "Start empty".
	err := app.navigateToTool(1)
	if err != nil {
		t.Fatalf("navigateToTool with prompt initialization failed: %v", err)
	}

	if !runInteractiveCalled {
		t.Error("expected gum choose to be called for prompt initialization")
	}

	// Verify that empty tools file was written
	toolsFile := filepath.Join(paths.DataDir, "tools", "test-session")
	data, err := os.ReadFile(toolsFile)
	if err != nil {
		t.Fatalf("failed to read created tools file: %v", err)
	}
	if !strings.Contains(string(data), "# Tools for session: test-session") {
		t.Errorf("unexpected tools file content: %s", string(data))
	}
	if strings.Contains(string(data), "1: @editor") {
		t.Errorf("sesh.toml defaults should not be loaded when starting empty, got: %s", string(data))
	}
}

func TestUIInitToolsLoadSlotsFromPresetIsNonDestructive(t *testing.T) {
	paths, tmpDir := setupTestPaths(t)
	defer os.RemoveAll(tmpDir)
	t.Setenv("TMUX", "")

	var chooseCalls int
	var tmuxWindowOps []string
	runner := &MockRunner{
		RunFunc: func(name string, args []string, stdin io.Reader) ([]byte, error) {
			if name == "tmux" && len(args) > 0 {
				switch args[0] {
				case "new-window", "kill-window", "select-window":
					tmuxWindowOps = append(tmuxWindowOps, strings.Join(args, " "))
				}
			}
			return []byte(""), nil
		},
		RunInteractiveFunc: func(name string, args []string, stdin io.Reader, stdout io.Writer, stderr io.Writer) error {
			if name != "gum" || len(args) == 0 {
				return nil
			}
			if args[0] == "style" {
				return nil
			}
			if args[0] != "choose" {
				t.Fatalf("unexpected gum command: %v", args)
			}

			chooseCalls++
			optionsBytes, _ := io.ReadAll(stdin)
			options := string(optionsBytes)
			switch chooseCalls {
			case 1:
				if !strings.Contains(options, "Load slots from preset") {
					t.Fatalf("initialize menu missing renamed preset option: %q", options)
				}
				if strings.Contains(options, "Choose a bootstrap preset") {
					t.Fatalf("initialize menu still contains old bootstrap wording: %q", options)
				}
				_, _ = stdout.Write([]byte("Load slots from preset\n"))
			case 2:
				if !strings.Contains(options, "default") {
					t.Fatalf("preset menu missing default preset: %q", options)
				}
				_, _ = stdout.Write([]byte("default\n"))
			default:
				t.Fatalf("unexpected extra gum choose call %d", chooseCalls)
			}
			return nil
		},
	}

	app := &App{
		Paths:  paths,
		Runner: runner,
		Config: Config{
			ToolWindowPrefix: "⚡",
			UI:               defaultConfig().UI,
			Presets: map[string][]string{
				"default": {"@editor", "cargo test", "@shell"},
			},
		},
		Stdout: io.Discard,
		Stderr: io.Discard,
	}

	initialized, err := app.uiInitTools("test-session")
	if err != nil {
		t.Fatalf("uiInitTools failed: %v", err)
	}
	if !initialized {
		t.Fatal("expected tools to be initialized")
	}
	if len(tmuxWindowOps) > 0 {
		t.Fatalf("slot initialization should not bootstrap tmux windows, got ops: %v", tmuxWindowOps)
	}

	data, err := os.ReadFile(filepath.Join(paths.DataDir, "tools", "test-session"))
	if err != nil {
		t.Fatalf("failed to read tools file: %v", err)
	}
	content := string(data)
	for _, want := range []string{"1: @editor", "2: cargo test", "3: @shell"} {
		if !strings.Contains(content, want) {
			t.Fatalf("tools file missing %q; content: %s", want, content)
		}
	}
}

func TestUIInitToolsLoadFromSeshTomlWritesDefaultSlots(t *testing.T) {
	paths, tmpDir := setupTestPaths(t)
	defer os.RemoveAll(tmpDir)
	t.Setenv("TMUX", "")

	var tmuxWindowOps []string
	runner := &MockRunner{
		RunFunc: func(name string, args []string, stdin io.Reader) ([]byte, error) {
			if name == "tmux" && len(args) > 0 {
				switch args[0] {
				case "new-window", "kill-window", "select-window":
					tmuxWindowOps = append(tmuxWindowOps, strings.Join(args, " "))
				}
			}
			return []byte(""), nil
		},
		RunInteractiveFunc: func(name string, args []string, stdin io.Reader, stdout io.Writer, stderr io.Writer) error {
			if name != "gum" || len(args) == 0 {
				return nil
			}
			if args[0] == "style" {
				return nil
			}
			if args[0] != "choose" {
				t.Fatalf("unexpected gum command: %v", args)
			}
			optionsBytes, _ := io.ReadAll(stdin)
			options := string(optionsBytes)
			if !strings.Contains(options, "Load from sesh.toml") {
				t.Fatalf("initialize menu missing sesh.toml option: %q", options)
			}
			_, _ = stdout.Write([]byte("Load from sesh.toml\n"))
			return nil
		},
	}

	app := &App{
		Paths:  paths,
		Runner: runner,
		Config: defaultConfig(),
		SeshConfig: SeshConfig{
			DefaultSession: DefaultSessionConfig{Windows: []string{"editor", "shell"}},
		},
		Stdout: io.Discard,
		Stderr: io.Discard,
	}

	initialized, err := app.uiInitTools("test-session")
	if err != nil {
		t.Fatalf("uiInitTools failed: %v", err)
	}
	if !initialized {
		t.Fatal("expected tools to be initialized")
	}
	if len(tmuxWindowOps) > 0 {
		t.Fatalf("sesh.toml initialization should not bootstrap tmux windows, got ops: %v", tmuxWindowOps)
	}

	data, err := os.ReadFile(filepath.Join(paths.DataDir, "tools", "test-session"))
	if err != nil {
		t.Fatalf("failed to read tools file: %v", err)
	}
	content := string(data)
	for _, want := range []string{"# auto-generated from sesh.toml default_session", "1: @editor", "2: @shell"} {
		if !strings.Contains(content, want) {
			t.Fatalf("tools file missing %q; content: %s", want, content)
		}
	}
}

func TestUIEditToolsReconcilesStaleAttached(t *testing.T) {
	paths, tmpDir := setupTestPaths(t)
	defer os.RemoveAll(tmpDir)
	t.Setenv("TMUX", "")

	toolsFile := filepath.Join(paths.DataDir, "tools", "my-session")
	if err := os.MkdirAll(filepath.Dir(toolsFile), 0755); err != nil {
		t.Fatalf("failed to create tools dir: %v", err)
	}
	// Slot 1: dead @attached (index 88 absent). Slot 2: persistent command.
	if err := os.WriteFile(toolsFile, []byte("1: @attached\n2: nvim .\n"), 0600); err != nil {
		t.Fatalf("failed to seed tools file: %v", err)
	}

	runner := &MockRunner{
		RunFunc: func(name string, args []string, stdin io.Reader) ([]byte, error) {
			if name == "tmux" {
				switch args[0] {
				case "display-message":
					// Session lookup (the loop queries #S each iteration).
					if len(args) == 3 && args[2] == "#S" {
						return []byte("my-session\n"), nil
					}
					// Attached window-name probe (must NOT be issued once reconciled).
					if len(args) == 5 && args[3] == ":88" && args[4] == "#{window_name}" {
						return nil, fmt.Errorf("window 88 does not exist")
					}
				case "list-windows":
					// Window index 88 (slot 1) is gone; only window 5 exists.
					return []byte("5\n"), nil
				}
			}
			return []byte(""), nil
		},
		RunInteractiveFunc: func(name string, args []string, stdin io.Reader, stdout io.Writer, stderr io.Writer) error {
			if name != "gum" || len(args) == 0 {
				return nil
			}
			if args[0] == "style" {
				return nil
			}
			if args[0] == "choose" {
				// Render then immediately exit the editor.
				options, _ := io.ReadAll(stdin)
				if !strings.Contains(string(options), "Label windows") {
					t.Fatalf("tools actions missing Label windows: %q", options)
				}
				_, _ = stdout.Write([]byte("Done\n"))
				return nil
			}
			return nil
		},
	}
	var stdout bytes.Buffer
	app := &App{Paths: paths, Runner: runner, Config: defaultConfig(), Stdout: &stdout, Stderr: io.Discard}

	if err := app.uiEditTools(); err != nil {
		t.Fatalf("uiEditTools failed: %v", err)
	}

	data, err := os.ReadFile(toolsFile)
	if err != nil {
		t.Fatalf("failed to read tools file: %v", err)
	}
	content := string(data)
	if strings.Contains(content, "1: @attached") {
		t.Errorf("stale @attached slot 1 should be reclaimed on editor open, got: %s", content)
	}
	if !strings.Contains(content, "2: nvim .") {
		t.Errorf("slot 2 should be preserved, got: %s", content)
	}
	if strings.Contains(stdout.String(), "empty\n\n") {
		t.Error("tools overview should not add a spacer that pushes popup metadata off-screen")
	}
}

func TestParseSlotFromOption(t *testing.T) {
	tests := []struct {
		opt     string
		want    int
		wantErr bool
	}{
		{"1: my-session", 1, false},
		{"9: (empty)", 9, false},
		{"\U000f0752 5: session-name", 5, false},
		{"\U000f0751 2: (empty)", 2, false},
		{"\uf155 @3: custom-command", 3, false},
		{"\ue795 @4: @shell", 4, false},
		{"@7: attached", 7, false},
		{"invalid", 0, true},
		{": empty-prefix", 0, true},
	}
	for _, tc := range tests {
		got, err := parseSlotFromOption(tc.opt)
		if (err != nil) != tc.wantErr {
			t.Errorf("parseSlotFromOption(%q) error = %v, wantErr = %v", tc.opt, err, tc.wantErr)
		}
		if err == nil && got != tc.want {
			t.Errorf("parseSlotFromOption(%q) got = %d, want = %d", tc.opt, got, tc.want)
		}
	}
}

func TestCmdBootstrapKillOwnWindow(t *testing.T) {
	paths, tmpDir := setupTestPaths(t)
	defer os.RemoveAll(tmpDir)

	// Set TMUX_PANE so we simulate running inside a tmux pane
	os.Setenv("TMUX_PANE", "%12")
	defer os.Unsetenv("TMUX_PANE")

	var tmuxCalls []string
	var killedWindows []string

	runner := &MockRunner{
		RunFunc: func(name string, args []string, stdin io.Reader) ([]byte, error) {
			if name == "tmux" {
				tmuxCalls = append(tmuxCalls, strings.Join(args, " "))
				switch args[0] {
				case "display-message":
					// If checking session name
					if args[2] == "#S" {
						return []byte("test-session\n"), nil
					}
					// If checking current window_id from %12
					if len(args) > 4 && args[4] == "#{window_id}" && args[3] == "%12" {
						return []byte("@1\n"), nil
					}
					// If checking target window index 1 -> @1
					if len(args) > 4 && args[4] == "#{window_id}" && args[3] == ":1" {
						return []byte("@1\n"), nil
					}
					// If checking pane_id or pane info
					if len(args) > 4 && args[4] == "#{pane_id} #{pane_current_command} #{pane_pid}" {
						return []byte("%12 bash 12345\n"), nil
					}
				case "list-windows":
					// Audit list-windows or clean list-windows
					if len(args) > 2 && args[2] == "#{window_index} #{window_panes}" {
						return []byte("1 1\n"), nil
					}
					return []byte("1\n88\n"), nil
				case "new-window":
					return []byte("@99\n"), nil
				case "kill-window":
					for idx, arg := range args {
						if arg == "-t" && idx+1 < len(args) {
							killedWindows = append(killedWindows, args[idx+1])
						}
					}
					return []byte(""), nil
				}
			} else if name == "pgrep" {
				return []byte(""), nil
			}
			return []byte(""), nil
		},
	}

	app := &App{
		Paths:  paths,
		Runner: runner,
		Config: Config{
			Presets: map[string][]string{
				"test-preset": {"nvim ."},
			},
			ToolWindowBase: 88,
		},
		Stdout: io.Discard,
		Stderr: io.Discard,
	}

	err := app.cmdBootstrap("test-preset", false, false)
	if err != nil {
		t.Fatalf("cmdBootstrap failed: %v", err)
	}

	// Verify that @1 was killed
	hasKilledOwn := false
	for _, kw := range killedWindows {
		if kw == "@1" || kw == ":1" {
			hasKilledOwn = true
		}
	}
	if !hasKilledOwn {
		t.Errorf("expected own window (@1) to be killed, but got: %v", killedWindows)
	}

	// Verify order: own window should be killed last (after select-window -t :88)
	selectIdx := -1
	killIdx := -1
	for idx, call := range tmuxCalls {
		if strings.Contains(call, "select-window -t :88") {
			selectIdx = idx
		}
		if strings.Contains(call, "kill-window -t @1") || strings.Contains(call, "kill-window -t :1") {
			killIdx = idx
		}
	}

	if selectIdx == -1 {
		t.Error("expected select-window -t :88 to be called")
	}
	if killIdx == -1 {
		t.Error("expected own window to be killed")
	}
	if killIdx < selectIdx {
		t.Errorf("expected own window to be killed after select-window, selectIdx=%d, killIdx=%d", selectIdx, killIdx)
	}
}

func TestToolRemoveRenamesAndRenumbersWindow(t *testing.T) {
	paths, tmpDir := setupTestPaths(t)
	defer os.RemoveAll(tmpDir)

	var tmuxCommands [][]string
	runner := &MockRunner{
		RunFunc: func(name string, args []string, stdin io.Reader) ([]byte, error) {
			if name == "tmux" {
				tmuxCommands = append(tmuxCommands, args)
				switch args[0] {
				case "display-message":
					if len(args) == 5 && args[2] == "-t" {
						switch args[3] {
						case "my-session:90":
							return []byte("@tool\x1f90\x1f@o my-tool-window\x1f@o\x1f0\n"), nil
						case "@tool":
							return []byte("@tool\x1f2\x1f@o my-tool-window\x1f@o\x1f0\n"), nil
						}
					}
				case "show-options":
					// show-options -t my-session -v base-index
					if len(args) == 5 && args[1] == "-t" && args[2] == "my-session" && args[4] == "base-index" {
						return []byte("1\n"), nil
					}
				case "list-windows":
					// list-windows -t my-session -F #{window_index}
					// occupied indexes: 1, 3, 90
					// lowest available should be 2
					if len(args) == 5 && args[2] == "my-session" && args[4] == "#{window_index}" {
						return []byte("1\n3\n90\n"), nil
					}
				}
			}
			return []byte(""), nil
		},
	}

	app := &App{
		Paths:  paths,
		Runner: runner,
		Config: defaultConfig(), // ToolWindowBase = 88, prefixes = @/#
		Stdout: io.Discard,
		Stderr: io.Discard,
	}

	// Setup: create tools file
	toolsFile := filepath.Join(paths.DataDir, "tools", "my-session")
	os.MkdirAll(filepath.Dir(toolsFile), 0755)
	err := os.WriteFile(toolsFile, []byte("3: my-tool-cmd\n"), 0600)
	if err != nil {
		t.Fatalf("failed to write tools file: %v", err)
	}

	err = app.toolRemove("my-session", 3)
	if err != nil {
		t.Fatalf("toolRemove failed: %v", err)
	}

	// Verify the tool was removed from file
	_, err = app.getTool("my-session", 3)
	if err == nil {
		t.Error("expected tool slot 3 to be removed from file")
	}

	// Verify tmux commands were executed
	foundRename := false
	foundMove := false
	for _, cmd := range tmuxCommands {
		if reflect.DeepEqual(cmd, []string{"rename-window", "-t", "@tool", "#2 my-tool-window"}) {
			foundRename = true
		}
		if reflect.DeepEqual(cmd, []string{"move-window", "-d", "-s", "my-session:90", "-t", "my-session:2"}) {
			foundMove = true
		}
	}

	if !foundRename {
		t.Errorf("expected tmux rename-window command to be run, got commands: %v", tmuxCommands)
	}
	if !foundMove {
		t.Errorf("expected tmux move-window command to be run, got commands: %v", tmuxCommands)
	}
}

func TestToolRemoveRenamesAndRenumbersWindowFallbackBaseIndex(t *testing.T) {
	paths, tmpDir := setupTestPaths(t)
	defer os.RemoveAll(tmpDir)

	var tmuxCommands [][]string
	runner := &MockRunner{
		RunFunc: func(name string, args []string, stdin io.Reader) ([]byte, error) {
			if name == "tmux" {
				tmuxCommands = append(tmuxCommands, args)
				switch args[0] {
				case "display-message":
					if len(args) == 5 && args[2] == "-t" {
						switch args[3] {
						case "my-session:90":
							return []byte("@tool\x1f90\x1f@o my-tool-window\x1f@o\x1f0\n"), nil
						case "@tool":
							return []byte("@tool\x1f2\x1f@o my-tool-window\x1f@o\x1f0\n"), nil
						}
					}
				case "show-options":
					// show-options -t my-session -v base-index (returns empty string since it is inherited)
					if len(args) == 5 && args[1] == "-t" && args[2] == "my-session" && args[4] == "base-index" {
						return []byte(""), nil
					}
					// show-options -gv base-index (returns global config value "1")
					if len(args) == 3 && args[1] == "-gv" && args[2] == "base-index" {
						return []byte("1\n"), nil
					}
				case "list-windows":
					// list-windows -t my-session
					if len(args) == 5 && args[2] == "my-session" && args[4] == "#{window_index}" {
						return []byte("1\n3\n90\n"), nil
					}
				}
			}
			return []byte(""), nil
		},
	}

	app := &App{
		Paths:  paths,
		Runner: runner,
		Config: defaultConfig(),
		Stdout: io.Discard,
		Stderr: io.Discard,
	}

	// Setup: create tools file
	toolsFile := filepath.Join(paths.DataDir, "tools", "my-session")
	os.MkdirAll(filepath.Dir(toolsFile), 0755)
	err := os.WriteFile(toolsFile, []byte("3: my-tool-cmd\n"), 0600)
	if err != nil {
		t.Fatalf("failed to write tools file: %v", err)
	}

	err = app.toolRemove("my-session", 3)
	if err != nil {
		t.Fatalf("toolRemove failed: %v", err)
	}

	// Verify the tool was removed from file
	_, err = app.getTool("my-session", 3)
	if err == nil {
		t.Error("expected tool slot 3 to be removed from file")
	}

	// Verify it fell back to global base-index of 1, and moved window to 2
	foundMove := false
	for _, cmd := range tmuxCommands {
		if reflect.DeepEqual(cmd, []string{"move-window", "-d", "-s", "my-session:90", "-t", "my-session:2"}) {
			foundMove = true
		}
	}

	if !foundMove {
		t.Errorf("expected tmux move-window command to be run targeting index 2 (falling back to global base-index 1), got commands: %v", tmuxCommands)
	}
}

func TestCmdBootstrapMovesRunnerWindowIfInToolRange(t *testing.T) {
	paths, tmpDir := setupTestPaths(t)
	defer os.RemoveAll(tmpDir)

	os.Setenv("TMUX_PANE", "%12")
	defer os.Unsetenv("TMUX_PANE")

	var tmuxCalls []string
	runner := &MockRunner{
		RunFunc: func(name string, args []string, stdin io.Reader) ([]byte, error) {
			if name == "tmux" {
				tmuxCalls = append(tmuxCalls, strings.Join(args, " "))
				switch args[0] {
				case "display-message":
					// Session name
					if args[2] == "#S" {
						return []byte("test-session\n"), nil
					}
					// Runner window index/id probe
					if len(args) == 5 && args[3] == "%12" {
						if args[4] == "#{window_index}" {
							return []byte("88\n"), nil
						}
						if args[4] == "#{window_id}" {
							return []byte("@12\n"), nil
						}
					}
				case "show-options":
					// base-index query
					return []byte("1\n"), nil
				case "list-windows":
					if len(args) == 5 && args[2] == "test-session" && args[4] == "#{window_index}" {
						return []byte("1\n88\n"), nil
					}
					return []byte("1\n88\n"), nil
				case "new-window":
					return []byte("@99\n"), nil
				}
			}
			return []byte(""), nil
		},
	}

	app := &App{
		Paths:  paths,
		Runner: runner,
		Config: Config{
			Presets: map[string][]string{
				"test-preset": {"nvim ."},
			},
			ToolWindowBase: 88,
		},
		Stdout: io.Discard,
		Stderr: io.Discard,
	}

	err := app.cmdBootstrap("test-preset", false, false)
	if err != nil {
		t.Fatalf("cmdBootstrap failed: %v", err)
	}

	// Verify that the runner window was moved (move-window -d -s test-session:88 -t test-session:2)
	foundMove := false
	for _, call := range tmuxCalls {
		if strings.Contains(call, "move-window -d -s test-session:88 -t test-session:2") {
			foundMove = true
		}
	}
	if !foundMove {
		t.Errorf("expected runner window to be moved out of tool slot range, got calls: %v", tmuxCalls)
	}
}

func TestCmdBootstrapKeepStrategyDoesNotKillWindows(t *testing.T) {
	paths, tmpDir := setupTestPaths(t)
	defer os.RemoveAll(tmpDir)

	os.Setenv("TMUX_PANE", "%12")
	defer os.Unsetenv("TMUX_PANE")

	var killedWindows []string
	runner := &MockRunner{
		RunFunc: func(name string, args []string, stdin io.Reader) ([]byte, error) {
			if name == "tmux" {
				switch args[0] {
				case "display-message":
					if args[2] == "#S" {
						return []byte("test-session\n"), nil
					}
					if len(args) == 5 && args[3] == "%12" && args[4] == "#{window_id}" {
						return []byte("@12\n"), nil
					}
				case "list-windows":
					return []byte("1\n88\n"), nil
				case "new-window":
					return []byte("@99\n"), nil
				case "kill-window":
					for idx, arg := range args {
						if arg == "-t" && idx+1 < len(args) {
							killedWindows = append(killedWindows, args[idx+1])
						}
					}
				}
			}
			return []byte(""), nil
		},
	}

	app := &App{
		Paths:  paths,
		Runner: runner,
		Config: Config{
			Presets: map[string][]string{
				"test-preset": {"nvim ."},
			},
			ToolWindowBase: 88,
		},
		Stdout: io.Discard,
		Stderr: io.Discard,
	}

	err := app.cmdBootstrap("test-preset", false, true)
	if err != nil {
		t.Fatalf("cmdBootstrap failed: %v", err)
	}

	// Verify that the temp window was killed, but NO other windows (like @12 or index 1) were killed
	for _, kw := range killedWindows {
		if kw != "@99" && kw != "shunpo-boot-tmp" {
			t.Errorf("expected only temp window @99 to be killed, but got killed window: %s", kw)
		}
	}
}
