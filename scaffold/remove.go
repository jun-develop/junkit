package scaffold

import (
	"fmt"
	"os"

	"github.com/jun-develop/junkit/console"
)

// RemoveOptions configures a Remove call — the inverse of GenerateOptions.
type RemoveOptions struct {
	// Dirs are whole directories removed via os.RemoveAll (e.g. the feature
	// directory itself).
	Dirs []string

	// ExtraFiles are individual files removed, if present (e.g. a
	// hand-written sqlc file or queries file living outside Dirs).
	ExtraFiles []string

	// FxFile, ImportPath, and ModuleLine mirror GenerateOptions's fields and
	// undo the registration Generate performed.
	FxFile     string
	ImportPath string
	ModuleLine string
}

// Remove undoes a prior Generate: it unregisters the module from Fx, then deletes the
// extra files and feature directories.
func Remove(cfg RemoveOptions) error {
	if err := removeModuleLine(cfg.FxFile, cfg.ModuleLine); err != nil {
		return fmt.Errorf("scaffold: remove module line: %w", err)
	}

	if err := removeImportLine(cfg.FxFile, cfg.ImportPath); err != nil {
		return fmt.Errorf("scaffold: remove import: %w", err)
	}

	if err := gofmtPaths([]string{cfg.FxFile}); err != nil {
		console.Warnf("gofmt %s: %v", cfg.FxFile, err)
	}

	console.Successf("Removed %s from %s", cfg.ImportPath, cfg.FxFile)

	for _, f := range cfg.ExtraFiles {
		if err := os.Remove(f); err != nil {
			if os.IsNotExist(err) {
				continue
			}

			return fmt.Errorf("scaffold: remove %s: %w", f, err)
		}

		console.Successf("Removed %s", f)
	}

	for _, d := range cfg.Dirs {
		if _, err := os.Stat(d); os.IsNotExist(err) {
			console.Warnf("%s does not exist, nothing to remove", d)
			continue
		}

		if err := os.RemoveAll(d); err != nil {
			return fmt.Errorf("scaffold: remove %s: %w", d, err)
		}

		console.Successf("Removed %s", d)
	}

	return nil
}
