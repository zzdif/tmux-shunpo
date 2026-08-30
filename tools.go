package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

type ToolInfo struct {
	WindowName      string
	RawCommand      string
	ResolvedCommand string
}

func globMatch(pattern, name string) bool {
	var sb strings.Builder
	sb.WriteString("^")
	for _, r := range pattern {
		switch r {
		case '*':
			sb.WriteString(".*")
		case '?':
			sb.WriteString(".")
		case '.', '+', '(', ')', '^', '$', '|', '[', ']', '{', '}':
			sb.WriteString("\\")
			sb.WriteRune(r)
		default:
			sb.WriteRune(r)
		}
	}
	sb.WriteString("$")
	re, err := regexp.Compile(sb.String())
	if err != nil {
		return false
	}
	return re.MatchString(name)
}

func generateWindowName(command string, customName string, maxLen int) string {
	if customName != "" {
		sanitized := customName
		for _, char := range []string{" ", "/", ":"} {
			sanitized = strings.ReplaceAll(sanitized, char, "-")
		}
		for _, char := range []string{"(", ")", "[", "]", "{", "}", "|", "&", ";"} {
			sanitized = strings.ReplaceAll(sanitized, char, "")
		}
		return sanitized
	}

	firstWord := command
	if idx := strings.Index(command, " "); idx != -1 {
		firstWord = command[:idx]
	}

	// strip non-alphanumeric, non-underscore, non-dash characters
	var sb strings.Builder
	for _, r := range firstWord {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-' {
			sb.WriteRune(r)
		}
	}

	name := sb.String()
	if name == "" {
		return "window"
	}
	if len(name) > maxLen {
		return name[:maxLen]
	}
	return name
}

func (app *App) seshResolveWindowTemplate(templateName string) (string, error) {
	if err := validateTemplateName(templateName); err != nil {
		return "", err
	}
	for _, w := range app.SeshConfig.Window {
		if w.Name == templateName {
			return w.StartupScript, nil
		}
	}
	return "", fmt.Errorf("template not found")
}

func (app *App) getTool(sessionName string, slot int) (*ToolInfo, error) {
	if err := validateSessionName(sessionName); err != nil {
		return nil, err
	}
	if err := validateRange(slot, MinSlot, MaxSlot, "Tool slot must be between 1-9"); err != nil {
		return nil, err
	}

	toolsFile := filepath.Join(app.Paths.DataDir, "tools", sessionName)
	if _, err := os.Stat(toolsFile); os.IsNotExist(err) {
		return nil, err
	}

	file, err := os.Open(toolsFile)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}

		sStr := strings.TrimSpace(parts[0])
		s, err := strconv.Atoi(sStr)
		if err != nil || s != slot {
			continue
		}

		rawCmd := strings.TrimSpace(parts[1])
		if rawCmd == "" {
			continue
		}

		resolvedCmd := rawCmd
		if strings.HasPrefix(rawCmd, "@") {
			tmpl := rawCmd[1:]
			if tmpl != "shell" && tmpl != "attached" {
				resolved, err := app.seshResolveWindowTemplate(tmpl)
				if err == nil {
					resolvedCmd = resolved
				}
			}
		}

		windowName := generateWindowName(resolvedCmd, "", app.Config.WindowNameMaxLength)
		if strings.HasPrefix(rawCmd, "@") {
			tmpl := rawCmd[1:]
			if tmpl == "shell" {
				windowName = "shell"
			} else if tmpl == "attached" {
				windowName = "attached"
			} else {
				_, err := app.seshResolveWindowTemplate(tmpl)
				if err != nil {
					windowName = tmpl
				}
			}
		}

		return &ToolInfo{
			WindowName:      windowName,
			RawCommand:      rawCmd,
			ResolvedCommand: resolvedCmd,
		}, nil
	}

	return nil, fmt.Errorf("tool slot %d not found", slot)
}

func (app *App) toolInitFromDefaults(sessionName string) error {
	if err := validateSessionName(sessionName); err != nil {
		return err
	}
	toolsFile := filepath.Join(app.Paths.DataDir, "tools", sessionName)
	if _, err := os.Stat(toolsFile); err == nil {
		return nil
	}

	var entries []string
	var sourceLabel string

	// Priority 1: sesh config session matching by name
	var matchedSession *SessionConfig
	for i := range app.SeshConfig.Session {
		s := &app.SeshConfig.Session[i]
		if s.Name == sessionName {
			matchedSession = s
			break
		}
	}

	if matchedSession != nil && len(matchedSession.Windows) > 0 {
		entries = matchedSession.Windows
		sourceLabel = "sesh.toml session=" + sessionName
	} else {
		// Priority 2: sesh wildcard match
		var sessionPath string
		if matchedSession != nil && matchedSession.Path != "" {
			sessionPath = matchedSession.Path
		} else {
			// Query tmux for session_path
			out, err := app.Runner.Run("tmux", []string{"display-message", "-p", "#{session_path}"}, nil)
			if err == nil {
				sessionPath = strings.TrimSpace(string(out))
			}
		}
		sessionPath = normalizePathNoSymlink(sessionPath)

		var matchedWildcard []string
		var matchedPattern string
		if sessionPath != "" {
			for _, w := range app.SeshConfig.Wildcard {
				expandedPattern := normalizePathNoSymlink(w.Pattern)
				if globMatch(expandedPattern, sessionPath) {
					matchedWildcard = w.Windows
					matchedPattern = w.Pattern
					break
				}
			}
		}

		if len(matchedWildcard) > 0 {
			entries = matchedWildcard
			sourceLabel = "sesh.toml wildcard=" + matchedPattern
		} else if len(app.SeshConfig.DefaultSession.Windows) > 0 {
			// Priority 3: default_session
			entries = app.SeshConfig.DefaultSession.Windows
			sourceLabel = "sesh.toml default_session"
		}
	}

	if len(entries) == 0 {
		return nil
	}

	content := "# auto-generated from " + sourceLabel
	slot := MinSlot
	for _, entry := range entries {
		if slot > MaxSlot {
			break
		}
		trimmed := strings.TrimSpace(entry)
		if trimmed == "" {
			continue
		}

		// sesh window entries are bare template names.
		if err := validateTemplateName(trimmed); err != nil {
			continue
		}

		content += fmt.Sprintf("\n%d: @%s", slot, trimmed)
		slot++
	}
	content += "\n"

	return atomicWrite(toolsFile, content)
}

func (app *App) toolSet(sessionName string, slot int, command string) error {
	if err := validateSessionName(sessionName); err != nil {
		return err
	}
	if err := validateRange(slot, MinSlot, MaxSlot, "Slot must be between 1-9"); err != nil {
		return err
	}

	if !strings.HasPrefix(command, "@") {
		if err := validateCommand(command); err != nil {
			return err
		}
	} else {
		tmpl := command[1:]
		if tmpl != "shell" && tmpl != "attached" {
			if err := validateTemplateName(tmpl); err != nil {
				return err
			}
		}
	}

	toolsFile := filepath.Join(app.Paths.DataDir, "tools", sessionName)
	tools := make(map[int]string)

	if _, err := os.Stat(toolsFile); err == nil {
		file, err := os.Open(toolsFile)
		if err == nil {
			scanner := bufio.NewScanner(file)
			for scanner.Scan() {
				line := strings.TrimSpace(scanner.Text())
				if line == "" || strings.HasPrefix(line, "#") {
					continue
				}
				parts := strings.SplitN(line, ":", 2)
				if len(parts) == 2 {
					s, err := strconv.Atoi(strings.TrimSpace(parts[0]))
					c := strings.TrimSpace(parts[1])
					if err == nil && c != "" {
						tools[s] = c
					}
				}
			}
			file.Close()
		}
	}

	tools[slot] = command

	content := "# Tools for session: " + sessionName
	for i := MinSlot; i <= MaxSlot; i++ {
		if c, ok := tools[i]; ok {
			content += fmt.Sprintf("\n%d: %s", i, c)
		}
	}
	content += "\n"

	return atomicWrite(toolsFile, content)
}

func (app *App) toolRemove(sessionName string, slot int) error {
	if err := validateSessionName(sessionName); err != nil {
		return err
	}
	if err := validateRange(slot, MinSlot, MaxSlot, "Slot must be between 1-9"); err != nil {
		return err
	}

	toolsFile := filepath.Join(app.Paths.DataDir, "tools", sessionName)
	if _, err := os.Stat(toolsFile); os.IsNotExist(err) {
		return nil
	}

	tools, _, err := app.loadToolsMap(sessionName)
	if err != nil {
		return err
	}
	delete(tools, slot)

	// A removed tool becomes an ordinary window at the lowest free normal
	// index. Moving is best-effort because the tool window may already be gone.
	targetIndex := app.Config.ToolWindowBase + slot - 1
	target := fmt.Sprintf("%s:%d", sessionName, targetIndex)
	state, stateErr := app.readWindowLabelState(target)
	if stateErr == nil {
		lowestIdx, findErr := app.findLowestAvailableWindowIndex(sessionName)
		if findErr != nil {
			app.warnWindowLabel(findErr.Error())
		} else {
			destination := fmt.Sprintf("%s:%d", sessionName, lowestIdx)
			if _, moveErr := app.Runner.Run("tmux", []string{"move-window", "-d", "-s", target, "-t", destination}, nil); moveErr != nil {
				app.warnWindowLabel(fmt.Sprintf("move removed tool window: %v", moveErr))
			} else if labelErr := app.labelNormalWindow(state.ID); labelErr != nil {
				app.warnWindowLabel(labelErr.Error())
			}
		}
	}

	content := "# Tools for session: " + sessionName
	for i := MinSlot; i <= MaxSlot; i++ {
		if c, ok := tools[i]; ok {
			content += fmt.Sprintf("\n%d: %s", i, c)
		}
	}
	content += "\n"

	return atomicWrite(toolsFile, content)
}

func (app *App) toolCompact(sessionName string) error {
	if err := validateSessionName(sessionName); err != nil {
		return err
	}
	// Reclaim dead @attached slots first so compact renumbers only live tools;
	// otherwise a closed @attached entry (no window to move) would be pushed
	// forward into a fresh slot index, leaving the live window behind.
	if _, err := app.reconcileAttachedTools(sessionName); err != nil {
		return err
	}
	oldTools, oldSlots, err := app.loadToolsMap(sessionName)
	if err != nil {
		return err
	}
	if len(oldSlots) == 0 {
		return nil
	}

	// Get list of existing window indexes in tmux
	existingWindows := make(map[int]bool)
	listOut, err := app.Runner.Run("tmux", []string{"list-windows", "-t", sessionName, "-F", "#{window_index}"}, nil)
	if err == nil {
		for _, line := range strings.Split(string(listOut), "\n") {
			if val := strings.TrimSpace(line); val != "" {
				if idx, err := strconv.Atoi(val); err == nil {
					existingWindows[idx] = true
				}
			}
		}
	}

	// Perform window moves/swaps to keep them in sync with new compacted slot indexes.
	// Track both sides so their labels can be repaired after the new slot map is saved.
	touchedIndexes := make(map[int]bool)
	for i, oldSlot := range oldSlots {
		newSlot := i + 1
		if oldSlot == newSlot {
			continue
		}

		oldIndex := app.Config.ToolWindowBase + oldSlot - 1
		newIndex := app.Config.ToolWindowBase + newSlot - 1
		touchedIndexes[oldIndex] = true
		touchedIndexes[newIndex] = true

		if existingWindows[oldIndex] {
			oldTarget := fmt.Sprintf("%s:%d", sessionName, oldIndex)
			newTarget := fmt.Sprintf("%s:%d", sessionName, newIndex)
			if existingWindows[newIndex] {
				_, _ = app.Runner.Run("tmux", []string{"swap-window", "-d", "-s", oldTarget, "-t", newTarget}, nil)
			} else {
				_, _ = app.Runner.Run("tmux", []string{"move-window", "-d", "-s", oldTarget, "-t", newTarget}, nil)
				existingWindows[newIndex] = true
				delete(existingWindows, oldIndex)
			}
		}
	}

	newTools := make(map[int]string)
	newSlots := make([]int, 0, len(oldSlots))
	for i, oldSlot := range oldSlots {
		newSlot := i + 1
		if newSlot > MaxSlot {
			break
		}
		newTools[newSlot] = oldTools[oldSlot]
		newSlots = append(newSlots, newSlot)
	}

	if err := app.saveToolsMap(sessionName, newTools, newSlots); err != nil {
		return err
	}

	bindings := app.discoverToolBindings()
	for index := range touchedIndexes {
		target := fmt.Sprintf("%s:%d", sessionName, index)
		state, err := app.readWindowLabelState(target)
		if err != nil {
			continue
		}

		expected := app.expectedNormalWindowLabel(state.Index)
		if slot, ok := toolSlotAtIndex(state.Index, app.Config, newTools); ok {
			var warning string
			expected, warning = app.expectedToolWindowLabel(slot, bindings)
			if warning != "" {
				app.warnWindowLabel(warning)
			}
		}
		if err := app.applyWindowLabel(state, expected); err != nil {
			app.warnWindowLabel(err.Error())
		}
	}
	return nil
}

func (app *App) getNonToolWindows(targetSession string) ([]string, error) {
	if targetSession == "" {
		out, err := app.Runner.Run("tmux", []string{"display-message", "-p", "#S"}, nil)
		if err != nil {
			return nil, err
		}
		targetSession = strings.TrimSpace(string(out))
	}

	out, err := app.Runner.Run("tmux", []string{"list-windows", "-t", targetSession, "-F", "#{window_index}:::#{window_name}"}, nil)
	if err != nil {
		return nil, err
	}

	// Read tools file once to get which slots are defined
	toolSlots := make(map[int]bool)
	toolsFile := filepath.Join(app.Paths.DataDir, "tools", targetSession)
	if _, err := os.Stat(toolsFile); err == nil {
		file, err := os.Open(toolsFile)
		if err == nil {
			scanner := bufio.NewScanner(file)
			for scanner.Scan() {
				l := strings.TrimSpace(scanner.Text())
				if parts := strings.SplitN(l, ":", 2); len(parts) == 2 {
					if slot, err := strconv.Atoi(strings.TrimSpace(parts[0])); err == nil {
						toolSlots[slot] = true
					}
				}
			}
			file.Close()
		}
	}

	var result []string
	lines := strings.Split(string(out), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		parts := strings.SplitN(line, ":::", 2)
		if len(parts) != 2 {
			continue
		}

		idxStr := parts[0]
		name := parts[1]

		idx, err := strconv.Atoi(idxStr)
		if err == nil {
			if idx >= app.Config.ToolWindowBase && idx < app.Config.ToolWindowBase+MaxSlot {
				slot := idx - app.Config.ToolWindowBase + 1
				if toolSlots[slot] {
					continue
				}
			}
		}

		result = append(result, fmt.Sprintf("%s %s", idxStr, name))
	}

	return result, nil
}

// loadToolsMap parses a session's tools file into slot→command, returning
// the slots ascending. Blank lines, comments, unparseable slot numbers,
// empty commands and out-of-range slots are skipped. On duplicate slots the
// last entry wins. A missing file yields an empty result and no error, so
// callers can treat it uniformly with "no tools yet".
func (app *App) loadToolsMap(sessionName string) (map[int]string, []int, error) {
	tools := make(map[int]string)
	var seen []int
	toolsFile := filepath.Join(app.Paths.DataDir, "tools", sessionName)
	if _, err := os.Stat(toolsFile); os.IsNotExist(err) {
		return tools, nil, nil
	}
	file, err := os.Open(toolsFile)
	if err != nil {
		return nil, nil, err
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}
		s, err := strconv.Atoi(strings.TrimSpace(parts[0]))
		c := strings.TrimSpace(parts[1])
		if err != nil || c == "" || s < MinSlot || s > MaxSlot {
			continue
		}
		if _, dup := tools[s]; !dup {
			seen = append(seen, s)
		}
		tools[s] = c
	}
	if err := scanner.Err(); err != nil {
		return nil, nil, err
	}
	sort.Ints(seen)
	return tools, seen, nil
}

// saveToolsMap writes a tools file in the canonical ascending order. slots
// must be the set of keys to emit, in the desired order (typically ascending).
// The value for each slot is taken from tools.
func (app *App) saveToolsMap(sessionName string, tools map[int]string, slots []int) error {
	content := "# Tools for session: " + sessionName
	for _, s := range slots {
		if c, ok := tools[s]; ok {
			content += fmt.Sprintf("\n%d: %s", s, c)
		}
	}
	content += "\n"
	toolsFile := filepath.Join(app.Paths.DataDir, "tools", sessionName)
	return atomicWrite(toolsFile, content)
}

// reconcileAttachedTools removes @attached entries whose tmux window no
// longer exists, returning the reclaimed slots in ascending order. Persistent
// tools (@template, raw commands, @shell) are never touched — by design they
// are re-created on next navigation. Liveness is decided by tmux window-index
// existence: a closed @attached window frees its index, so "absent index ⟺
// dead attached". If tmux state can't be queried the file is left untouched
// (fail-safe: keep data rather than silently dropping it).
func (app *App) reconcileAttachedTools(sessionName string) ([]int, error) {
	if err := validateSessionName(sessionName); err != nil {
		return nil, err
	}

	tools, slots, err := app.loadToolsMap(sessionName)
	if err != nil {
		return nil, err
	}
	if len(slots) == 0 {
		return nil, nil
	}

	// Read tmux window indexes once.
	existingWindows := make(map[int]bool)
	listOut, err := app.Runner.Run("tmux", []string{"list-windows", "-t", sessionName, "-F", "#{window_index}"}, nil)
	if err != nil {
		// Fail safe: cannot verify liveness, leave tools intact.
		return nil, nil
	}
	for _, line := range strings.Split(string(listOut), "\n") {
		if val := strings.TrimSpace(line); val != "" {
			if idx, err := strconv.Atoi(val); err == nil {
				existingWindows[idx] = true
			}
		}
	}

	var reclaimed []int
	var remaining []int
	for _, s := range slots {
		if tools[s] == "@attached" && !existingWindows[app.Config.ToolWindowBase+s-1] {
			delete(tools, s)
			reclaimed = append(reclaimed, s)
			continue
		}
		remaining = append(remaining, s)
	}

	if len(reclaimed) == 0 {
		return nil, nil
	}

	if err := app.saveToolsMap(sessionName, tools, remaining); err != nil {
		return nil, err
	}
	return reclaimed, nil
}

func (app *App) nextEmptyToolSlot(sessionName string) (int, error) {
	if err := validateSessionName(sessionName); err != nil {
		return 0, err
	}

	occupied := make(map[int]bool)
	toolsFile := filepath.Join(app.Paths.DataDir, "tools", sessionName)
	if file, err := os.Open(toolsFile); err == nil {
		defer file.Close()
		scanner := bufio.NewScanner(file)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			parts := strings.SplitN(line, ":", 2)
			if len(parts) != 2 || strings.TrimSpace(parts[1]) == "" {
				continue
			}
			slot, err := strconv.Atoi(strings.TrimSpace(parts[0]))
			if err == nil && slot >= MinSlot && slot <= MaxSlot {
				occupied[slot] = true
			}
		}
		if err := scanner.Err(); err != nil {
			return 0, err
		}
	} else if !os.IsNotExist(err) {
		return 0, err
	}

	for slot := MinSlot; slot <= MaxSlot; slot++ {
		if !occupied[slot] {
			return slot, nil
		}
	}
	return 0, fmt.Errorf("all tool slots (%d-%d) are full", MinSlot, MaxSlot)
}

func (app *App) toolAddCurrentWindow() (int, error) {
	sessionBytes, err := app.Runner.Run("tmux", []string{"display-message", "-p", "#S"}, nil)
	if err != nil {
		return 0, fmt.Errorf("not in a tmux session")
	}
	sessionName := strings.TrimSpace(string(sessionBytes))
	if err := validateSessionName(sessionName); err != nil {
		return 0, err
	}

	// Reclaim @attached slots whose windows were closed so the next empty
	// slot reflects live state rather than stale tool-file entries.
	if _, err := app.reconcileAttachedTools(sessionName); err != nil {
		return 0, err
	}

	slot, err := app.nextEmptyToolSlot(sessionName)
	if err != nil {
		return 0, err
	}

	idxBytes, err := app.Runner.Run("tmux", []string{"display-message", "-p", "#{window_index}"}, nil)
	if err != nil {
		return 0, fmt.Errorf("cannot determine current window")
	}
	currentIndexStr := strings.TrimSpace(string(idxBytes))
	currentIndex, err := strconv.Atoi(currentIndexStr)
	if err != nil {
		return 0, fmt.Errorf("cannot determine current window")
	}

	minToolIndex := app.Config.ToolWindowBase
	maxToolIndex := app.Config.ToolWindowBase + MaxSlot - 1
	if currentIndex >= minToolIndex && currentIndex <= maxToolIndex {
		return 0, fmt.Errorf("current window is already in the tool slot range")
	}

	if err := app.assignWindowToTool(sessionName, slot, currentIndexStr); err != nil {
		return 0, err
	}
	return slot, nil
}

func (app *App) assignWindowToTool(sessionName string, slot int, srcTarget string) error {
	if err := validateSessionName(sessionName); err != nil {
		return err
	}
	if err := validateRange(slot, MinSlot, MaxSlot, "Slot must be between 1-9"); err != nil {
		return err
	}

	targetIndex := app.Config.ToolWindowBase + slot - 1
	targetTarget := fmt.Sprintf("%s:%d", sessionName, targetIndex)

	if !strings.Contains(srcTarget, ":") {
		out, err := app.Runner.Run("tmux", []string{"display-message", "-p", "#S"}, nil)
		if err != nil {
			return err
		}
		srcTarget = strings.TrimSpace(string(out)) + ":" + srcTarget
	}

	if _, err := app.Runner.Run("tmux", []string{"display-message", "-p", "-t", srcTarget, "#{window_name}"}, nil); err != nil {
		return fmt.Errorf("window %s no longer exists", srcTarget)
	}

	srcFullIdxBytes, err := app.Runner.Run("tmux", []string{"display-message", "-p", "-t", srcTarget, "#S:#I"}, nil)
	if err != nil {
		return err
	}
	srcFullIdx := strings.TrimSpace(string(srcFullIdxBytes))
	targetFullIdx := fmt.Sprintf("%s:%d", sessionName, targetIndex)

	if srcFullIdx != targetFullIdx {
		listOut, _ := app.Runner.Run("tmux", []string{"list-windows", "-t", sessionName, "-F", "#{window_index}"}, nil)
		targetExists := false
		for _, idxStr := range strings.Split(string(listOut), "\n") {
			if strings.TrimSpace(idxStr) == strconv.Itoa(targetIndex) {
				targetExists = true
				break
			}
		}

		if targetExists {
			_, err = app.Runner.Run("tmux", []string{"swap-window", "-s", srcTarget, "-t", targetTarget}, nil)
			if err != nil {
				return fmt.Errorf("failed to swap window to tool slot")
			}
			if err := app.labelNormalWindow(srcTarget); err != nil {
				app.warnWindowLabel(err.Error())
			}
		} else {
			_, err = app.Runner.Run("tmux", []string{"move-window", "-s", srcTarget, "-t", targetTarget}, nil)
			if err != nil {
				return fmt.Errorf("failed to move window to tool slot")
			}
		}
	}

	if err := app.toolSet(sessionName, slot, "@attached"); err != nil {
		return err
	}
	bindings := app.discoverToolBindings()
	app.labelToolWindow(targetTarget, slot, bindings)
	_, _ = app.Runner.Run("tmux", []string{"select-window", "-t", targetTarget}, nil)
	return nil
}
