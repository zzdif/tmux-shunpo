package main

import (
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
)

const (
	windowLabelOption = "@shunpo_label"
	windowFieldSep    = "\x1f"
)

var (
	errInvalidWindowState = errors.New("invalid tmux window state")
	errLinkedWindow       = errors.New("window is linked into multiple sessions")
	errWindowDisappeared  = errors.New("window disappeared")
)

type windowLabelState struct {
	ID            string
	Index         int
	Name          string
	RecordedLabel string
	Linked        bool
}

type toolBindingMap struct {
	keys         map[int][]string
	discoveryErr error
}

// joinWindowLabel renders a label and descriptor without inventing or
// normalizing user-authored text.
func joinWindowLabel(label, descriptor string) string {
	if descriptor == "" {
		return label
	}
	return label + " " + descriptor
}

// stripExactWindowLabel removes label only at a complete label boundary. A raw
// prefix match would corrupt names such as "@user" when the label is "@u".
func stripExactWindowLabel(name, label string) (string, bool) {
	if label == "" {
		return "", false
	}
	if name == label {
		return "", true
	}
	prefix := label + " "
	if strings.HasPrefix(name, prefix) {
		return strings.TrimPrefix(name, prefix), true
	}
	return "", false
}

// planWindowLabel returns the desired live name. The calculated label is
// authoritative, the recorded label is the only old label we will remove, and
// all other text is preserved exactly.
func planWindowLabel(currentName, recordedLabel, expectedLabel string) string {
	if _, ok := stripExactWindowLabel(currentName, expectedLabel); ok {
		return currentName
	}

	descriptor := currentName
	if stripped, ok := stripExactWindowLabel(currentName, recordedLabel); ok {
		descriptor = stripped
	}
	return joinWindowLabel(expectedLabel, descriptor)
}

func parseDirectToolBinding(command string) (int, bool) {
	const commandPrefix = "run-shell "
	if !strings.HasPrefix(command, commandPrefix) {
		return 0, false
	}

	payload := strings.TrimSpace(strings.TrimPrefix(command, commandPrefix))
	if len(payload) < 2 || payload[0] != '"' || payload[len(payload)-1] != '"' {
		return 0, false
	}
	unquoted, err := strconv.Unquote(payload)
	if err != nil {
		return 0, false
	}

	fields := strings.Fields(unquoted)
	if len(fields) != 3 || filepath.Base(fields[0]) != "tmux-shunpo" || fields[1] != "--goto" {
		return 0, false
	}
	if len(fields[2]) != 2 || fields[2][0] != '@' {
		return 0, false
	}

	slot, err := strconv.Atoi(fields[2][1:])
	if err != nil || slot < MinSlot || slot > MaxSlot {
		return 0, false
	}
	return slot, true
}

func normalizeToolBindingKey(key string) string {
	key = strings.TrimSpace(key)
	if strings.HasPrefix(key, "M-") {
		key = strings.TrimPrefix(key, "M-")
	}
	return key
}

func (app *App) discoverToolBindings() toolBindingMap {
	bindings := toolBindingMap{keys: make(map[int][]string)}
	format := "#{key_table}:::#{key_string}:::#{key_command}"
	out, err := app.Runner.Run("tmux", []string{"list-keys", "-a", "-F", format}, nil)
	if err != nil {
		bindings.discoveryErr = err
		return bindings
	}

	for _, line := range strings.Split(string(out), "\n") {
		parts := strings.SplitN(strings.TrimSpace(line), ":::", 3)
		if len(parts) != 3 {
			continue
		}
		slot, ok := parseDirectToolBinding(parts[2])
		if !ok {
			continue
		}
		key := normalizeToolBindingKey(parts[1])
		if key == "" || strings.ContainsAny(key, " \t\r\n") {
			continue
		}
		bindings.keys[slot] = append(bindings.keys[slot], key)
	}
	return bindings
}

func (bindings toolBindingMap) keyForSlot(slot int) (string, string) {
	if bindings.discoveryErr != nil {
		return strconv.Itoa(slot), "could not detect tool bindings; using logical tool slots"
	}

	keys := bindings.keys[slot]
	switch len(keys) {
	case 0:
		return strconv.Itoa(slot), fmt.Sprintf("no physical binding found for tool @%d; using logical slot", slot)
	case 1:
		return keys[0], ""
	default:
		return strconv.Itoa(slot), fmt.Sprintf("multiple physical bindings found for tool @%d; using logical slot", slot)
	}
}

func (app *App) expectedToolWindowLabel(slot int, bindings toolBindingMap) (string, string) {
	key, warning := bindings.keyForSlot(slot)
	return app.Config.ToolWindowPrefix + key, warning
}

func (app *App) expectedNormalWindowLabel(index int) string {
	return app.Config.NormalWindowPrefix + strconv.Itoa(index)
}

func (app *App) readWindowLabelState(target string) (windowLabelState, error) {
	format := strings.Join([]string{
		"#{window_id}",
		"#{window_index}",
		"#{window_name}",
		"#{" + windowLabelOption + "}",
		"#{window_linked}",
	}, windowFieldSep)
	out, err := app.Runner.Run("tmux", []string{"display-message", "-p", "-t", target, format}, nil)
	if err != nil {
		return windowLabelState{}, err
	}

	parts := strings.SplitN(strings.TrimSuffix(string(out), "\n"), windowFieldSep, 5)
	if len(parts) != 5 || parts[0] == "" {
		return windowLabelState{}, errInvalidWindowState
	}
	index, err := strconv.Atoi(parts[1])
	if err != nil {
		return windowLabelState{}, fmt.Errorf("%w: invalid window index %q", errInvalidWindowState, parts[1])
	}

	return windowLabelState{
		ID:            parts[0],
		Index:         index,
		Name:          parts[2],
		RecordedLabel: parts[3],
		Linked:        parts[4] == "1",
	}, nil
}

func (app *App) windowDisappeared(windowID string) bool {
	_, err := app.Runner.Run("tmux", []string{"display-message", "-p", "-t", windowID, "#{window_id}"}, nil)
	return err != nil
}

func (app *App) applyWindowLabel(state windowLabelState, expectedLabel string) error {
	if state.Linked {
		return errLinkedWindow
	}

	desiredName := planWindowLabel(state.Name, state.RecordedLabel, expectedLabel)
	if desiredName != state.Name {
		if _, err := app.Runner.Run("tmux", []string{"rename-window", "-t", state.ID, desiredName}, nil); err != nil {
			if app.windowDisappeared(state.ID) {
				return errWindowDisappeared
			}
			return fmt.Errorf("rename window %s: %w", state.ID, err)
		}
	}

	if state.RecordedLabel != expectedLabel {
		if _, err := app.Runner.Run("tmux", []string{"set-option", "-w", "-t", state.ID, windowLabelOption, expectedLabel}, nil); err != nil {
			if app.windowDisappeared(state.ID) {
				return errWindowDisappeared
			}
			return fmt.Errorf("record label for window %s: %w", state.ID, err)
		}
	}
	return nil
}

func (app *App) labelWindow(target, expectedLabel string) error {
	state, err := app.readWindowLabelState(target)
	if err != nil {
		return err
	}
	return app.applyWindowLabel(state, expectedLabel)
}

func (app *App) labelNormalWindow(target string) error {
	state, err := app.readWindowLabelState(target)
	if err != nil {
		return err
	}
	return app.applyWindowLabel(state, app.expectedNormalWindowLabel(state.Index))
}

func (app *App) warnWindowLabel(message string) {
	app.Notify("Window label warning: "+message, true)
}

func (app *App) labelToolWindow(target string, slot int, bindings toolBindingMap) {
	expected, warning := app.expectedToolWindowLabel(slot, bindings)
	if warning != "" {
		app.warnWindowLabel(warning)
	}
	if err := app.labelWindow(target, expected); err != nil {
		app.warnWindowLabel(err.Error())
	}
}

func toolSlotAtIndex(index int, cfg Config, tools map[int]string) (int, bool) {
	slot := index - cfg.ToolWindowBase + 1
	if slot < MinSlot || slot > MaxSlot {
		return 0, false
	}
	_, ok := tools[slot]
	return slot, ok
}

func appendUnique(messages []string, message string) []string {
	for _, existing := range messages {
		if existing == message {
			return messages
		}
	}
	return append(messages, message)
}

func (app *App) cmdLabelWindows() error {
	sessionBytes, err := app.Runner.Run("tmux", []string{"display-message", "-p", "#S"}, nil)
	if err != nil {
		return fmt.Errorf("not in a tmux session")
	}
	sessionName := strings.TrimSpace(string(sessionBytes))

	tools, _, err := app.loadToolsMap(sessionName)
	if err != nil {
		return fmt.Errorf("read tools for session %q: %w", sessionName, err)
	}
	bindings := app.discoverToolBindings()

	out, err := app.Runner.Run("tmux", []string{"list-windows", "-t", sessionName, "-F", "#{window_id}"}, nil)
	if err != nil {
		return fmt.Errorf("list windows for session %q: %w", sessionName, err)
	}

	var warnings []string
	var failures []string
	for _, line := range strings.Split(string(out), "\n") {
		windowID := strings.TrimSpace(line)
		if windowID == "" {
			continue
		}

		state, err := app.readWindowLabelState(windowID)
		if err != nil {
			if errors.Is(err, errInvalidWindowState) {
				failures = append(failures, fmt.Sprintf("read window %s: %v", windowID, err))
			} else {
				warnings = appendUnique(warnings, fmt.Sprintf("window %s disappeared", windowID))
			}
			continue
		}
		if state.Linked {
			warnings = appendUnique(warnings, fmt.Sprintf("window %s is linked and was skipped", windowID))
			continue
		}

		expected := app.expectedNormalWindowLabel(state.Index)
		if slot, ok := toolSlotAtIndex(state.Index, app.Config, tools); ok {
			var warning string
			expected, warning = app.expectedToolWindowLabel(slot, bindings)
			if warning != "" {
				warnings = appendUnique(warnings, warning)
			}
		}

		if err := app.applyWindowLabel(state, expected); err != nil {
			if errors.Is(err, errWindowDisappeared) {
				warnings = appendUnique(warnings, fmt.Sprintf("window %s disappeared", windowID))
				continue
			}
			failures = append(failures, err.Error())
		}
	}

	if len(failures) > 0 {
		return fmt.Errorf("window labeling incomplete: %s", strings.Join(failures, "; "))
	}

	message := "Labeled windows"
	if len(warnings) > 0 {
		message += "; warning: " + strings.Join(warnings, "; ")
	}
	app.Notify(message, false)
	return nil
}
