package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"unicode"

	"github.com/pelletier/go-toml/v2"
)

type Paths struct {
	ConfigDir        string
	ConfigFile       string
	DataDir          string
	MarksFile        string
	SessionStateFile string
	SeshConfigFile   string
}

type Config struct {
	ToolWindowBase      int                 `toml:"tool_window_base"`
	ShellInitDelay      float64             `toml:"shell_init_delay"`
	WindowNameMaxLength int                 `toml:"window_name_max_length"`
	ToolWindowPrefix    string              `toml:"tool_window_prefix"`
	NormalWindowPrefix  string              `toml:"normal_window_prefix"`
	Presets             map[string][]string `toml:"presets"`
	UI                  UIConfig            `toml:"ui"`
	Guardrails          GuardrailsConfig    `toml:"guardrails"`
}

type UIConfig struct {
	PopupWidth       string `toml:"popup_width"`
	PopupHeight      string `toml:"popup_height"`
	PopupMinWidth    int    `toml:"popup_min_width"`
	PopupMinHeight   int    `toml:"popup_min_height"`
	PopupBorderLines string `toml:"popup_border_lines"`
	PopupBorderStyle string `toml:"popup_border_style"`
	PopupStyle       string `toml:"popup_style"`
	UseNerdFonts     bool   `toml:"use_nerd_fonts"`
}

// GuardrailsConfig controls the optional "this looks destructive" confirmation
// shown when a tool command is set interactively. It is a convenience to catch
// typos and bad pastes — NOT a security boundary. Commands run in the user's own
// shell either way, and commands defined in presets / sesh.toml are not checked.
type GuardrailsConfig struct {
	ConfirmDestructive bool     `toml:"confirm_destructive"`
	AlsoConfirm        []string `toml:"also_confirm"`
	SkipConfirm        []string `toml:"skip_confirm"`
}

// builtinDestructiveCommands is the default set of unambiguously-catastrophic
// commands that trigger a confirmation prompt when set interactively. Routine
// dev commands (cp, mv, sed, chmod, ...) are intentionally excluded. It is a
// cross-platform union: entries that don't exist on a given OS simply never
// match (e.g. mkswap on macOS, diskutil on Linux).
var builtinDestructiveCommands = []string{
	// disk / filesystem destroyers (Linux)
	"rm", "dd", "shred", "mkfs", "fdisk", "parted", "mkswap", "wipefs",
	// disk / filesystem destroyers (macOS)
	"diskutil", "newfs", "asr", "srm",
	// system power
	"shutdown", "reboot", "poweroff", "halt",
}

type SeshConfig struct {
	DefaultSession DefaultSessionConfig `toml:"default_session"`
	Session        []SessionConfig      `toml:"session"`
	Wildcard       []WildcardConfig     `toml:"wildcard"`
	Window         []WindowConfig       `toml:"window"`
}

type DefaultSessionConfig struct {
	Windows []string `toml:"windows"`
}

type SessionConfig struct {
	Name    string   `toml:"name"`
	Path    string   `toml:"path"`
	Windows []string `toml:"windows"`
}

type WildcardConfig struct {
	Pattern string   `toml:"pattern"`
	Windows []string `toml:"windows"`
}

type WindowConfig struct {
	Name          string `toml:"name"`
	StartupScript string `toml:"startup_script"`
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func resolvePaths() Paths {
	home, err := os.UserHomeDir()
	if err != nil {
		home = os.Getenv("HOME")
	}

	configDir := getEnv("CONFIG_DIR", filepath.Join(home, ".config", "tmux-shunpo"))
	configFile := getEnv("CONFIG_FILE", filepath.Join(configDir, "config.toml"))
	dataDir := getEnv("DATA_DIR", filepath.Join(home, ".local", "share", "tmux-shunpo"))
	marksFile := getEnv("MARKS_FILE", filepath.Join(dataDir, "marks"))
	sessionStateFile := getEnv("SESSION_STATE_FILE", filepath.Join(dataDir, "session_state"))

	xdgConfig := os.Getenv("XDG_CONFIG_HOME")
	if xdgConfig == "" {
		xdgConfig = filepath.Join(home, ".config")
	}
	seshConfigFile := getEnv("SESH_CONFIG", filepath.Join(xdgConfig, "sesh", "sesh.toml"))

	return Paths{
		ConfigDir:        configDir,
		ConfigFile:       configFile,
		DataDir:          dataDir,
		MarksFile:        marksFile,
		SessionStateFile: sessionStateFile,
		SeshConfigFile:   seshConfigFile,
	}
}

func checkFileSafety(filePath string) error {
	info, err := os.Stat(filePath)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}

	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return nil
	}

	// Check ownership
	currentUID := os.Getuid()
	if int(stat.Uid) != currentUID {
		if resolved, err := filepath.EvalSymlinks(filePath); err == nil {
			if strings.HasPrefix(resolved, "/nix/store/") {
				return nil
			}
		}
		return fmt.Errorf("config file owned by another user: %s", filePath)
	}

	// Check permissions (not world-writable: write bit for other users must not be set)
	// Other users write permission is 0002.
	if info.Mode().Perm()&0002 != 0 {
		return fmt.Errorf("config file is world-writable: %s (mode %o)", filePath, info.Mode().Perm())
	}

	return nil
}

func normalizeWindowPrefix(name, prefix string, stderr io.Writer) string {
	trimmed := strings.TrimFunc(prefix, func(r rune) bool {
		return unicode.IsSpace(r) && !unicode.IsControl(r)
	})
	if trimmed != prefix {
		fmt.Fprintf(stderr, "warning: %s has surrounding whitespace; ignoring it because window labels add their own separator\n", name)
	}
	return trimmed
}

func validateWindowPrefixes(toolPrefix, normalPrefix string) error {
	if toolPrefix == "" || normalPrefix == "" {
		return fmt.Errorf("tool_window_prefix and normal_window_prefix must be non-empty")
	}
	if toolPrefix == normalPrefix {
		return fmt.Errorf("tool_window_prefix and normal_window_prefix must be distinct")
	}
	for _, candidate := range []struct {
		name   string
		prefix string
	}{
		{name: "tool_window_prefix", prefix: toolPrefix},
		{name: "normal_window_prefix", prefix: normalPrefix},
	} {
		for _, r := range candidate.prefix {
			if unicode.IsSpace(r) || unicode.IsControl(r) {
				return fmt.Errorf("%s must not contain internal whitespace or control characters", candidate.name)
			}
		}
	}
	return nil
}

func defaultConfig() Config {
	return Config{
		ToolWindowBase:      88,
		ShellInitDelay:      0.2,
		WindowNameMaxLength: 20,
		ToolWindowPrefix:    "@",
		NormalWindowPrefix:  "#",
		UI: UIConfig{
			PopupWidth:       "80%",
			PopupHeight:      "26",
			PopupMinWidth:    80,
			PopupMinHeight:   26,
			PopupBorderLines: "rounded",
			PopupBorderStyle: "fg=default",
			PopupStyle:       "bg=default,fg=default",
			UseNerdFonts:     false,
		},
		Presets: make(map[string][]string),
		Guardrails: GuardrailsConfig{
			ConfirmDestructive: true,
			AlsoConfirm:        []string{},
			SkipConfirm:        []string{},
		},
	}
}

func loadConfig(path string, stderr io.Writer) (Config, error) {
	cfg := defaultConfig()
	if err := checkFileSafety(path); err != nil {
		return cfg, err
	}

	if _, err := os.Stat(path); os.IsNotExist(err) {
		return cfg, nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return cfg, err
	}

	var raw map[string]any
	if err := toml.Unmarshal(data, &raw); err != nil {
		// If TOML is totally malformed, return it as an error
		return cfg, err
	}

	// Safely extract fields with default fallbacks for invalid/non-matching types
	if val, ok := raw["tool_window_base"]; ok {
		if n, err := toInt(val); err == nil {
			cfg.ToolWindowBase = n
		}
	}
	if val, ok := raw["shell_init_delay"]; ok {
		if f, err := toFloat(val); err == nil {
			cfg.ShellInitDelay = f
		}
	}
	if val, ok := raw["window_name_max_length"]; ok {
		if n, err := toInt(val); err == nil {
			cfg.WindowNameMaxLength = n
		}
	}
	if val, ok := raw["tool_window_prefix"]; ok {
		s, ok := val.(string)
		if !ok {
			return cfg, fmt.Errorf("tool_window_prefix must be a string")
		}
		cfg.ToolWindowPrefix = s
	}
	if val, ok := raw["normal_window_prefix"]; ok {
		s, ok := val.(string)
		if !ok {
			return cfg, fmt.Errorf("normal_window_prefix must be a string")
		}
		cfg.NormalWindowPrefix = s
	}
	if val, ok := raw["presets"]; ok {
		if m, ok := val.(map[string]any); ok {
			presets := make(map[string][]string)
			for k, v := range m {
				if arr, ok := v.([]any); ok {
					var list []string
					for _, item := range arr {
						if s, ok := item.(string); ok {
							list = append(list, s)
						}
					}
					presets[k] = list
				}
			}
			cfg.Presets = presets
		}
	}

	// Extract UI config
	if uiRaw, ok := raw["ui"].(map[string]any); ok {
		if val, ok := uiRaw["popup_width"]; ok {
			if s, ok := val.(string); ok {
				cfg.UI.PopupWidth = s
			} else if n, err := toInt(val); err == nil {
				cfg.UI.PopupWidth = strconv.Itoa(n)
			}
		}
		if val, ok := uiRaw["popup_height"]; ok {
			if s, ok := val.(string); ok {
				cfg.UI.PopupHeight = s
			} else if n, err := toInt(val); err == nil {
				cfg.UI.PopupHeight = strconv.Itoa(n)
			}
		}
		if val, ok := uiRaw["popup_min_width"]; ok {
			if n, err := toInt(val); err == nil {
				cfg.UI.PopupMinWidth = n
			}
		}
		if val, ok := uiRaw["popup_min_height"]; ok {
			if n, err := toInt(val); err == nil {
				cfg.UI.PopupMinHeight = n
			}
		}
		if val, ok := uiRaw["popup_border_lines"]; ok {
			if s, ok := val.(string); ok {
				cfg.UI.PopupBorderLines = s
			}
		}
		if val, ok := uiRaw["popup_border_style"]; ok {
			if s, ok := val.(string); ok {
				cfg.UI.PopupBorderStyle = s
			}
		}
		if val, ok := uiRaw["popup_style"]; ok {
			if s, ok := val.(string); ok {
				cfg.UI.PopupStyle = s
			}
		}
		if val, ok := uiRaw["use_nerd_fonts"]; ok {
			if b, ok := val.(bool); ok {
				cfg.UI.UseNerdFonts = b
			}
		}
	}

	// Extract Guardrails config
	if grRaw, ok := raw["guardrails"].(map[string]any); ok {
		if val, ok := grRaw["confirm_destructive"]; ok {
			if b, ok := val.(bool); ok {
				cfg.Guardrails.ConfirmDestructive = b
			}
		}
		if val, ok := grRaw["also_confirm"]; ok {
			if arr, ok := val.([]any); ok {
				cfg.Guardrails.AlsoConfirm = toStringSlice(arr)
			}
		}
		if val, ok := grRaw["skip_confirm"]; ok {
			if arr, ok := val.([]any); ok {
				cfg.Guardrails.SkipConfirm = toStringSlice(arr)
			}
		}
	}

	cfg.ToolWindowPrefix = normalizeWindowPrefix("tool_window_prefix", cfg.ToolWindowPrefix, stderr)
	cfg.NormalWindowPrefix = normalizeWindowPrefix("normal_window_prefix", cfg.NormalWindowPrefix, stderr)
	if err := validateWindowPrefixes(cfg.ToolWindowPrefix, cfg.NormalWindowPrefix); err != nil {
		return cfg, err
	}

	// Post-validation defaults check
	if cfg.ToolWindowBase <= 0 {
		cfg.ToolWindowBase = 88
	}
	if cfg.ShellInitDelay <= 0 {
		cfg.ShellInitDelay = 0.2
	}
	if cfg.WindowNameMaxLength <= 0 {
		cfg.WindowNameMaxLength = 20
	}
	if cfg.UI.PopupMinWidth <= 0 {
		cfg.UI.PopupMinWidth = 80
	}
	if cfg.UI.PopupMinHeight <= 0 {
		cfg.UI.PopupMinHeight = 25
	}
	validBorders := map[string]bool{
		"single": true, "rounded": true, "double": true, "heavy": true,
		"simple": true, "padded": true, "none": true, "default": true,
	}
	if !validBorders[cfg.UI.PopupBorderLines] {
		cfg.UI.PopupBorderLines = "rounded"
	}

	// Validate popup dimensions (percentage or absolute number)
	if !validateDimension(cfg.UI.PopupWidth) {
		fmt.Fprintf(stderr, "warning: invalid popup_width %q, using default 80%%\n", cfg.UI.PopupWidth)
		cfg.UI.PopupWidth = "80%"
	}
	if !validateDimension(cfg.UI.PopupHeight) {
		fmt.Fprintf(stderr, "warning: invalid popup_height %q, using default 70%%\n", cfg.UI.PopupHeight)
		cfg.UI.PopupHeight = "70%"
	}

	return cfg, nil
}

func configKeyDiagnostics(path string) ([]string, error) {
	if err := checkFileSafety(path); err != nil {
		return nil, err
	}
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return nil, nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var raw map[string]any
	if err := toml.Unmarshal(data, &raw); err != nil {
		return nil, err
	}

	knownTopLevel := map[string]bool{
		"tool_window_base":       true,
		"shell_init_delay":       true,
		"window_name_max_length": true,
		"tool_window_prefix":     true,
		"normal_window_prefix":   true,
		"presets":                true,
		"ui":                     true,
		"guardrails":             true,
	}
	sectionKeys := map[string]map[string]bool{
		"ui": {
			"popup_width":        true,
			"popup_height":       true,
			"popup_min_width":    true,
			"popup_min_height":   true,
			"popup_border_lines": true,
			"popup_border_style": true,
			"popup_style":        true,
			"use_nerd_fonts":     true,
		},
		"guardrails": {
			"confirm_destructive": true,
			"also_confirm":        true,
			"skip_confirm":        true,
		},
	}
	keySection := make(map[string]string)
	for section, keys := range sectionKeys {
		for key := range keys {
			keySection[key] = section
		}
	}

	var warnings []string
	for key, val := range raw {
		if section, ok := keySection[key]; ok {
			warnings = append(warnings, fmt.Sprintf("top-level %q is ignored; move it under [%s]", key, section))
			continue
		}
		if !knownTopLevel[key] {
			warnings = append(warnings, fmt.Sprintf("unknown top-level key %q is ignored", key))
			continue
		}
		if (key == "ui" || key == "guardrails" || key == "presets") && val != nil {
			if _, ok := val.(map[string]any); !ok {
				warnings = append(warnings, fmt.Sprintf("%q should be a table", key))
			}
		}
	}

	for section, knownKeys := range sectionKeys {
		val, ok := raw[section]
		if !ok {
			continue
		}
		sectionMap, ok := val.(map[string]any)
		if !ok {
			continue
		}
		for key := range sectionMap {
			if knownKeys[key] {
				continue
			}
			if knownTopLevel[key] {
				warnings = append(warnings, fmt.Sprintf("[%s].%s is ignored; move %q to top level", section, key, key))
			} else {
				warnings = append(warnings, fmt.Sprintf("unknown [%s].%s key is ignored", section, key))
			}
		}
	}
	sort.Strings(warnings)
	return warnings, nil
}

func loadSeshConfig(path string) (SeshConfig, error) {
	var cfg SeshConfig
	if err := checkFileSafety(path); err != nil {
		return cfg, err
	}

	if _, err := os.Stat(path); os.IsNotExist(err) {
		return cfg, nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return cfg, err
	}

	err = toml.Unmarshal(data, &cfg)
	return cfg, err
}

func toStringSlice(arr []any) []string {
	var out []string
	for _, item := range arr {
		if s, ok := item.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

func toInt(v any) (int, error) {
	switch val := v.(type) {
	case int:
		return val, nil
	case int64:
		return int(val), nil
	case float64:
		return int(val), nil
	case string:
		n, err := strconv.Atoi(val)
		if err == nil {
			return n, nil
		}
	}
	return 0, fmt.Errorf("invalid int: %v", v)
}

// validateDimension checks if a dimension string is a valid percentage (e.g., "80%")
// or absolute number (e.g., "80"). Percentages must be 10-100, absolute values must be >= 20.
func validateDimension(spec string) bool {
	if spec == "" {
		return false
	}
	if strings.HasSuffix(spec, "%") {
		pctStr := strings.TrimSuffix(spec, "%")
		pct, err := strconv.Atoi(pctStr)
		if err != nil {
			return false
		}
		return pct >= 10 && pct <= 100
	}
	// Absolute number
	val, err := strconv.Atoi(spec)
	if err != nil {
		return false
	}
	return val >= 20
}

func toFloat(v any) (float64, error) {
	switch val := v.(type) {
	case float64:
		return val, nil
	case int:
		return float64(val), nil
	case int64:
		return float64(val), nil
	case string:
		f, err := strconv.ParseFloat(val, 64)
		if err == nil {
			return f, nil
		}
	}
	return 0, fmt.Errorf("invalid float: %v", v)
}
