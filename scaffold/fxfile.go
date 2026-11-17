package scaffold

import (
	"fmt"
	"os"
	"strings"
)

// addImportLine inserts importPath into the import block of path.
func addImportLine(path, importPath, module string) error {
	content, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}

	lines := strings.Split(string(content), "\n")

	anchor := -1
	for i, l := range lines {
		if strings.Contains(l, module+"/") {
			anchor = i
		}
	}

	if anchor == -1 {
		return fmt.Errorf("no existing %q import found in %s to anchor the new import next to",
			module, path)
	}

	out := make([]string, 0, len(lines)+1)
	out = append(out, lines[:anchor+1]...)
	out = append(out, "\t\""+importPath+"\"")
	out = append(out, lines[anchor+1:]...)

	return os.WriteFile(path, []byte(strings.Join(out, "\n")), 0o644)
}

// removeImportLine removes the line importing importPath, if present.
func removeImportLine(path, importPath string) error {
	return filterLinesContaining(path, "\""+importPath+"\"")
}

// addModuleLine inserts moduleLine after marker in path.
func addModuleLine(path, marker, moduleLine string) error {
	content, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}

	lines := strings.Split(string(content), "\n")

	markerAt := -1
	for i, l := range lines {
		if strings.Contains(l, marker) {
			markerAt = i
			break
		}
	}

	if markerAt == -1 {
		return fmt.Errorf("marker %q not found in %s", marker, path)
	}

	insertAt := markerAt + 1
	if insertAt < len(lines) && strings.Contains(lines[insertAt], "// ===") {
		insertAt++
	}

	out := make([]string, 0, len(lines)+1)
	out = append(out, lines[:insertAt]...)
	out = append(out, "\t\t"+moduleLine)
	out = append(out, lines[insertAt:]...)

	return os.WriteFile(path, []byte(strings.Join(out, "\n")), 0o644)
}

// removeModuleLine removes the line containing moduleLine, if present.
func removeModuleLine(path, moduleLine string) error {
	return filterLinesContaining(path, moduleLine)
}

// filterLinesContaining rewrites path with every line containing needle dropped.
func filterLinesContaining(path, needle string) error {
	content, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}

	lines := strings.Split(string(content), "\n")

	out := lines[:0]
	for _, l := range lines {
		if strings.Contains(l, needle) {
			continue
		}

		out = append(out, l)
	}

	return os.WriteFile(path, []byte(strings.Join(out, "\n")), 0o644)
}
