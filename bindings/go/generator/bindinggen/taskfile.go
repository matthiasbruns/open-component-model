package bindinggen

import (
	"fmt"
	"os"
	"strings"
)

// PatchRootTaskfile inserts the include entries for a binding directory (and its
// integration sub-module) into the root Taskfile's `includes:` block, preserving
// existing formatting and comments. relDir is the binding directory relative to the
// repository root, e.g. "bindings/go/wget".
//
// It is idempotent: an include whose key already exists is left untouched. It
// returns whether the file was changed.
func PatchRootTaskfile(taskfilePath, relDir string) (bool, error) {
	data, err := os.ReadFile(taskfilePath)
	if err != nil {
		return false, fmt.Errorf("reading root taskfile: %w", err)
	}
	// Preserve a trailing newline decision by splitting on "\n".
	trailingNewline := strings.HasSuffix(string(data), "\n")
	lines := strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")

	keys := []string{relDir, relDir + "/integration"}

	includesStart := -1
	for i, l := range lines {
		if l == "includes:" {
			includesStart = i
			break
		}
	}
	if includesStart == -1 {
		return false, fmt.Errorf("no top-level 'includes:' block found in %s", taskfilePath)
	}

	// The includes block runs until the next top-level key (column 0) or EOF.
	blockEnd := len(lines)
	for i := includesStart + 1; i < len(lines); i++ {
		if len(lines[i]) > 0 && lines[i][0] != ' ' && lines[i][0] != '\t' {
			blockEnd = i
			break
		}
	}

	existing := existingIncludeKeys(lines, includesStart+1, blockEnd)

	var block []string
	for _, key := range keys {
		if existing[key] {
			continue
		}
		block = append(block, includeEntry(key)...)
	}
	if len(block) == 0 {
		return false, nil
	}

	insertAt := insertionIndex(lines, includesStart+1, blockEnd, relDir)

	out := make([]string, 0, len(lines)+len(block))
	out = append(out, lines[:insertAt]...)
	out = append(out, block...)
	out = append(out, lines[insertAt:]...)

	result := strings.Join(out, "\n")
	if trailingNewline {
		result += "\n"
	}
	if err := os.WriteFile(taskfilePath, []byte(result), 0o644); err != nil {
		return false, fmt.Errorf("writing root taskfile: %w", err)
	}
	return true, nil
}

// includeEntry renders the four-line include block for a directory key.
func includeEntry(key string) []string {
	return []string{
		"  " + key + ":",
		"    optional: true",
		"    taskfile: ./" + key + "/Taskfile.yml",
		"    dir: ./" + key,
	}
}

// isEntryKeyLine reports whether a line is a two-space-indented include key line
// (e.g. "  bindings/go/wget:") rather than one of its indented properties.
func isEntryKeyLine(line string) (string, bool) {
	if !strings.HasPrefix(line, "  ") || strings.HasPrefix(line, "   ") {
		return "", false
	}
	trimmed := strings.TrimSpace(line)
	if !strings.HasSuffix(trimmed, ":") {
		return "", false
	}
	return strings.TrimSuffix(trimmed, ":"), true
}

func existingIncludeKeys(lines []string, start, end int) map[string]bool {
	keys := make(map[string]bool)
	for i := start; i < end; i++ {
		if key, ok := isEntryKeyLine(lines[i]); ok {
			keys[key] = true
		}
	}
	return keys
}

// insertionIndex returns the line index at which a new include for newKey should be
// spliced so keys stay in ascending order. It falls back to the end of the block.
func insertionIndex(lines []string, start, end int, newKey string) int {
	for i := start; i < end; i++ {
		if key, ok := isEntryKeyLine(lines[i]); ok {
			if key > newKey {
				return i
			}
		}
	}
	return end
}
