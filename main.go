package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"golang.org/x/term"
)

type CommandRunner interface {
	Run(name string, args []string, stdin io.Reader) ([]byte, error)
	RunCombined(name string, args []string, stdin io.Reader) ([]byte, error)
	RunInteractive(name string, args []string, stdin io.Reader, stdout io.Writer, stderr io.Writer) error
	LookPath(file string) (string, error)
}

type RealRunner struct{}

func (r RealRunner) Run(name string, args []string, stdin io.Reader) ([]byte, error) {
	cmd := exec.Command(name, args...)
	cmd.Stdin = stdin
	return cmd.Output()
}

func (r RealRunner) RunCombined(name string, args []string, stdin io.Reader) ([]byte, error) {
	cmd := exec.Command(name, args...)
	cmd.Stdin = stdin
	return cmd.CombinedOutput()
}

func (r RealRunner) RunInteractive(name string, args []string, stdin io.Reader, stdout io.Writer, stderr io.Writer) error {
	cmd := exec.Command(name, args...)
	cmd.Stdin = stdin
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	return cmd.Run()
}

func (r RealRunner) LookPath(file string) (string, error) {
	return exec.LookPath(file)
}

type App struct {
	Paths      Paths
	Config     Config
	SeshConfig SeshConfig
	Runner     CommandRunner
	Stdout     io.Writer
	Stderr     io.Writer
}

func isatty(fd uintptr) bool {
	return term.IsTerminal(int(fd))
}

func (app *App) Notify(message string, isError bool) {
	if isatty(os.Stdin.Fd()) {
		if isError {
			fmt.Fprintln(app.Stderr, message)
		} else {
			fmt.Fprintln(app.Stdout, message)
		}
	} else if os.Getenv("TMUX") != "" {
		_, _ = app.Runner.Run("tmux", []string{"display-message", message}, nil)
	} else if isError {
		fmt.Fprintln(app.Stderr, message)
	}
}

func (app *App) errorExit(message string, exitCode int) {
	app.Notify(message, true)
	if !isatty(os.Stdin.Fd()) && os.Getenv("TMUX") != "" {
		os.Exit(0)
	}
	os.Exit(exitCode)
}

func (app *App) ensureTTYOrPopup(flag string, extraArgs ...string) {
	if isatty(os.Stdin.Fd()) {
		return
	}

	if os.Getenv("TMUX") == "" {
		return
	}

	selfPath, err := os.Executable()
	if err != nil {
		selfPath = os.Args[0]
	}

	args := []string{"display-message", "-p", "-F", "#{window_width} #{window_height}"}
	out, err := app.Runner.Run("tmux", args, nil)
	termW, termH := 80, 24
	if err == nil {
		fmt.Sscanf(string(out), "%d %d", &termW, &termH)
	}

	width := app.parseDimension(app.Config.UI.PopupWidth, termW, app.Config.UI.PopupMinWidth)
	height := app.parseDimension(app.Config.UI.PopupHeight, termH, app.Config.UI.PopupMinHeight)

	// Clamp popup sizes on large terminals (e.g., 4K/8K screens) to prevent excessive blank space
	if termW > 110 && width > 100 {
		width = 100
	}
	if termH > 40 && height > 35 {
		height = 35
	}

	// Clamp popup height for TUI screens to keep it tight and prevent excess blank space
	if flag == "--marks" || flag == "--tools" || flag == "--bootstrap" {
		if height > 26 {
			height = 26
		}
	}

	if width > termW {
		width = termW
	}
	if height > termH {
		height = termH
	}

	popupArgs := []string{
		"display-popup",
		"-E",
		"-w", fmt.Sprintf("%d", width),
		"-h", fmt.Sprintf("%d", height),
	}

	if app.tmuxSupportsPopupFlag("-b") {
		if app.Config.UI.PopupBorderLines != "" && app.Config.UI.PopupBorderLines != "default" {
			popupArgs = append(popupArgs, "-b", app.Config.UI.PopupBorderLines)
		}
	}
	if app.tmuxSupportsPopupFlag("-s") {
		if app.Config.UI.PopupStyle != "" {
			popupArgs = append(popupArgs, "-s", app.Config.UI.PopupStyle)
		}
	}
	if app.tmuxSupportsPopupFlag("-S") {
		if app.Config.UI.PopupBorderStyle != "" {
			popupArgs = append(popupArgs, "-S", app.Config.UI.PopupBorderStyle)
		}
	}

	cmdArgs := append([]string{selfPath, flag}, extraArgs...)
	popupArgs = append(popupArgs, "--")
	popupArgs = append(popupArgs, cmdArgs...)

	_, _ = app.Runner.Run("tmux", popupArgs, nil)
	os.Exit(0)
}

func (app *App) tmuxSupportsPopupFlag(flag string) bool {
	var args []string
	switch flag {
	case "-b":
		args = []string{"display-popup", "-b", "rounded", "-C"}
	case "-s":
		args = []string{"display-popup", "-s", "fg=default", "-C"}
	case "-S":
		args = []string{"display-popup", "-S", "fg=default", "-C"}
	default:
		return false
	}
	_, err := app.Runner.Run("tmux", args, nil)
	return err == nil
}

func (app *App) parseDimension(spec string, total, min int) int {
	var val int
	if strings.HasSuffix(spec, "%") {
		pctStr := strings.TrimSuffix(spec, "%")
		pct, err := strconv.Atoi(pctStr)
		if err != nil {
			val = total * 80 / 100
		} else {
			val = total * pct / 100
		}
	} else {
		var err error
		val, err = strconv.Atoi(spec)
		if err != nil {
			val = min
		}
	}
	if val < min {
		return min
	}
	return val
}

func (app *App) checkDependencies(needSesh, needGum bool) error {
	var missing []string

	if _, err := app.Runner.LookPath("tmux"); err != nil {
		missing = append(missing, "tmux>=3.2")
	}

	if needSesh {
		if _, err := app.Runner.LookPath("sesh"); err != nil {
			missing = append(missing, "sesh")
		}
	}

	if needGum {
		if _, err := app.Runner.LookPath("gum"); err != nil {
			missing = append(missing, "gum")
		}
	}

	if len(missing) > 0 {
		return fmt.Errorf("missing required tools: %s", strings.Join(missing, ", "))
	}
	return nil
}

func (app *App) markAddCurrent() (int, string, error) {
	insideTmux := os.Getenv("TMUX") != ""
	currSess := ""
	currDir := ""

	if insideTmux {
		out, err := app.Runner.Run("tmux", []string{"display-message", "-p", "#S"}, nil)
		if err == nil {
			currSess = strings.TrimSpace(string(out))
		}

		pathOut, err := app.Runner.Run("tmux", []string{"display-message", "-p", "#{session_path}"}, nil)
		if err == nil {
			currDir = strings.TrimSpace(string(pathOut))
		}
	}

	if currDir == "" {
		currDir, _ = os.Getwd()
	}

	return markAdd(app.Paths.MarksFile, insideTmux, currSess, currDir)
}

func (app *App) cmdDoctor() int {
	exitCode := 0
	fmt.Fprintln(app.Stdout, "tmux-shunpo doctor")

	for _, dep := range []string{"tmux", "sesh", "gum"} {
		if _, err := app.Runner.LookPath(dep); err != nil {
			fmt.Fprintf(app.Stdout, "WARN dependency %s not found\n", dep)
		} else {
			fmt.Fprintf(app.Stdout, "OK dependency %s found\n", dep)
		}
	}

	fmt.Fprintf(app.Stdout, "Config: %s\n", app.Paths.ConfigFile)
	if _, err := os.Stat(app.Paths.ConfigFile); os.IsNotExist(err) {
		fmt.Fprintln(app.Stdout, "WARN config file not found; defaults in use")
	} else {
		var warnings bytes.Buffer
		_, err := loadConfig(app.Paths.ConfigFile, &warnings)
		if err != nil {
			fmt.Fprintf(app.Stdout, "FAIL config parse: %v\n", err)
			exitCode = 1
		} else {
			fmt.Fprintln(app.Stdout, "OK config parsed")
			for _, warning := range strings.Split(strings.TrimSpace(warnings.String()), "\n") {
				if warning != "" {
					fmt.Fprintf(app.Stdout, "WARN %s\n", warning)
				}
			}
			keyWarnings, err := configKeyDiagnostics(app.Paths.ConfigFile)
			if err != nil {
				fmt.Fprintf(app.Stdout, "WARN unable to inspect raw config keys: %v\n", err)
			} else {
				for _, warning := range keyWarnings {
					fmt.Fprintf(app.Stdout, "WARN %s\n", warning)
				}
			}
		}
	}

	fmt.Fprintf(app.Stdout, "Sesh config: %s\n", app.Paths.SeshConfigFile)
	if _, err := os.Stat(app.Paths.SeshConfigFile); os.IsNotExist(err) {
		fmt.Fprintln(app.Stdout, "WARN sesh config file not found; no templates/defaults loaded")
	} else if _, err := loadSeshConfig(app.Paths.SeshConfigFile); err != nil {
		fmt.Fprintf(app.Stdout, "FAIL sesh config parse: %v\n", err)
		exitCode = 1
	} else {
		fmt.Fprintln(app.Stdout, "OK sesh config parsed")
	}

	return exitCode
}

func (app *App) cmdBootstrap(presetName string, force bool, keep bool) error {
	sessionNameBytes, err := app.Runner.Run("tmux", []string{"display-message", "-p", "#S"}, nil)
	if err != nil {
		return fmt.Errorf("not in a tmux session")
	}
	sessionName := strings.TrimSpace(string(sessionNameBytes))

	minIdx := app.Config.ToolWindowBase
	maxIdx := app.Config.ToolWindowBase + 8

	// Move the runner window out of the tool window base range if it occupies one of the tool slots
	runnerWindowIndex := -1
	if tmuxPane := os.Getenv("TMUX_PANE"); tmuxPane != "" {
		out, err := app.Runner.Run("tmux", []string{"display-message", "-p", "-t", tmuxPane, "#{window_index}"}, nil)
		if err == nil {
			runnerWindowIndex, _ = strconv.Atoi(strings.TrimSpace(string(out)))
		}
	}
	if runnerWindowIndex >= minIdx && runnerWindowIndex <= maxIdx {
		safeIdx, err := app.findLowestAvailableWindowIndex(sessionName)
		if err == nil {
			_, err = app.Runner.Run("tmux", []string{"move-window", "-d", "-s", fmt.Sprintf("%s:%d", sessionName, runnerWindowIndex), "-t", fmt.Sprintf("%s:%d", sessionName, safeIdx)}, nil)
			if err != nil {
				// If moving fails, proceed best-effort.
			}
		}
	}

	var toolsToApply []string

	if presetName != "" {
		presetList, ok := app.Config.Presets[presetName]
		if !ok {
			return fmt.Errorf("preset '%s' not found in configuration", presetName)
		}
		toolsToApply = presetList
	} else {
		if err := app.toolInitFromDefaults(sessionName); err != nil {
			return err
		}
		for i := MinSlot; i <= MaxSlot; i++ {
			if toolData, err := app.getTool(sessionName, i); err == nil {
				toolsToApply = append(toolsToApply, toolData.RawCommand)
			} else {
				toolsToApply = append(toolsToApply, "")
			}
		}
	}

	hasTools := false
	for _, t := range toolsToApply {
		if t != "" {
			hasTools = true
			break
		}
	}
	if !hasTools {
		return fmt.Errorf("no tools found to bootstrap; configure tools or presets first")
	}

	// One confirmation for the whole preset if it contains destructive commands.
	// Only when interactive — scripted `--bootstrap <name>` runs are deliberate.
	if presetName != "" && isatty(os.Stdin.Fd()) {
		if destructive := app.destructiveCommandsIn(toolsToApply); len(destructive) > 0 {
			prompt := fmt.Sprintf("⚠ Preset %q includes destructive commands: %s. Apply anyway?",
				presetName, strings.Join(destructive, ", "))
			if !app.confirmDestructive(prompt) {
				return nil
			}
		}
	}

	// Audit windows for busy processes (only if we plan to nuke them, i.e., keep is false)
	var busyWindows []string
	if !keep {
		listOut, err := app.Runner.Run("tmux", []string{"list-windows", "-F", "#{window_index} #{window_panes}"}, nil)
		if err == nil {
			lines := strings.Split(string(listOut), "\n")
			for _, line := range lines {
				line = strings.TrimSpace(line)
				if line == "" {
					continue
				}
				parts := strings.Fields(line)
				if len(parts) < 2 {
					continue
				}
				wIdxStr := parts[0]
				panesCount, _ := strconv.Atoi(parts[1])
				wIdx, _ := strconv.Atoi(wIdxStr)

				if wIdx < minIdx || wIdx > maxIdx {
					for p := 0; p < panesCount; p++ {
						paneID := fmt.Sprintf(":%d.%d", wIdx, p)
						if !app.isPaneIdle(paneID) {
							cmdNameBytes, err := app.Runner.Run("tmux", []string{"display-message", "-p", "-t", paneID, "#{pane_current_command}"}, nil)
							cmdName := "unknown"
							if err == nil {
								cmdName = strings.TrimSpace(string(cmdNameBytes))
							}
							busyWindows = append(busyWindows, fmt.Sprintf("%d: %s", wIdx, cmdName))
							break
						}
					}
				}
			}
		}
	}

	if !keep && !force && len(busyWindows) > 0 {
		fmt.Fprintln(app.Stdout, "Busy processes detected in existing windows:")
		for _, bw := range busyWindows {
			fmt.Fprintln(app.Stdout, "  Window "+bw)
		}
		fmt.Fprintln(app.Stdout)

		if isatty(os.Stdin.Fd()) {
			if !app.runGumConfirm(fmt.Sprintf("Nuke all windows and bootstrap session '%s'?", sessionName)) {
				fmt.Fprintln(app.Stdout, "Aborted.")
				return nil
			}
		} else {
			return fmt.Errorf("busy processes detected and not interactive; run with --force to bypass")
		}
	}

	// Apply tools (if preset was used, update the tools file)
	if presetName != "" {
		// Pre-validate templates first
		for _, checkCmd := range toolsToApply {
			if checkCmd == "" {
				continue
			}
			if strings.HasPrefix(checkCmd, "@") {
				tmpl := checkCmd[1:]
				if tmpl != "shell" && tmpl != "attached" {
					if _, err := app.seshResolveWindowTemplate(tmpl); err != nil {
						return fmt.Errorf("template '@%s' not found in sesh.toml; bootstrap aborted", tmpl)
					}
				}
			}
		}

		for i, cmd := range toolsToApply {
			slot := i + 1
			if cmd != "" {
				_ = app.toolSet(sessionName, slot, cmd)
			}
		}
	}

	// The Rebirth (Surgical Clean)
	tempWinBytes, err := app.Runner.Run("tmux", []string{"new-window", "-P", "-d", "-n", "shunpo-boot-tmp", "-F", "#{window_id}"}, nil)
	if err != nil {
		return fmt.Errorf("failed to create temporary bootstrap window: %w", err)
	}
	tempWin := strings.TrimSpace(string(tempWinBytes))

	defer func() {
		_, _ = app.Runner.Run("tmux", []string{"kill-window", "-t", tempWin}, nil)
	}()

	for i, cmd := range toolsToApply {
		if cmd == "" {
			continue
		}
		slot := i + 1
		_ = app.navigateToTool(slot)
	}

	currentPaneWindowID := ""
	tmuxPane := os.Getenv("TMUX_PANE")
	if tmuxPane != "" {
		out, err := app.Runner.Run("tmux", []string{"display-message", "-p", "-t", tmuxPane, "#{window_id}"}, nil)
		if err == nil {
			currentPaneWindowID = strings.TrimSpace(string(out))
		}
	}

	ownWindowToKill := ""
	if !keep {
		allWinsBytes, err := app.Runner.Run("tmux", []string{"list-windows", "-F", "#{window_index}"}, nil)
		if err == nil {
			lines := strings.Split(string(allWinsBytes), "\n")
			for _, line := range lines {
				line = strings.TrimSpace(line)
				if line == "" {
					continue
				}
				winIdx, err := strconv.Atoi(line)
				if err != nil {
					continue
				}

				if winIdx < minIdx || winIdx > maxIdx {
					curIDBytes, err := app.Runner.Run("tmux", []string{"display-message", "-p", "-t", fmt.Sprintf(":%d", winIdx), "#{window_id}"}, nil)
					curID := strings.TrimSpace(string(curIDBytes))
					if err == nil && curID != tempWin {
						if curID == currentPaneWindowID {
							ownWindowToKill = curID
						} else {
							_, _ = app.Runner.Run("tmux", []string{"kill-window", "-t", fmt.Sprintf(":%d", winIdx)}, nil)
						}
					}
				}
			}
		}
	}

	if len(toolsToApply) > 0 && toolsToApply[0] != "" {
		_, _ = app.Runner.Run("tmux", []string{"select-window", "-t", fmt.Sprintf(":%d", minIdx)}, nil)
	}

	app.Notify(fmt.Sprintf("Session '%s' bootstrapped with %d tools", sessionName, len(toolsToApply)), false)

	if !keep && ownWindowToKill != "" {
		_, _ = app.Runner.Run("tmux", []string{"kill-window", "-t", ownWindowToKill}, nil)
	}
	return nil
}

func (app *App) dataReset(scope string) error {
	switch scope {
	case "session":
		sessionNameBytes, err := app.Runner.Run("tmux", []string{"display-message", "-p", "#S"}, nil)
		if err != nil {
			return fmt.Errorf("not in a tmux session")
		}
		sessionName := strings.TrimSpace(string(sessionNameBytes))
		toolsFile := filepath.Join(app.Paths.DataDir, "tools", sessionName)

		if _, err := os.Stat(toolsFile); err == nil {
			if isatty(os.Stdin.Fd()) {
				if app.runGumConfirm(fmt.Sprintf("Delete tools for session '%s'?", sessionName)) {
					_ = os.Remove(toolsFile)
					app.Notify(fmt.Sprintf("Tools for session '%s' reset", sessionName), false)
				} else {
					app.Notify("Reset cancelled", false)
				}
			} else {
				_ = os.Remove(toolsFile)
				app.Notify(fmt.Sprintf("Tools for session '%s' reset", sessionName), false)
			}
		} else {
			app.Notify(fmt.Sprintf("No tools file found for session '%s'", sessionName), false)
		}
	case "all":
		if isatty(os.Stdin.Fd()) {
			if app.runGumConfirm("Delete ALL tmux-shunpo data?") {
				_ = os.Remove(app.Paths.MarksFile)
				_ = os.Remove(app.Paths.SessionStateFile)
				_ = os.RemoveAll(filepath.Join(app.Paths.DataDir, "tools"))
				_ = os.MkdirAll(filepath.Join(app.Paths.DataDir, "tools"), 0755)
				app.Notify("All tmux-shunpo data reset", false)
			} else {
				app.Notify("Reset cancelled", false)
			}
		} else {
			_ = os.Remove(app.Paths.MarksFile)
			_ = os.Remove(app.Paths.SessionStateFile)
			_ = os.RemoveAll(filepath.Join(app.Paths.DataDir, "tools"))
			_ = os.MkdirAll(filepath.Join(app.Paths.DataDir, "tools"), 0755)
			app.Notify("All tmux-shunpo data reset", false)
		}
	default:
		return fmt.Errorf("invalid reset scope '%s'; use 'session' or 'all'", scope)
	}
	return nil
}

func showUsage(out io.Writer) {
	fmt.Fprintln(out, `Usage: tmux-shunpo [OPTION]

Session Navigation:
  --goto N               Jump to mark slot N (1-9) via sesh connect
  --goto @N              Navigate to tool window slot N (1-9)
  --connect <session>    Connect to session by name (saves/restores window state)

Mark Management:
  --add-mark             Mark current session (inside tmux) or current directory (outside tmux)
  --remove N             Remove mark slot N
  --remove all           Remove all marks
  --marks                Interactive mark editor (gum TUI, tmux popup when from keybinding)
  --compact-marks        Compact mark slots (remove gaps)

Tool Management:
  --tools                Interactive tool editor for current session (gum TUI, tmux popup)
  --add-tool             Append current window to next empty tool slot as @attached
  --compact-tools        Compact tool slots for current session (remove gaps)
  --bootstrap [preset]   Nuke existing windows and setup session tools from [preset] or interactive selection

Maintenance:
  --label-windows        Label all current-session windows with @key or #index
  --reset [session|all]  Reset tools for current session, or all data
  doctor, --doctor       Diagnose dependencies and config parsing

Info:
  -h, --help             Show usage
  -v, --version          Show version
  --completion [shell]   Print shell completion script (bash, zsh, fish)

Configuration:
  ~/.config/tmux-shunpo/config.toml    UI/behavior settings and presets
  ~/.config/sesh/sesh.toml             Session + window templates
  ~/.local/share/tmux-shunpo/marks     Mark storage
  ~/.local/share/tmux-shunpo/tools/    Per-session tool storage

Tool initialization sources (chosen on first tool access):
  1. sesh.toml    [[session]].windows / [[wildcard]].windows / [default_session].windows
  2. [presets]    load slots from a preset, or start empty`)
}

func main() {
	app := &App{
		Paths:  resolvePaths(),
		Runner: RealRunner{},
		Stdout: os.Stdout,
		Stderr: os.Stderr,
	}

	// Warn about deprecated config.conf
	if _, err := os.Stat(filepath.Join(app.Paths.ConfigDir, "config.conf")); err == nil {
		if _, err := os.Stat(app.Paths.ConfigFile); os.IsNotExist(err) {
			app.Notify("config.conf is deprecated. Create config.toml (see --help)", true)
		}
	}

	action := ""
	if len(os.Args) >= 2 {
		action = os.Args[1]
	}

	// Context-aware dependency checking
	var err error
	switch action {
	case "--list-marks", "--completion", "--_complete", "doctor", "--doctor", "-h", "--help", "-v", "--version", "":
		// no dependencies needed
	case "--marks", "--tools", "--bootstrap":
		err = app.checkDependencies(true, true)
	case "--goto", "--connect", "--add-mark", "--add", "--add-tool", "--remove", "--reset", "--compact-marks", "--compact-tools", "--label-windows":
		err = app.checkDependencies(false, false)
	default:
		err = app.checkDependencies(true, false)
	}
	if err != nil {
		app.errorExit(err.Error(), 1)
	}

	if action == "doctor" || action == "--doctor" {
		os.Exit(app.cmdDoctor())
	}

	// Load configuration
	app.Config, err = loadConfig(app.Paths.ConfigFile, app.Stderr)
	if err != nil {
		app.errorExit("failed to load configuration: "+err.Error(), 1)
	}

	app.SeshConfig, err = loadSeshConfig(app.Paths.SeshConfigFile)
	if err != nil {
		app.errorExit("failed to load sesh configuration: "+err.Error(), 1)
	}

	switch action {
	case "-h", "--help", "":
		showUsage(app.Stdout)
		os.Exit(0)

	case "-v", "--version":
		fmt.Fprintln(app.Stdout, "tmux-shunpo v"+Version)
		os.Exit(0)

	case "--list-marks":
		marks, err := parseMarks(app.Paths.MarksFile)
		if err == nil {
			for i := MinSlot; i <= MaxSlot; i++ {
				if val, ok := marks[i]; ok {
					fmt.Fprintf(app.Stdout, "%d: %s\n", i, val)
				}
			}
		}
		os.Exit(0)

	case "--connect":
		if len(os.Args) < 3 {
			app.errorExit("error: --connect requires a session name", 128)
		}
		if err := app.sessionConnectWithState(os.Args[2]); err != nil {
			app.errorExit(err.Error(), 1)
		}

	case "--goto":
		if len(os.Args) < 3 {
			app.errorExit("error: --goto requires a target (mark slot or @tool)", 128)
		}
		target := os.Args[2]
		if strings.HasPrefix(target, "@") {
			slot, err := strconv.Atoi(target[1:])
			if err != nil || slot < MinSlot || slot > MaxSlot {
				app.errorExit("invalid tool slot index", 128)
			}
			if err := app.navigateToTool(slot); err != nil {
				app.errorExit(err.Error(), 1)
			}
		} else {
			slot, err := strconv.Atoi(target)
			if err == nil {
				val, err := parseMarksSlot(app.Paths.MarksFile, slot)
				if err != nil {
					app.Notify(fmt.Sprintf("Mark %d not configured (use --marks to edit marks)", slot), false)
					if isatty(os.Stdin.Fd()) {
						os.Exit(1)
					}
					os.Exit(0)
				}
				if err := app.sessionConnectWithState(val); err != nil {
					app.errorExit(err.Error(), 1)
				}
			} else {
				app.errorExit("invalid mark slot", 128)
			}
		}

	case "--add-mark", "--add":
		slot, ref, err := app.markAddCurrent()
		if err != nil {
			if err.Error() == "already marked" {
				if isatty(os.Stdin.Fd()) {
					fmt.Fprintf(app.Stdout, "Already marked in slot %d: %s\n", slot, ref)
				} else {
					app.Notify(fmt.Sprintf("Already marked in slot %d", slot), false)
				}
				os.Exit(0)
			}
			app.errorExit(err.Error(), 1)
		}

		if isatty(os.Stdin.Fd()) {
			fmt.Fprintf(app.Stdout, "Added mark %d: %s\n", slot, ref)
		} else {
			app.Notify(fmt.Sprintf("Added mark %d", slot), false)
		}

	case "--remove":
		if len(os.Args) < 3 {
			app.errorExit("error: --remove requires a target (mark slot number or 'all')", 128)
		}
		if err := markRemove(app.Paths.MarksFile, os.Args[2]); err != nil {
			app.errorExit(err.Error(), 1)
		}

	case "--marks":
		if err := app.uiEditMarks(); err != nil {
			app.errorExit(err.Error(), 1)
		}

	case "--compact-marks":
		_ = markRearrange(app.Paths.MarksFile)
		app.Notify("Marks compacted", false)

	case "--tools":
		if err := app.uiEditTools(); err != nil {
			app.errorExit(err.Error(), 1)
		}

	case "--add-tool":
		slot, err := app.toolAddCurrentWindow()
		if err != nil {
			app.errorExit(err.Error(), 1)
		}
		app.Notify(fmt.Sprintf("Added current window to tool @%d", slot), false)

	case "--compact-tools":
		sessionNameBytes, err := app.Runner.Run("tmux", []string{"display-message", "-p", "#S"}, nil)
		if err != nil {
			app.errorExit("not in a tmux session", 1)
		}
		sessionName := strings.TrimSpace(string(sessionNameBytes))
		_ = app.toolCompact(sessionName)
		app.Notify("Tools compacted", false)

	case "--bootstrap":
		force := false
		keep := false
		preset := ""
		for i := 2; i < len(os.Args); i++ {
			if os.Args[i] == "--force" {
				force = true
			} else if os.Args[i] == "--keep" {
				keep = true
			} else {
				preset = os.Args[i]
			}
		}

		if preset == "" {
			if !isatty(os.Stdin.Fd()) && os.Getenv("TMUX") != "" {
				extra := []string{}
				if force {
					extra = append(extra, "--force")
				}
				if keep {
					extra = append(extra, "--keep")
				}
				app.ensureTTYOrPopup("--bootstrap", extra...)
				os.Exit(0)
			}
			if err := app.uiBootstrap(force); err != nil {
				app.errorExit(err.Error(), 1)
			}
		} else {
			if err := app.cmdBootstrap(preset, force, keep); err != nil {
				app.errorExit(err.Error(), 1)
			}
		}

	case "--label-windows":
		if err := app.cmdLabelWindows(); err != nil {
			app.Notify(err.Error(), true)
			os.Exit(1)
		}

	case "--reset":
		scope := "session"
		if len(os.Args) >= 3 {
			scope = os.Args[2]
		}
		if err := app.dataReset(scope); err != nil {
			app.errorExit(err.Error(), 1)
		}

	case "--completion":
		if len(os.Args) < 3 {
			app.errorExit("error: --completion requires a shell: bash, zsh, fish", 128)
		}
		shell := os.Args[2]
		switch shell {
		case "bash":
			fmt.Fprint(app.Stdout, completionBash())
		case "zsh":
			fmt.Fprint(app.Stdout, completionZsh())
		case "fish":
			fmt.Fprint(app.Stdout, completionFish())
		default:
			app.errorExit("unknown shell '"+shell+"'; supported: bash, zsh, fish", 128)
		}

	case "--_complete":
		if len(os.Args) < 3 {
			os.Exit(0)
		}
		app.handleComplete(os.Args[2])

	default:
		app.errorExit("unknown option '"+action+"'; use --help for usage information", 128)
	}
}
