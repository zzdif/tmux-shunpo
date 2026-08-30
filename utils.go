package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const (
	// Version of tmux-shunpo.
	Version = "0.2.0"

	// MinSlot and MaxSlot define the valid range for mark and tool slots.
	MinSlot = 1
	MaxSlot = 9

	// UI feedback delays (milliseconds).
	uiDelayShort  = 500
	uiDelayMedium = 1000
	uiDelayLong   = 1500
)

var (
	rxCommand      = regexp.MustCompile(`^[a-zA-Z0-9\ +./_:@=~'"|&<>;$()!` + "`" + `!-]+$`)
	rxSessionName  = regexp.MustCompile(`^[a-zA-Z0-9._-]+$`)
	rxTemplateName = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)
	rxMarkEntry    = regexp.MustCompile(`^[a-zA-Z0-9._/~-]+$`)

	// idleShells is the set of shell basenames considered "idle" when they
	// have no child processes.
	idleShells = map[string]bool{
		"bash": true, "zsh": true, "sh": true, "fish": true,
		"ksh": true, "tcsh": true, "dash": true, "csh": true,
	}
)

func validateRange(val, min, max int, errorMsg string) error {
	if val < min || val > max {
		return fmt.Errorf("%s", errorMsg)
	}
	return nil
}

func validateCommand(cmd string) error {
	trimmed := strings.TrimSpace(cmd)
	if trimmed == "" {
		return fmt.Errorf("error: command cannot be empty")
	}
	if !rxCommand.MatchString(trimmed) {
		return fmt.Errorf("error: invalid characters in command")
	}
	if strings.Contains(trimmed, "../..") {
		return fmt.Errorf("error: directory traversal not allowed")
	}
	return nil
}

func validateSessionName(name string) error {
	if name == "" {
		return fmt.Errorf("error: session name cannot be empty")
	}
	if strings.Contains(name, "../") || strings.HasPrefix(name, "/") || strings.Contains(name, "..") {
		return fmt.Errorf("error: path traversal in session name")
	}
	if !rxSessionName.MatchString(name) {
		return fmt.Errorf("error: invalid characters in session name")
	}
	return nil
}

func validateTemplateName(name string) error {
	if name == "" {
		return fmt.Errorf("error: template name cannot be empty")
	}
	if !rxTemplateName.MatchString(name) {
		return fmt.Errorf("error: invalid template name: %s", name)
	}
	return nil
}

func validateMarkEntry(path string) error {
	if path == "" {
		return nil
	}
	if !rxMarkEntry.MatchString(path) {
		return fmt.Errorf("error: invalid characters in path: %s", path)
	}
	return nil
}

func atomicWrite(filePath, content string) error {
	dir := filepath.Dir(filePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("cannot create directory for: %s: %w", filePath, err)
	}

	tempFile, err := os.CreateTemp(dir, filepath.Base(filePath)+".tmp.*")
	if err != nil {
		return fmt.Errorf("cannot create temp file in %s: %w", dir, err)
	}
	tempPath := tempFile.Name()
	defer func() {
		// Clean up temp file if it still exists
		_ = os.Remove(tempPath)
	}()

	if _, err := io.WriteString(tempFile, content); err != nil {
		_ = tempFile.Close()
		return fmt.Errorf("cannot write to temp file: %s: %w", tempPath, err)
	}
	if err := tempFile.Close(); err != nil {
		return fmt.Errorf("cannot close temp file: %s: %w", tempPath, err)
	}

	// Rename temp file to target file atomically
	if err := os.Rename(tempPath, filePath); err != nil {
		return fmt.Errorf("cannot move temp file to: %s: %w", filePath, err)
	}

	return nil
}

func expandTilde(path string) string {
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err == nil {
			return filepath.Join(home, path[2:])
		}
	} else if path == "~" {
		home, err := os.UserHomeDir()
		if err == nil {
			return home
		}
	}
	return path
}

func normalizePath(path string) string {
	if path == "" {
		return ""
	}
	expanded := expandTilde(path)
	abs, err := filepath.Abs(expanded)
	if err == nil {
		if resolved, err := filepath.EvalSymlinks(abs); err == nil {
			return resolved
		}
		return abs
	}
	return expanded
}

// normalizePathNoSymlink expands tilde and resolves to an absolute path
// without following symlinks. Used for glob matching where symlink
// resolution would break wildcard patterns.
func normalizePathNoSymlink(path string) string {
	if path == "" {
		return ""
	}
	expanded := expandTilde(path)
	abs, err := filepath.Abs(expanded)
	if err == nil {
		return abs
	}
	return expanded
}

// isDestructive reports whether cmd looks like a catastrophic command that
// warrants a confirmation prompt before it is stored. It is a best-effort
// guardrail against typos/accidental pastes in the interactive editor — not a
// security boundary — so an imperfect match is acceptable. The returned word is
// the matched command, for display in the prompt.
func (app *App) isDestructive(cmd string) (string, bool) {
	if !app.Config.Guardrails.ConfirmDestructive {
		return "", false
	}

	guarded := make(map[string]bool)
	for _, c := range builtinDestructiveCommands {
		guarded[strings.ToLower(c)] = true
	}
	for _, c := range app.Config.Guardrails.AlsoConfirm {
		guarded[strings.ToLower(c)] = true
	}
	for _, c := range app.Config.Guardrails.SkipConfirm {
		delete(guarded, strings.ToLower(c))
	}

	words := strings.Fields(cmd)
	for _, rawWord := range words {
		subparts := strings.FieldsFunc(rawWord, func(r rune) bool {
			return r == ';' || r == '|' || r == '&' || r == '<' || r == '>' || r == '(' || r == ')' || r == '`' || r == '$' || r == '='
		})
		for _, part := range subparts {
			clean := strings.Trim(part, "\"'`.-/\\")
			if clean == "" {
				continue
			}
			cleanLower := strings.ToLower(clean)
			for g := range guarded {
				// Exact match, or name + a `.`/`-`/`_` variant separator so
				// "mkfs" catches "mkfs.ext4" and "newfs" catches "newfs_apfs".
				if cleanLower == g ||
					strings.HasPrefix(cleanLower, g+".") ||
					strings.HasPrefix(cleanLower, g+"-") ||
					strings.HasPrefix(cleanLower, g+"_") {
					return clean, true
				}
			}
		}
	}
	return "", false
}

// confirmDestructive shows a yes/no prompt via gum and reports whether the user
// affirmed. gum confirm exits 0 on "Yes" and non-zero otherwise.
func (app *App) confirmDestructive(prompt string) bool {
	args := []string{"confirm", prompt, "--default=false"}
	return app.Runner.RunInteractive("gum", args, nil, app.Stdout, app.Stderr) == nil
}

// destructiveCommandsIn returns the raw commands in cmds that look destructive,
// skipping empty entries and @template references (which resolve elsewhere).
func (app *App) destructiveCommandsIn(cmds []string) []string {
	var found []string
	for _, c := range cmds {
		if c == "" || strings.HasPrefix(c, "@") {
			continue
		}
		if _, ok := app.isDestructive(c); ok {
			found = append(found, c)
		}
	}
	return found
}
