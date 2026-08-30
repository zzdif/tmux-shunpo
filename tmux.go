package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

func timeSleep(ms int) {
	time.Sleep(time.Duration(ms) * time.Millisecond)
}

func escapePath(path string) string {
	const whitelist = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789/_.+-~"
	safe := true
	for _, r := range path {
		if !strings.ContainsRune(whitelist, r) {
			safe = false
			break
		}
	}
	if safe {
		return path
	}
	escaped := strings.ReplaceAll(path, "'", `'\''`)
	return "'" + escaped + "'"
}

func (app *App) saveSessionWindow(sessionName, windowIndex string) error {
	if sessionName == "" || windowIndex == "" {
		return nil
	}
	if _, err := strconv.Atoi(windowIndex); err != nil {
		return nil
	}

	stateFile := app.Paths.SessionStateFile
	var states []string
	if _, err := os.Stat(stateFile); err == nil {
		file, err := os.Open(stateFile)
		if err == nil {
			scanner := bufio.NewScanner(file)
			for scanner.Scan() {
				line := strings.TrimSpace(scanner.Text())
				if line == "" || strings.HasPrefix(line, "#") {
					continue
				}
				parts := strings.SplitN(line, ":", 2)
				if len(parts) == 2 {
					if parts[0] != sessionName {
						states = append(states, line)
					}
				}
			}
			file.Close()
		}
	}

	states = append(states, fmt.Sprintf("%s:%s", sessionName, windowIndex))

	content := "# Session last window state\n" + strings.Join(states, "\n") + "\n"
	return atomicWrite(stateFile, content)
}

func (app *App) restoreSessionWindow(sessionName string) error {
	if sessionName == "" {
		return nil
	}
	stateFile := app.Paths.SessionStateFile
	if _, err := os.Stat(stateFile); os.IsNotExist(err) {
		return nil
	}

	var savedWindow string
	file, err := os.Open(stateFile)
	if err == nil {
		scanner := bufio.NewScanner(file)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			parts := strings.SplitN(line, ":", 2)
			if len(parts) == 2 {
				if parts[0] == sessionName {
					savedWindow = parts[1]
					break
				}
			}
		}
		file.Close()
	}

	if savedWindow == "" {
		return nil
	}
	if _, err := strconv.Atoi(savedWindow); err != nil {
		return nil
	}

	listOut, err := app.Runner.Run("tmux", []string{"list-windows", "-t", sessionName, "-F", "#{window_index}"}, nil)
	if err == nil {
		for _, wIdx := range strings.Split(string(listOut), "\n") {
			if strings.TrimSpace(wIdx) == savedWindow {
				_, _ = app.Runner.Run("tmux", []string{"select-window", "-t", fmt.Sprintf("%s:%s", sessionName, savedWindow)}, nil)
				break
			}
		}
	}

	return nil
}

func (app *App) sessionConnectWithState(sessionRef string) error {
	if sessionRef == "" {
		return fmt.Errorf("no session specified")
	}

	if os.Getenv("TMUX") != "" {
		curSessBytes, err := app.Runner.Run("tmux", []string{"display-message", "-p", "#S"}, nil)
		if err == nil {
			curSess := strings.TrimSpace(string(curSessBytes))
			curWinBytes, err := app.Runner.Run("tmux", []string{"display-message", "-p", "#I"}, nil)
			if err == nil {
				curWin := strings.TrimSpace(string(curWinBytes))
				_ = app.saveSessionWindow(curSess, curWin)
			}
		}
	}

	seshErrBytes, err := app.Runner.RunCombined("sesh", []string{"connect", sessionRef}, nil)
	if err != nil {
		seshErr := strings.TrimSpace(string(seshErrBytes))
		if os.Getenv("TMUX") != "" {
			var fallbackSession string
			if filepath.IsAbs(sessionRef) {
				listOut, err := app.Runner.Run("tmux", []string{"list-sessions", "-F", "#{session_name}\t#{session_path}"}, nil)
				if err == nil {
					for _, line := range strings.Split(string(listOut), "\n") {
						line = strings.TrimSpace(line)
						if line == "" {
							continue
						}
						parts := strings.SplitN(line, "\t", 2)
						if len(parts) == 2 && parts[1] == sessionRef {
							fallbackSession = parts[0]
							break
						}
					}
				}
			}

			if fallbackSession != "" {
				_, err = app.Runner.Run("tmux", []string{"switch-client", "-t", fallbackSession}, nil)
				if err != nil {
					return fmt.Errorf("failed to connect to '%s': %s", sessionRef, seshErr)
				}
			} else {
				_, err = app.Runner.Run("sesh", []string{"connect", "--switch", sessionRef}, nil)
				if err != nil {
					return fmt.Errorf("failed to connect to '%s': %s", sessionRef, seshErr)
				}
			}
		} else {
			return fmt.Errorf("failed to connect to '%s': %s", sessionRef, seshErr)
		}
	}

	if os.Getenv("TMUX") != "" {
		newSessBytes, err := app.Runner.Run("tmux", []string{"display-message", "-p", "#S"}, nil)
		if err == nil {
			newSess := strings.TrimSpace(string(newSessBytes))
			_ = app.restoreSessionWindow(newSess)
		}
	}

	return nil
}

func (app *App) isPaneIdle(paneID string) bool {
	out, err := app.Runner.Run("tmux", []string{"display-message", "-p", "-t", paneID, "#{pane_id} #{pane_current_command} #{pane_pid}"}, nil)
	if err != nil {
		return false
	}

	parts := strings.Fields(strings.TrimSpace(string(out)))
	if len(parts) < 3 {
		return false
	}
	actualPaneID := parts[0]
	currentCmd := parts[1]
	panePIDStr := parts[2]

	if tmuxPane := os.Getenv("TMUX_PANE"); tmuxPane != "" && actualPaneID == tmuxPane {
		return true // Our own bootstrapper pane is considered idle/safe
	}

	if idleShells[currentCmd] {
		panePID, err := strconv.Atoi(panePIDStr)
		if err != nil {
			return false
		}
		pgrepOut, err := app.Runner.Run("pgrep", []string{"-P", strconv.Itoa(panePID)}, nil)
		if err != nil {
			return true // No children => idle
		}
		children := strings.Split(strings.TrimSpace(string(pgrepOut)), "\n")
		count := 0
		for _, c := range children {
			if strings.TrimSpace(c) != "" {
				count++
			}
		}
		return count == 0
	}

	return false
}

func (app *App) defaultShell() string {
	out, err := app.Runner.Run("tmux", []string{"show-option", "-gv", "default-shell"}, nil)
	if err == nil {
		if shell := strings.TrimSpace(string(out)); shell != "" {
			return shell
		}
	}
	return os.Getenv("SHELL")
}

func (app *App) navigateToTool(slot int) error {
	if err := validateRange(slot, MinSlot, MaxSlot, "Invalid slot. Use 1-9"); err != nil {
		return err
	}

	sessionNameBytes, err := app.Runner.Run("tmux", []string{"display-message", "-p", "#S"}, nil)
	if err != nil {
		return fmt.Errorf("not in a tmux session")
	}
	sessionName := strings.TrimSpace(string(sessionNameBytes))

	sessionPathBytes, err := app.Runner.Run("tmux", []string{"display-message", "-p", "#{session_path}"}, nil)
	sessionPath := ""
	if err == nil {
		sessionPath = strings.TrimSpace(string(sessionPathBytes))
	} else {
		sessionPath, _ = os.Getwd()
	}
	sessionPath = normalizePath(sessionPath)

	windowIndex := app.Config.ToolWindowBase + slot - 1

	toolsFile := filepath.Join(app.Paths.DataDir, "tools", sessionName)
	if _, err := os.Stat(toolsFile); os.IsNotExist(err) {
		initialized, err := app.uiInitTools(sessionName)
		if err != nil {
			return err
		}
		if !initialized {
			return nil
		}
	}

	toolData, err := app.getTool(sessionName, slot)
	if err != nil {
		errorMsg := fmt.Sprintf("No tool set for slot %d in session '%s'", slot, sessionName)
		if isatty(os.Stdin.Fd()) {
			return fmt.Errorf("%s; use --tools to edit tools for this session", errorMsg)
		} else {
			app.Notify(fmt.Sprintf("Tool @%d not configured (use --tools to edit tools)", slot), false)
			return nil
		}
	}

	name := toolData.WindowName
	rawCommand := toolData.RawCommand
	command := toolData.ResolvedCommand

	if strings.HasPrefix(rawCommand, "@") && command == rawCommand {
		tmpl := rawCommand[1:]
		if tmpl == "shell" {
			command = app.defaultShell()
		} else if tmpl == "attached" {
			command = "@attached"
		} else {
			return fmt.Errorf("template '@%s' not found in sesh.toml", tmpl)
		}
	}

	if !strings.HasPrefix(command, "@") {
		if err := validateCommand(command); err != nil {
			return fmt.Errorf("invalid command for tool @%d: %s", slot, command)
		}
	}

	isShellOnly := false
	switch filepath.Base(command) {
	case "bash", "zsh", "sh", "fish", "ksh", "tcsh":
		isShellOnly = true
	}
	isAttached := rawCommand == "@attached"

	bindings := app.discoverToolBindings()
	expectedLabel, labelWarning := app.expectedToolWindowLabel(slot, bindings)
	if labelWarning != "" {
		app.warnWindowLabel(labelWarning)
	}

	windowInfoBytes, _ := app.Runner.Run("tmux", []string{"list-windows", "-F", "#{window_index} #{window_panes}"}, nil)
	windowExists := false
	paneCount := 0
	for _, line := range strings.Split(string(windowInfoBytes), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, strconv.Itoa(windowIndex)+" ") {
			windowExists = true
			parts := strings.Fields(line)
			if len(parts) > 1 {
				paneCount, _ = strconv.Atoi(parts[1])
			}
			break
		}
	}

	hasPanes := paneCount > 0

	if windowExists && hasPanes {
		if err := app.labelWindow(fmt.Sprintf(":%d", windowIndex), expectedLabel); err != nil {
			app.warnWindowLabel(err.Error())
		}

		isIdle := app.isPaneIdle(fmt.Sprintf(":%d.0", windowIndex))
		if isIdle && !isShellOnly && !isAttached {
			_, _ = app.Runner.Run("tmux", []string{"send-keys", "-t", fmt.Sprintf(":%d", windowIndex), "C-u"}, nil)
			_, _ = app.Runner.Run("tmux", []string{"send-keys", "-t", fmt.Sprintf(":%d", windowIndex), "-l", "--", "cd " + escapePath(sessionPath)}, nil)
			_, _ = app.Runner.Run("tmux", []string{"send-keys", "-t", fmt.Sprintf(":%d", windowIndex), "C-m"}, nil)
			_, _ = app.Runner.Run("tmux", []string{"send-keys", "-t", fmt.Sprintf(":%d", windowIndex), "-l", "--", "clear"}, nil)
			_, _ = app.Runner.Run("tmux", []string{"send-keys", "-t", fmt.Sprintf(":%d", windowIndex), "C-m"}, nil)
			_, _ = app.Runner.Run("tmux", []string{"send-keys", "-t", fmt.Sprintf(":%d", windowIndex), "C-u"}, nil)
			_, _ = app.Runner.Run("tmux", []string{"send-keys", "-t", fmt.Sprintf(":%d", windowIndex), "-l", "--", command}, nil)
			_, _ = app.Runner.Run("tmux", []string{"send-keys", "-t", fmt.Sprintf(":%d", windowIndex), "C-m"}, nil)
		}

		_, _ = app.Runner.Run("tmux", []string{"select-window", "-t", fmt.Sprintf(":%d", windowIndex)}, nil)
	} else {
		if rawCommand == "@attached" {
			_ = app.toolRemove(sessionName, slot)
			app.Notify(fmt.Sprintf("Attached window for slot @%d was closed. Tool removed.", slot), false)
			return nil
		}

		if windowExists {
			_, _ = app.Runner.Run("tmux", []string{"kill-window", "-t", fmt.Sprintf(":%d", windowIndex)}, nil)
		}

		_, err := app.Runner.Run("tmux", []string{"new-window", "-t", fmt.Sprintf(":%d", windowIndex), "-n", joinWindowLabel(expectedLabel, name), "-c", sessionPath}, nil)
		if err != nil {
			return fmt.Errorf("failed to create tool window %d: %w", windowIndex, err)
		}

		for attempt := 0; attempt < 20; attempt++ {
			paneCmdBytes, err := app.Runner.Run("tmux", []string{"display-message", "-p", "-t", fmt.Sprintf(":%d.0", windowIndex), "#{pane_current_command}"}, nil)
			if err == nil {
				paneCmd := strings.TrimSpace(string(paneCmdBytes))
				if idleShells[paneCmd] {
					break
				}
			}
			timeSleep(10)
		}

		timeSleep(int(app.Config.ShellInitDelay * 1000.0))

		if !isShellOnly {
			_, _ = app.Runner.Run("tmux", []string{"send-keys", "-t", fmt.Sprintf(":%d", windowIndex), "-l", "--", command}, nil)
			_, _ = app.Runner.Run("tmux", []string{"send-keys", "-t", fmt.Sprintf(":%d", windowIndex), "C-m"}, nil)
		}

		_, _ = app.Runner.Run("tmux", []string{"select-window", "-t", fmt.Sprintf(":%d", windowIndex)}, nil)

		if err := app.labelWindow(fmt.Sprintf(":%d", windowIndex), expectedLabel); err != nil {
			app.warnWindowLabel(err.Error())
		}
	}

	return nil
}

func (app *App) findLowestAvailableWindowIndex(sessionName string) (int, error) {
	baseIndex := 0
	out, err := app.Runner.Run("tmux", []string{"show-options", "-t", sessionName, "-v", "base-index"}, nil)
	val := strings.TrimSpace(string(out))
	if err != nil || val == "" {
		out, err = app.Runner.Run("tmux", []string{"show-options", "-gv", "base-index"}, nil)
		if err == nil {
			val = strings.TrimSpace(string(out))
		}
	}
	if val != "" {
		if idx, err := strconv.Atoi(val); err == nil {
			baseIndex = idx
		}
	}

	existing := make(map[int]bool)
	listOut, err := app.Runner.Run("tmux", []string{"list-windows", "-t", sessionName, "-F", "#{window_index}"}, nil)
	if err != nil {
		return 0, err
	}
	for _, line := range strings.Split(string(listOut), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if idx, err := strconv.Atoi(line); err == nil {
			existing[idx] = true
		}
	}

	minToolIndex := app.Config.ToolWindowBase
	maxToolIndex := app.Config.ToolWindowBase + MaxSlot - 1

	for i := baseIndex; ; i++ {
		if i >= minToolIndex && i <= maxToolIndex {
			continue
		}
		if !existing[i] {
			return i, nil
		}
	}
}
