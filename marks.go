package main

import (
	"bufio"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
)

// parseMarks reads the marks file and returns a map of slot to session reference
func parseMarks(marksFile string) (map[int]string, error) {
	marks := make(map[int]string)
	if _, err := os.Stat(marksFile); os.IsNotExist(err) {
		return marks, nil
	}

	file, err := os.Open(marksFile)
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

		slotStr := strings.TrimSpace(parts[0])
		val := strings.TrimSpace(parts[1])
		if val == "" {
			continue
		}

		slot, err := strconv.Atoi(slotStr)
		if err != nil || slot < 1 || slot > 9 {
			continue
		}

		marks[slot] = val
	}

	return marks, scanner.Err()
}

// parseMarksSlot retrieves a single mark slot
func parseMarksSlot(marksFile string, targetSlot int) (string, error) {
	marks, err := parseMarks(marksFile)
	if err != nil {
		return "", err
	}
	val, ok := marks[targetSlot]
	if !ok {
		return "", fmt.Errorf("mark slot %d not found", targetSlot)
	}
	return val, nil
}

func isPathLikeMarkRef(ref string) bool {
	ref = strings.TrimSpace(ref)
	return ref == "~" || strings.HasPrefix(ref, "/") || strings.HasPrefix(ref, "~/") || strings.HasPrefix(ref, "./") || strings.HasPrefix(ref, "../") || strings.Contains(ref, "/")
}

func markRefMatchesPath(ref, path string) bool {
	if path == "" || !isPathLikeMarkRef(ref) {
		return false
	}
	return normalizePath(ref) == normalizePath(path)
}

// markSet writes a session reference to a specific mark slot
func markSet(marksFile string, slot int, sessionRef string) error {
	if err := validateRange(slot, MinSlot, MaxSlot, "Slot must be between 1-9"); err != nil {
		return err
	}
	if strings.TrimSpace(sessionRef) == "" {
		return fmt.Errorf("cannot set empty mark")
	}
	if err := validateMarkEntry(sessionRef); err != nil {
		return err
	}

	marks, err := parseMarks(marksFile)
	if err != nil {
		return err
	}

	marks[slot] = sessionRef

	content := "# Marks for tmux-shunpo\n# Format: SLOT: PATH or SESSION"
	for i := MinSlot; i <= MaxSlot; i++ {
		if val, ok := marks[i]; ok {
			content += fmt.Sprintf("\n%d: %s", i, val)
		}
	}
	content += "\n"

	return atomicWrite(marksFile, content)
}

// markAdd adds the current session or directory to the marks file
func markAdd(marksFile string, insideTmux bool, currentSession, currentDir string) (int, string, error) {
	var sessionRef string
	if insideTmux {
		if currentSession == "" {
			return 0, "", fmt.Errorf("cannot determine current session")
		}
		sessionRef = currentSession
	} else {
		sessionRef = normalizePath(currentDir)
	}

	marks, err := parseMarks(marksFile)
	if err != nil {
		return 0, "", err
	}

	// Check for duplicates. A mark may be stored as either the tmux session name
	// or the path used to create that session from sesh (including a literal
	// ~/path chosen in the marks editor), so compare path-like refs by their
	// normalized filesystem path.
	normalizedDir := normalizePath(currentDir)
	for slot, val := range marks {
		if val == sessionRef || val == normalizedDir || markRefMatchesPath(val, normalizedDir) {
			return slot, sessionRef, fmt.Errorf("already marked")
		}
	}

	// Find next empty slot
	nextSlot := MinSlot
	for {
		if _, ok := marks[nextSlot]; !ok {
			break
		}
		nextSlot++
		if nextSlot > MaxSlot {
			return 0, "", fmt.Errorf("all mark slots (%d-%d) are full", MinSlot, MaxSlot)
		}
	}

	if err := markSet(marksFile, nextSlot, sessionRef); err != nil {
		return 0, "", err
	}

	return nextSlot, sessionRef, nil
}

// markRemove deletes a mark slot
func markRemove(marksFile string, target string) error {
	if target == "" {
		return fmt.Errorf("no target specified for remove")
	}

	if target == "all" {
		content := "# Marks for tmux-shunpo\n"
		return atomicWrite(marksFile, content)
	}

	slot, err := strconv.Atoi(target)
	if err != nil {
		return fmt.Errorf("mark slot must be a number or 'all'")
	}

	marks, err := parseMarks(marksFile)
	if err != nil {
		return err
	}

	if _, ok := marks[slot]; !ok {
		return fmt.Errorf("mark slot %d does not exist", slot)
	}

	delete(marks, slot)

	content := "# Marks for tmux-shunpo"
	for i := MinSlot; i <= MaxSlot; i++ {
		if val, ok := marks[i]; ok {
			content += fmt.Sprintf("\n%d: %s", i, val)
		}
	}
	content += "\n"

	return atomicWrite(marksFile, content)
}

// markRearrange compacts the mark slots by eliminating gaps
func markRearrange(marksFile string) error {
	marks, err := parseMarks(marksFile)
	if err != nil {
		return err
	}

	if len(marks) == 0 {
		return nil
	}

	// Extract and sort slot numbers to preserve order during rearrangement
	var slots []int
	for slot := range marks {
		slots = append(slots, slot)
	}
	sort.Ints(slots)

	content := "# Marks for tmux-shunpo"
	newSlot := 1
	for _, slot := range slots {
		content += fmt.Sprintf("\n%d: %s", newSlot, marks[slot])
		newSlot++
	}
	content += "\n"

	return atomicWrite(marksFile, content)
}

// markRemoveInvalid removes an invalid path mark from the file
func markRemoveInvalid(marksFile string, targetSlot int, invalidPath string) error {
	if _, err := os.Stat(marksFile); os.IsNotExist(err) {
		return nil
	}

	marks, err := parseMarks(marksFile)
	if err != nil {
		return err
	}

	if val, ok := marks[targetSlot]; ok && val == invalidPath {
		delete(marks, targetSlot)
	} else {
		return nil
	}

	content := "# Marks for tmux-shunpo"
	for i := MinSlot; i <= MaxSlot; i++ {
		if val, ok := marks[i]; ok {
			content += fmt.Sprintf("\n%d: %s", i, val)
		}
	}
	content += "\n"

	return atomicWrite(marksFile, content)
}
