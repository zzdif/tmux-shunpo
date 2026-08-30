package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"golang.org/x/term"
)

func (app *App) runGumChoose(header string, options []string, selected string) (string, error) {
	args := []string{"choose", "--padding", "0 2"}
	if app.Config.UI.UseNerdFonts {
		args = append(args, "--cursor", "\ueb70 ")
	}
	if header != "" {
		args = append(args, "--header", header)
	}
	if selected != "" {
		args = append(args, "--selected", selected)
	}
	var stdout bytes.Buffer
	stdin := strings.NewReader(strings.Join(options, "\n"))
	err := app.Runner.RunInteractive("gum", args, stdin, &stdout, os.Stderr)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(stdout.String()), nil
}

func (app *App) runGumFilter(header, placeholder string, options []string) (string, error) {
	args := []string{"filter", "--padding", "0 2"}
	if app.Config.UI.UseNerdFonts {
		args = append(args, "--indicator", "\ueb70 ")
	}
	if header != "" {
		args = append(args, "--header", header)
	}
	if placeholder != "" {
		args = append(args, "--placeholder", placeholder)
	}
	var stdout bytes.Buffer
	stdin := strings.NewReader(strings.Join(options, "\n"))
	err := app.Runner.RunInteractive("gum", args, stdin, &stdout, os.Stderr)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(stdout.String()), nil
}

func (app *App) runGumInput(header, placeholder, defaultValue string) (string, error) {
	args := []string{"input", "--padding", "0 2"}
	if header != "" {
		args = append(args, "--header", header)
	}
	if placeholder != "" {
		args = append(args, "--placeholder", placeholder)
	}
	if defaultValue != "" {
		args = append(args, "--value", defaultValue)
	}
	var stdout bytes.Buffer
	err := app.Runner.RunInteractive("gum", args, nil, &stdout, os.Stderr)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(stdout.String()), nil
}

func (app *App) runGumConfirm(prompt string) bool {
	args := []string{"confirm", "--padding", "0 2", "  " + prompt}
	err := app.Runner.RunInteractive("gum", args, os.Stdin, app.Stdout, app.Stderr)
	return err == nil
}

func (app *App) drawHeader(text string) {
	app.drawHeaderWithMargin(text, "1 2 1 2")
}

func (app *App) drawHeaderWithMargin(text, margin string) {
	args := []string{
		"style",
		"--border", "rounded",
		"--padding", "0 2",
		"--margin", margin,
		"--border-foreground", "212",
		"--bold",
		text,
	}
	_ = app.Runner.RunInteractive("gum", args, nil, app.Stdout, app.Stderr)
}

func (app *App) outputWidth() int {
	const fallbackWidth = 80
	file, ok := app.Stdout.(*os.File)
	if !ok || !isatty(file.Fd()) {
		return fallbackWidth
	}
	width, _, err := term.GetSize(int(file.Fd()))
	if err != nil || width <= 0 {
		return fallbackWidth
	}
	return width
}

func (app *App) drawMetadata(sessionName string) {
	termW := app.outputWidth()

	// 1. Clear screen and move cursor to (1,1)
	fmt.Fprint(app.Stdout, "\033[H\033[2J")

	leftText := " " + sessionName
	rightText := "tmux-shunpo v" + Version + " "

	// Ensure they fit
	if len(leftText)+len(rightText) >= termW {
		avail := termW - len(rightText) - 4
		if avail > 1 {
			leftText = " " + sessionName[:avail-1] + "..."
		} else {
			leftText = " "
		}
	}

	paddingW := termW - len(leftText) - len(rightText)
	if paddingW < 1 {
		paddingW = 1
	}

	leftStyled := "\033[2m" + leftText + "\033[0m"
	rightStyled := "\033[38;5;238m" + rightText + "\033[0m"
	paddingStr := strings.Repeat(" ", paddingW)

	fmt.Fprintf(app.Stdout, "%s%s%s\n", leftStyled, paddingStr, rightStyled)
}

func (app *App) uiEditMarks() error {
	// Re-invoke self in tmux popup if we have no TTY and are inside TMUX
	if !isatty(os.Stdin.Fd()) && os.Getenv("TMUX") != "" {
		app.ensureTTYOrPopup("--marks")
		return nil
	}

	sessionName := ""
	sessionNameBytes, err := app.Runner.Run("tmux", []string{"display-message", "-p", "#S"}, nil)
	if err == nil {
		sessionName = strings.TrimSpace(string(sessionNameBytes))
	}

	for {
		marks, err := parseMarks(app.Paths.MarksFile)
		if err != nil {
			return err
		}

		app.drawMetadata(sessionName)
		app.drawHeader("Marks")

		for i := 1; i <= 9; i++ {
			if val, ok := marks[i]; ok {
				if app.Config.UI.UseNerdFonts {
					fmt.Fprintf(app.Stdout, "  \U000f0752  %d  %s\n", i, val)
				} else {
					fmt.Fprintf(app.Stdout, "  %d  %s\n", i, val)
				}
			} else {
				if app.Config.UI.UseNerdFonts {
					fmt.Fprintf(app.Stdout, "  \U000f0751  %d  (empty)\n", i)
				} else {
					fmt.Fprintf(app.Stdout, "  %d  (empty)\n", i)
				}
			}
		}
		fmt.Fprintln(app.Stdout)

		action, err := app.runGumChoose("", []string{"Set mark", "Clear mark", "Compact marks", "Done"}, "")
		if err != nil {
			break
		}

		switch action {
		case "Set mark":
			var choices []string
			var defaultChoice string
			for i := 1; i <= 9; i++ {
				var choiceStr string
				if val, ok := marks[i]; ok {
					if app.Config.UI.UseNerdFonts {
						choiceStr = fmt.Sprintf("\U000f0752 %d: %s", i, val)
					} else {
						choiceStr = fmt.Sprintf("%d: %s", i, val)
					}
				} else {
					if app.Config.UI.UseNerdFonts {
						choiceStr = fmt.Sprintf("\U000f0751 %d: (empty)", i)
					} else {
						choiceStr = fmt.Sprintf("%d: (empty)", i)
					}
					if defaultChoice == "" {
						defaultChoice = choiceStr
					}
				}
				choices = append(choices, choiceStr)
			}

			slotChoiceStr, err := app.runGumChoose("  Select slot", choices, defaultChoice)
			if err != nil {
				continue
			}
			slot, err := parseSlotFromOption(slotChoiceStr)
			if err != nil {
				continue
			}

			method, err := app.runGumChoose("  Source", []string{"Pick from sesh sessions", "Enter manually"}, "")
			if err != nil {
				continue
			}

			var newValue string
			switch method {
			case "Pick from sesh sessions":
				// Run sesh list -t -c -z
				seshOut, err := app.Runner.Run("sesh", []string{"list", "-t", "-c", "-z"}, nil)
				if err != nil {
					continue
				}
				lines := strings.Split(strings.TrimSpace(string(seshOut)), "\n")
				var cleanLines []string
				for _, line := range lines {
					if l := strings.TrimSpace(line); l != "" {
						cleanLines = append(cleanLines, l)
					}
				}
				newValue, err = app.runGumFilter("  Select session", "  Filter...", cleanLines)
				if err != nil {
					continue
				}

			case "Enter manually":
				current := marks[slot]
				newValue, err = app.runGumInput(fmt.Sprintf("  Session name or path for slot %d", slot), "session-name or ~/path", current)
				if err != nil {
					continue
				}
			}

			newValue = strings.TrimSpace(newValue)
			if newValue == "" {
				continue
			}
			_ = markSet(app.Paths.MarksFile, slot, newValue)

		case "Clear mark":
			var filled []string
			for i := 1; i <= 9; i++ {
				if val, ok := marks[i]; ok {
					if app.Config.UI.UseNerdFonts {
						filled = append(filled, fmt.Sprintf("\U000f0752 %d: %s", i, val))
					} else {
						filled = append(filled, fmt.Sprintf("%d: %s", i, val))
					}
				}
			}
			if len(filled) == 0 {
				args := []string{"style", "--margin", "0 2", "--foreground", "208", "No marks to clear"}
				_ = app.Runner.RunInteractive("gum", args, nil, app.Stdout, app.Stderr)
				timeSleep(1000)
				continue
			}

			toClear, err := app.runGumChoose("  Select mark to clear", filled, "")
			if err != nil {
				continue
			}
			slot, err := parseSlotFromOption(toClear)
			if err != nil {
				continue
			}
			_ = markRemove(app.Paths.MarksFile, strconv.Itoa(slot))

		case "Compact marks":
			_ = markRearrange(app.Paths.MarksFile)
			args := []string{"style", "--margin", "0 2", "--foreground", "82", "Marks compacted"}
			_ = app.Runner.RunInteractive("gum", args, nil, app.Stdout, app.Stderr)
			timeSleep(500)

		case "Done":
			return nil
		}
	}
	return nil
}

type toolOverviewRow struct {
	Icon   string
	Slot   int
	Window string
	Source string
}

func toolOverviewIcon(command string, useNerdFonts bool) string {
	if !useNerdFonts {
		return ""
	}
	if command == "" {
		return "\U000f0751"
	}
	if !strings.HasPrefix(command, "@") {
		return "\uf155"
	}
	switch strings.TrimPrefix(command, "@") {
	case "shell":
		return "\ue795"
	case "attached":
		return "\uf0c6"
	default:
		return "\U000f1a7c"
	}
}

func toolOverviewSource(command string, templates map[string]string) string {
	if command == "" {
		return "empty"
	}
	if !strings.HasPrefix(command, "@") {
		return command
	}
	if command == "@attached" {
		return command
	}

	resolved := templates[strings.TrimPrefix(command, "@")]
	if resolved == "" {
		resolved = "?"
	}
	return command + " → " + resolved
}

func truncateOverviewText(text string, maxRunes int) string {
	runes := []rune(text)
	if len(runes) <= maxRunes {
		return text
	}
	if maxRunes <= 1 {
		return "…"
	}
	return string(runes[:maxRunes-1]) + "…"
}

func formatToolOverview(rows []toolOverviewRow, terminalWidth int) string {
	const (
		fallbackWidth     = 80
		widthSafety       = 2 // Nerd Font glyphs may occupy an extra terminal cell.
		fixedPrefixWidth  = 13
		columnGapWidth    = 2
		minWindowWidth    = 18
		maxWindowWidth    = 24
		minSourceWidth    = 12
		preferredSource   = 40
		minimumLineBudget = fixedPrefixWidth + columnGapWidth + 1
	)
	if terminalWidth <= widthSafety {
		terminalWidth = fallbackWidth
	}
	lineBudget := terminalWidth - widthSafety
	if lineBudget < minimumLineBudget {
		lineBudget = minimumLineBudget
	}

	formattedRows := make([]toolOverviewRow, len(rows))
	copy(formattedRows, rows)
	windowWidth := minWindowWidth
	for _, row := range formattedRows {
		if width := len([]rune(row.Window)); width > windowWidth {
			windowWidth = min(width, maxWindowWidth)
		}
	}

	availableColumns := lineBudget - fixedPrefixWidth - columnGapWidth
	if maxWindow := availableColumns - minSourceWidth; windowWidth > maxWindow {
		windowWidth = max(1, maxWindow)
	}
	sourceWidth := min(preferredSource, max(1, availableColumns-windowWidth))

	for i := range formattedRows {
		formattedRows[i].Window = truncateOverviewText(formattedRows[i].Window, windowWidth)
		formattedRows[i].Source = truncateOverviewText(formattedRows[i].Source, sourceWidth)
	}

	var out strings.Builder
	fmt.Fprintf(&out, "       SLOT  %-*s  SOURCE\n", windowWidth, "WINDOW")
	for _, row := range formattedRows {
		fmt.Fprintf(&out, "  %-2s   @%d    %-*s  %s\n", row.Icon, row.Slot, windowWidth, row.Window, row.Source)
	}
	return out.String()
}

func (app *App) toolOverviewWindow(sessionName string, slot int, expectedLabel string) string {
	target := fmt.Sprintf("%s:%d", sessionName, app.Config.ToolWindowBase+slot-1)
	state, err := app.readWindowLabelState(target)
	if err != nil || state.Name == "" {
		return joinWindowLabel(expectedLabel, "—")
	}
	return state.Name
}

func (app *App) uiEditTools() error {
	if !isatty(os.Stdin.Fd()) && os.Getenv("TMUX") != "" {
		app.ensureTTYOrPopup("--tools")
		return nil
	}

	sessionNameBytes, err := app.Runner.Run("tmux", []string{"display-message", "-p", "#S"}, nil)
	if err != nil {
		return fmt.Errorf("not in a tmux session")
	}
	sessionName := strings.TrimSpace(string(sessionNameBytes))

	if err := validateSessionName(sessionName); err != nil {
		return err
	}

	// Load templates once
	templates := make(map[string]string)
	for _, w := range app.SeshConfig.Window {
		templates[w.Name] = w.StartupScript
	}
	shellName := filepath.Base(app.defaultShell())
	if shellName == "" || shellName == "." {
		shellName = "?"
	}
	templates["shell"] = shellName

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

	for {
		// Drop @attached entries whose windows were closed before rendering so
		// the listing reflects live tmux state, not stale tool-file rows.
		reclaimed, _ := app.reconcileAttachedTools(sessionName)
		if len(reclaimed) > 0 {
			slots := make([]string, len(reclaimed))
			for i, s := range reclaimed {
				slots[i] = fmt.Sprintf("@%d", s)
			}
			args := []string{"style", "--margin", "0 2", "--foreground", "82",
				"Reclaimed stale slot(s): " + strings.Join(slots, ", ")}
			_ = app.Runner.RunInteractive("gum", args, nil, app.Stdout, app.Stderr)
			timeSleep(800)
		}

		// Read tools
		toolCmds, _, _ := app.loadToolsMap(sessionName)

		app.drawMetadata(sessionName)
		app.drawHeaderWithMargin("Tools", "0 2 1 2")

		bindings := app.discoverToolBindings()
		rows := make([]toolOverviewRow, 0, MaxSlot)
		for i := MinSlot; i <= MaxSlot; i++ {
			cmd, configured := toolCmds[i]
			row := toolOverviewRow{
				Icon:   toolOverviewIcon(cmd, app.Config.UI.UseNerdFonts),
				Slot:   i,
				Window: "—",
				Source: "empty",
			}
			if configured {
				expectedLabel, _ := app.expectedToolWindowLabel(i, bindings)
				row.Window = app.toolOverviewWindow(sessionName, i, expectedLabel)
				row.Source = toolOverviewSource(cmd, templates)
			}
			rows = append(rows, row)
		}
		fmt.Fprint(app.Stdout, formatToolOverview(rows, app.outputWidth()))

		action, err := app.runGumChoose("", []string{"Set tool", "Clear tool", "Compact tools", "Label windows", "Done"}, "")
		if err != nil {
			break
		}

		switch action {
		case "Set tool":
			var choices []string
			var defaultChoice string
			for i := MinSlot; i <= MaxSlot; i++ {
				var choiceStr string
				if cmd, ok := toolCmds[i]; ok {
					var icon string
					if app.Config.UI.UseNerdFonts {
						icon = "\uf155 "
						if strings.HasPrefix(cmd, "@") {
							tmpl := cmd[1:]
							switch tmpl {
							case "shell":
								icon = "\ue795 "
							case "attached":
								icon = "\uf0c6 "
							default:
								icon = "\U000f1a7c "
							}
						}
					}
					if strings.HasPrefix(cmd, "@") {
						tmpl := cmd[1:]
						expanded := templates[tmpl]
						if expanded == "" {
							expanded = "?"
						}
						if tmpl == "attached" {
							winIdx := app.Config.ToolWindowBase + i - 1
							out, err := app.Runner.Run("tmux", []string{"display-message", "-p", "-t", fmt.Sprintf(":%d", winIdx), "#{window_name}"}, nil)
							if err == nil {
								expanded = strings.TrimSpace(string(out))
							} else {
								expanded = "missing"
							}
						}
						choiceStr = fmt.Sprintf("%s%d: %s (→ %s)", icon, i, cmd, expanded)
					} else {
						choiceStr = fmt.Sprintf("%s%d: %s", icon, i, cmd)
					}
				} else {
					if app.Config.UI.UseNerdFonts {
						choiceStr = fmt.Sprintf("\U000f0751 %d: (empty)", i)
					} else {
						choiceStr = fmt.Sprintf("%d: (empty)", i)
					}
					if defaultChoice == "" {
						defaultChoice = choiceStr
					}
				}
				choices = append(choices, choiceStr)
			}

			slotChoiceStr, err := app.runGumChoose("  Select slot", choices, defaultChoice)
			if err != nil {
				continue
			}
			slot, err := parseSlotFromOption(slotChoiceStr)
			if err != nil {
				continue
			}

			if currentCmd, ok := toolCmds[slot]; ok {
				if !app.runGumConfirm(fmt.Sprintf("Slot @%d already has '%s'. Overwrite?", slot, currentCmd)) {
					continue
				}
			}

			method, err := app.runGumChoose("  Source", []string{
				"Pick from existing window",
				"Pick from sesh window templates",
				"Enter command manually",
				"Shell (default shell)",
			}, "")
			if err != nil {
				continue
			}

			var newCmd string
			switch method {
			case "Pick from existing window":
				winList, err := app.getNonToolWindows(sessionName)
				if err != nil || len(winList) == 0 {
					args := []string{"style", "--margin", "0 2", "--foreground", "208", "No regular windows found"}
					_ = app.Runner.RunInteractive("gum", args, nil, app.Stdout, app.Stderr)
					timeSleep(1000)
					continue
				}
				picked, err := app.runGumChoose("  Select window", winList, "")
				if err != nil {
					continue
				}
				pickedIdx := strings.Index(picked, " ")
				if pickedIdx == -1 {
					continue
				}
				_ = app.assignWindowToTool(sessionName, slot, picked[:pickedIdx])
				continue

			case "Pick from sesh window templates":
				var tmplNames []string
				for name := range templates {
					if name == "shell" {
						hasSeshShell := false
						for _, w := range app.SeshConfig.Window {
							if w.Name == "shell" {
								hasSeshShell = true
								break
							}
						}
						if !hasSeshShell {
							continue
						}
					}
					tmplNames = append(tmplNames, name)
				}
				if len(tmplNames) == 0 {
					args := []string{"style", "--margin", "0 2", "--foreground", "208", "No window templates in sesh.toml"}
					_ = app.Runner.RunInteractive("gum", args, nil, app.Stdout, app.Stderr)
					timeSleep(1000)
					continue
				}
				picked, err := app.runGumFilter("  Select template", "  Filter...", tmplNames)
				if err != nil {
					continue
				}
				newCmd = "@" + picked

			case "Enter command manually":
				current := toolCmds[slot]
				newCmd, err = app.runGumInput(fmt.Sprintf("  Command for slot @%d", slot), "e.g. nvim . or cargo watch", current)
				if err != nil {
					continue
				}
				if !strings.HasPrefix(newCmd, "@") {
					if err := validateCommand(newCmd); err != nil {
						args := []string{"style", "--margin", "0 2", "--foreground", "196", err.Error()}
						_ = app.Runner.RunInteractive("gum", args, nil, app.Stdout, app.Stderr)
						timeSleep(1500)
						continue
					}
					if word, ok := app.isDestructive(newCmd); ok {
						prompt := fmt.Sprintf("⚠ %q looks destructive (%s). Save it to slot @%d anyway?", strings.TrimSpace(newCmd), word, slot)
						if !app.confirmDestructive(prompt) {
							continue
						}
					}
				}

			case "Shell (default shell)":
				newCmd = "@shell"
			}

			newCmd = strings.TrimSpace(newCmd)
			if newCmd == "" {
				continue
			}
			_ = app.toolSet(sessionName, slot, newCmd)

		case "Clear tool":
			var filled []string
			for i := MinSlot; i <= MaxSlot; i++ {
				if val, ok := toolCmds[i]; ok {
					var icon string
					if app.Config.UI.UseNerdFonts {
						icon = "\uf155 "
						if strings.HasPrefix(val, "@") {
							tmpl := val[1:]
							switch tmpl {
							case "shell":
								icon = "\ue795 "
							case "attached":
								icon = "\uf0c6 "
							default:
								icon = "\U000f1a7c "
							}
						}
					}
					filled = append(filled, fmt.Sprintf("%s@%d: %s", icon, i, val))
				}
			}
			if len(filled) == 0 {
				args := []string{"style", "--margin", "0 2", "--foreground", "208", "No tools to clear"}
				_ = app.Runner.RunInteractive("gum", args, nil, app.Stdout, app.Stderr)
				timeSleep(1000)
				continue
			}

			toClear, err := app.runGumChoose("  Select tool to clear", filled, "")
			if err != nil {
				continue
			}
			clearSlot, err := parseSlotFromOption(toClear)
			if err != nil {
				continue
			}
			_ = app.toolRemove(sessionName, clearSlot)

		case "Compact tools":
			_ = app.toolCompact(sessionName)
			args := []string{"style", "--margin", "0 2", "--foreground", "82", "Tools compacted"}
			_ = app.Runner.RunInteractive("gum", args, nil, app.Stdout, app.Stderr)
			timeSleep(500)

		case "Label windows":
			if err := app.cmdLabelWindows(); err != nil {
				app.Notify(err.Error(), true)
			}
			timeSleep(800)

		case "Done":
			return nil
		}
	}
	return nil
}

func (app *App) uiBootstrap(force bool) error {
	sessionName := ""
	sessionNameBytes, err := app.Runner.Run("tmux", []string{"display-message", "-p", "#S"}, nil)
	if err == nil {
		sessionName = strings.TrimSpace(string(sessionNameBytes))
	}

	var choices []string
	var keys []string
	for k := range app.Config.Presets {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, k := range keys {
		v := app.Config.Presets[k]
		preview := strings.Join(v, ", ")
		if len(preview) > 50 {
			preview = preview[:47] + "..."
		}
		choices = append(choices, fmt.Sprintf("%s (→ %s)", k, preview))
	}
	if len(choices) == 0 {
		return fmt.Errorf("no presets defined in configuration; add [presets] to config.toml")
	}

	app.drawMetadata(sessionName)
	app.drawHeader("Bootstrap")
	fmt.Fprintln(app.Stdout)

	picked, err := app.runGumChoose("", choices, "")
	if err != nil || picked == "" {
		return nil
	}

	idx := strings.Index(picked, " (→")
	if idx != -1 {
		picked = picked[:idx]
	}

	strategyOptions := []string{
		"Clean: Close all existing non-tool windows (fresh start)",
		"Keep: Keep existing windows alongside tool windows",
		"Cancel",
	}
	strategy, err := app.runGumChoose("  Bootstrap strategy for session '"+sessionName+"'", strategyOptions, "")
	if err != nil || strategy == "" || strategy == "Cancel" {
		return nil
	}

	keep := false
	if strings.HasPrefix(strategy, "Keep:") {
		keep = true
	}

	return app.cmdBootstrap(picked, force, keep)
}

func (app *App) uiInitTools(sessionName string) (bool, error) {
	if !isatty(os.Stdin.Fd()) && os.Getenv("TMUX") != "" {
		app.ensureTTYOrPopup("--tools")
		return false, nil
	}

	for {
		app.drawMetadata(sessionName)
		app.drawHeader("Initialize Tools")
		fmt.Fprintln(app.Stdout)

		options := []string{
			"Load from sesh.toml",
			"Load slots from preset",
			"Start empty (manual setup)",
			"Cancel",
		}

		choice, err := app.runGumChoose("", options, "")
		if err != nil || choice == "Cancel" || choice == "" {
			return false, err
		}

		switch choice {
		case "Load from sesh.toml":
			err := app.toolInitFromDefaults(sessionName)
			if err != nil {
				return false, err
			}
			toolsFile := filepath.Join(app.Paths.DataDir, "tools", sessionName)
			if _, err := os.Stat(toolsFile); os.IsNotExist(err) {
				args := []string{"style", "--margin", "0 2", "--foreground", "208", "No sesh.toml tool defaults found"}
				_ = app.Runner.RunInteractive("gum", args, nil, app.Stdout, app.Stderr)
				timeSleep(1000)
				continue
			}
			args := []string{"style", "--margin", "0 2", "--foreground", "82", "Loaded tools from sesh.toml"}
			_ = app.Runner.RunInteractive("gum", args, nil, app.Stdout, app.Stderr)
			timeSleep(800)
			return true, nil

		case "Load slots from preset":
			var presetNames []string
			for k := range app.Config.Presets {
				presetNames = append(presetNames, k)
			}
			if len(presetNames) == 0 {
				args := []string{"style", "--margin", "0 2", "--foreground", "208", "No presets defined in configuration"}
				_ = app.Runner.RunInteractive("gum", args, nil, app.Stdout, app.Stderr)
				timeSleep(1000)
				continue
			}

			picked, err := app.runGumChoose("Select preset:", presetNames, "")
			if err != nil || picked == "" {
				continue
			}

			toolsToApply := app.Config.Presets[picked]
			for i, cmd := range toolsToApply {
				slot := i + 1
				if cmd != "" {
					_ = app.toolSet(sessionName, slot, cmd)
				}
			}
			args := []string{"style", "--margin", "0 2", "--foreground", "82", fmt.Sprintf("Initialized with preset '%s'", picked)}
			_ = app.Runner.RunInteractive("gum", args, nil, app.Stdout, app.Stderr)
			timeSleep(800)
			return true, nil

		case "Start empty (manual setup)":
			toolsFile := filepath.Join(app.Paths.DataDir, "tools", sessionName)
			content := "# Tools for session: " + sessionName + "\n"
			err := atomicWrite(toolsFile, content)
			if err != nil {
				return false, err
			}
			args := []string{"style", "--margin", "0 2", "--foreground", "82", "Initialized empty tools file"}
			_ = app.Runner.RunInteractive("gum", args, nil, app.Stdout, app.Stderr)
			timeSleep(800)
			return true, nil
		}
	}
}

func parseSlotFromOption(opt string) (int, error) {
	idx := strings.Index(opt, ":")
	if idx == -1 {
		return 0, fmt.Errorf("invalid option format")
	}
	prefix := opt[:idx]
	var digitSB strings.Builder
	for _, r := range prefix {
		if r >= '0' && r <= '9' {
			digitSB.WriteRune(r)
		}
	}
	return strconv.Atoi(digitSB.String())
}
