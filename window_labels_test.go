package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestPlanWindowLabel(t *testing.T) {
	tests := []struct {
		name     string
		current  string
		recorded string
		expected string
		want     string
	}{
		{
			name:     "adds label to manual name",
			current:  "opus auth",
			expected: "@u",
			want:     "@u opus auth",
		},
		{
			name:     "adopts exact expected label without metadata",
			current:  "@u opus auth",
			expected: "@u",
			want:     "@u opus auth",
		},
		{
			name:     "replaces recorded label after remap",
			current:  "@u opus auth",
			recorded: "@u",
			expected: "@o",
			want:     "@o opus auth",
		},
		{
			name:     "does not treat raw prefix as label",
			current:  "@user investigation",
			recorded: "@u",
			expected: "@u",
			want:     "@u @user investigation",
		},
		{
			name:     "preserves manually entered different label",
			current:  "@i experimental",
			recorded: "@u",
			expected: "@u",
			want:     "@u @i experimental",
		},
		{
			name:     "supports empty descriptor",
			expected: "#1",
			want:     "#1",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := planWindowLabel(tc.current, tc.recorded, tc.expected); got != tc.want {
				t.Fatalf("planWindowLabel() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestParseDirectToolBinding(t *testing.T) {
	tests := []struct {
		command  string
		wantSlot int
		wantOK   bool
	}{
		{`run-shell "tmux-shunpo --goto @1"`, 1, true},
		{`run-shell "/Users/me/bin/tmux-shunpo --goto @6"`, 6, true},
		{`run-shell "wrapper tmux-shunpo --goto @1"`, 0, false},
		{`run-shell "tmux-shunpo --goto @1 && echo done"`, 0, false},
		{`run-shell "tmux-shunpo --goto 1"`, 0, false},
		{`run-shell "tmux-shunpo --goto @0"`, 0, false},
		{`select-window -t :88`, 0, false},
	}

	for _, tc := range tests {
		t.Run(tc.command, func(t *testing.T) {
			gotSlot, gotOK := parseDirectToolBinding(tc.command)
			if gotSlot != tc.wantSlot || gotOK != tc.wantOK {
				t.Fatalf("parseDirectToolBinding() = (%d, %t), want (%d, %t)", gotSlot, gotOK, tc.wantSlot, tc.wantOK)
			}
		})
	}
}

func TestToolBindingMapUsesUniquePhysicalKey(t *testing.T) {
	bindings := toolBindingMap{keys: map[int][]string{
		1: {"u"},
		2: {"i", "2"},
	}}

	if key, warning := bindings.keyForSlot(1); key != "u" || warning != "" {
		t.Fatalf("unique binding = (%q, %q), want (u, empty)", key, warning)
	}
	if key, warning := bindings.keyForSlot(2); key != "2" || !strings.Contains(warning, "multiple") {
		t.Fatalf("ambiguous binding = (%q, %q), want logical fallback with warning", key, warning)
	}
	if key, warning := bindings.keyForSlot(3); key != "3" || !strings.Contains(warning, "no physical binding") {
		t.Fatalf("missing binding = (%q, %q), want logical fallback with warning", key, warning)
	}

	failed := toolBindingMap{discoveryErr: fmt.Errorf("tmux unavailable")}
	if key, warning := failed.keyForSlot(4); key != "4" || !strings.Contains(warning, "could not detect") {
		t.Fatalf("failed discovery = (%q, %q), want logical fallback with warning", key, warning)
	}
}

func TestLoadConfigWindowPrefixes(t *testing.T) {
	paths, tmpDir := setupTestPaths(t)
	defer os.RemoveAll(tmpDir)
	if err := os.MkdirAll(paths.ConfigDir, 0755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(paths.ConfigFile, []byte("tool_window_prefix = \"⚡\"\nnormal_window_prefix = \"§\"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := loadConfig(paths.ConfigFile, io.Discard)
	if err != nil {
		t.Fatalf("loadConfig failed: %v", err)
	}
	if cfg.ToolWindowPrefix != "⚡" || cfg.NormalWindowPrefix != "§" {
		t.Fatalf("prefixes = %q/%q, want ⚡/§", cfg.ToolWindowPrefix, cfg.NormalWindowPrefix)
	}
}

func TestLoadConfigAcceptsLegacyPrefixSeparator(t *testing.T) {
	paths, tmpDir := setupTestPaths(t)
	defer os.RemoveAll(tmpDir)
	if err := os.MkdirAll(paths.ConfigDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths.ConfigFile, []byte("tool_window_prefix = \"⚡ \"\nnormal_window_prefix = \"#\"\n"), 0600); err != nil {
		t.Fatal(err)
	}

	var stderr strings.Builder
	cfg, err := loadConfig(paths.ConfigFile, &stderr)
	if err != nil {
		t.Fatalf("previously valid prefix should load: %v", err)
	}
	if cfg.ToolWindowPrefix != "⚡" {
		t.Fatalf("tool prefix = %q, want surrounding separator removed", cfg.ToolWindowPrefix)
	}
	if !strings.Contains(stderr.String(), "surrounding whitespace") {
		t.Fatalf("expected normalization warning, got %q", stderr.String())
	}
}

func TestLoadConfigRejectsInvalidWindowPrefixes(t *testing.T) {
	tests := []struct {
		name    string
		content string
	}{
		{"empty", "tool_window_prefix = \"\"\n"},
		{"same", "tool_window_prefix = \"#\"\nnormal_window_prefix = \"#\"\n"},
		{"embedded whitespace", "normal_window_prefix = \"# x\"\n"},
		{"control character", "normal_window_prefix = \"#\\t\"\n"},
		{"wrong type", "normal_window_prefix = 3\n"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			paths, tmpDir := setupTestPaths(t)
			defer os.RemoveAll(tmpDir)
			if err := os.MkdirAll(paths.ConfigDir, 0755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(paths.ConfigFile, []byte(tc.content), 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := loadConfig(paths.ConfigFile, io.Discard); err == nil {
				t.Fatal("loadConfig should reject invalid prefixes")
			}
		})
	}
}

func TestToolOverviewFormatting(t *testing.T) {
	rows := []toolOverviewRow{
		{Icon: "\U000f1a7c", Slot: 1, Window: "@u nvim", Source: "@editor → nvim -S"},
		{Icon: "\ue795", Slot: 2, Window: "@i —", Source: "@shell → zsh"},
		{Icon: "\uf0c6", Slot: 3, Window: "@o pi-trunk", Source: "@attached"},
		{Icon: "\uf155", Slot: 4, Window: "@p pi-review", Source: "pi --no-session"},
		{Icon: "\U000f0751", Slot: 5, Window: "—", Source: "empty"},
	}

	lines := strings.Split(strings.TrimSuffix(formatToolOverview(rows, 78), "\n"), "\n")
	if len(lines) != len(rows)+1 {
		t.Fatalf("overview has %d lines, want %d: %q", len(lines), len(rows)+1, lines)
	}
	runeColumn := func(line, value string) int {
		index := strings.Index(line, value)
		if index < 0 {
			return -1
		}
		return len([]rune(line[:index]))
	}
	slotColumn := runeColumn(lines[0], "SLOT")
	windowColumn := runeColumn(lines[0], "WINDOW")
	sourceColumn := runeColumn(lines[0], "SOURCE")
	if slotColumn < 0 || windowColumn < 0 || sourceColumn < 0 {
		t.Fatalf("overview header is incomplete: %q", lines[0])
	}
	for i, line := range lines[1:] {
		if runeColumn(line, fmt.Sprintf("@%d", rows[i].Slot)) != slotColumn {
			t.Fatalf("slot column is not aligned in %q", line)
		}
		if runeColumn(line, rows[i].Window) != windowColumn {
			t.Fatalf("window column is not aligned in %q", line)
		}
		if runeColumn(line, rows[i].Source) != sourceColumn {
			t.Fatalf("source column is not aligned in %q", line)
		}
	}

	longRows := []toolOverviewRow{{
		Icon:   "\uf155",
		Slot:   1,
		Window: strings.Repeat("window", 10),
		Source: strings.Repeat("command ", 20),
	}}
	for _, line := range strings.Split(strings.TrimSuffix(formatToolOverview(longRows, 78), "\n"), "\n") {
		if width := len([]rune(line)); width > 76 {
			t.Fatalf("formatted line width = %d, want at most 76: %q", width, line)
		}
	}
}

func TestToolOverviewKindsAndSources(t *testing.T) {
	templates := map[string]string{"editor": "nvim -S", "shell": "zsh"}
	tests := []struct {
		command string
		icon    string
		source  string
	}{
		{"@editor", "\U000f1a7c", "@editor → nvim -S"},
		{"@shell", "\ue795", "@shell → zsh"},
		{"@attached", "\uf0c6", "@attached"},
		{"pi --no-session", "\uf155", "pi --no-session"},
		{"", "\U000f0751", "empty"},
	}
	for _, tc := range tests {
		t.Run(tc.command, func(t *testing.T) {
			if got := toolOverviewIcon(tc.command, true); got != tc.icon {
				t.Fatalf("icon = %q, want %q", got, tc.icon)
			}
			if got := toolOverviewSource(tc.command, templates); got != tc.source {
				t.Fatalf("source = %q, want %q", got, tc.source)
			}
		})
	}
}

func TestToolOverviewWindowUsesLiveNameOrClosedMarker(t *testing.T) {
	app := &App{
		Config: defaultConfig(),
		Runner: &MockRunner{RunFunc: func(name string, args []string, stdin io.Reader) ([]byte, error) {
			if name == "tmux" && len(args) == 5 && args[3] == "work:88" {
				return []byte("@live\x1f88\x1f@u nvim\x1f@u\x1f0\n"), nil
			}
			return nil, fmt.Errorf("window missing")
		}},
	}
	if got := app.toolOverviewWindow("work", 1, "@u"); got != "@u nvim" {
		t.Fatalf("live window = %q, want exact tmux name", got)
	}
	if got := app.toolOverviewWindow("work", 2, "@i"); got != "@i —" {
		t.Fatalf("closed window = %q, want physical key plus closed marker", got)
	}
}

func TestZshCompletionUsesValidDoctorPosition(t *testing.T) {
	completion := completionZsh()
	if strings.Contains(completion, "'(-)doctor[") {
		t.Fatal("zsh completion must not pass bare doctor as an _arguments option spec")
	}
	if !strings.Contains(completion, "'1:command:(doctor)'") {
		t.Fatal("zsh completion must expose doctor as a positional command")
	}
	if !strings.Contains(completion, "'--label-windows[Label current-session windows]'") {
		t.Fatal("zsh completion must expose --label-windows")
	}
}

func TestCmdLabelWindows(t *testing.T) {
	paths, tmpDir := setupTestPaths(t)
	defer os.RemoveAll(tmpDir)
	t.Setenv("TMUX", "1")

	toolsFile := filepath.Join(paths.DataDir, "tools", "work")
	if err := os.MkdirAll(filepath.Dir(toolsFile), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(toolsFile, []byte("1: pi\n"), 0600); err != nil {
		t.Fatal(err)
	}

	type liveWindow struct {
		index  int
		name   string
		label  string
		linked bool
	}
	windows := map[string]*liveWindow{
		"@normal": {index: 1, name: "editor"},
		"@tool":   {index: 88, name: "opus auth"},
		"@linked": {index: 2, name: "shared", linked: true},
	}
	var notifications []string

	runner := &MockRunner{RunFunc: func(name string, args []string, stdin io.Reader) ([]byte, error) {
		if name != "tmux" {
			return nil, nil
		}
		switch args[0] {
		case "display-message":
			if len(args) == 3 && args[2] == "#S" {
				return []byte("work\n"), nil
			}
			if len(args) == 2 {
				notifications = append(notifications, args[1])
				return nil, nil
			}
			if len(args) == 5 {
				window, ok := windows[args[3]]
				if !ok {
					return nil, fmt.Errorf("missing window")
				}
				linked := "0"
				if window.linked {
					linked = "1"
				}
				return []byte(strings.Join([]string{args[3], fmt.Sprint(window.index), window.name, window.label, linked}, windowFieldSep) + "\n"), nil
			}
		case "list-keys":
			return []byte("root:::M-u:::run-shell \"tmux-shunpo --goto @1\"\n"), nil
		case "list-windows":
			return []byte("@normal\n@tool\n@linked\n"), nil
		case "rename-window":
			window := windows[args[2]]
			window.name = args[3]
			return nil, nil
		case "set-option":
			window := windows[args[3]]
			window.label = args[5]
			return nil, nil
		}
		return nil, nil
	}}

	app := &App{
		Paths:  paths,
		Config: defaultConfig(),
		Runner: runner,
		Stdout: io.Discard,
		Stderr: io.Discard,
	}
	if err := app.cmdLabelWindows(); err != nil {
		t.Fatalf("cmdLabelWindows failed: %v", err)
	}
	if err := app.cmdLabelWindows(); err != nil {
		t.Fatalf("idempotent cmdLabelWindows failed: %v", err)
	}

	if got, want := windows["@normal"].name, "#1 editor"; got != want {
		t.Fatalf("normal name = %q, want %q", got, want)
	}
	if got, want := windows["@tool"].name, "@u opus auth"; got != want {
		t.Fatalf("tool name = %q, want %q", got, want)
	}
	if got, want := windows["@linked"].name, "shared"; got != want {
		t.Fatalf("linked name = %q, want unchanged %q", got, want)
	}
	if got := []string{windows["@normal"].label, windows["@tool"].label}; !reflect.DeepEqual(got, []string{"#1", "@u"}) {
		t.Fatalf("recorded labels = %v, want [#1 @u]", got)
	}
	wantNotification := "Labeled windows; warning: window @linked is linked and was skipped"
	if !reflect.DeepEqual(notifications, []string{wantNotification, wantNotification}) {
		t.Fatalf("notifications = %v, want one confirmation per run", notifications)
	}
}
