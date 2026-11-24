// Package scaffold provides code generation and Fx module registration utilities.
package scaffold

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/jun-develop/junkit/console"
	"github.com/jun-develop/junkit/stringx"
)

// FeatureData holds derived name forms for scaffold templates.
type FeatureData struct {
	// Name is the feature name as given, in snake_case (e.g. "product").
	Name string

	// Pascal is Name converted to PascalCase (e.g. "Product").
	Pascal string

	// Camel is Name converted to camelCase (e.g. "product").
	Camel string

	// Plural is Name pluralized (e.g. "categories"); it is the feature's URL path segment.
	Plural string

	// Table is the database table name the feature's migration and queries target.
	Table string

	// Model is the struct sqlc generates for Table: singularized, PascalCase (e.g. "items" -> "Item").
	Model string

	// Module is the target project's Go module path.
	Module string
}

// NewFeatureData derives name forms (PascalCase, camelCase, table) from a feature name.
//
// Usage:
//
//	data := scaffold.NewFeatureData("product", "jungo", "")
//	// data.Pascal == "Product", data.Camel == "product"
//	// data.Table == "products", data.Model == "Product"
func NewFeatureData(name, module, table string) FeatureData {
	plural := pluralize(name)
	if table == "" {
		table = plural
	}

	return FeatureData{
		Name:   name,
		Pascal: stringx.ToPascal(name),
		Camel:  stringx.ToCamel(name),
		Plural: plural,
		Table:  table,
		Model:  stringx.ToPascal(singularize(table)),
		Module: module,
	}
}

// pluralize applies the regular English plural rules (e.g. "category" -> "categories",
// "box" -> "boxes").
func pluralize(s string) string {
	switch {
	case len(s) > 1 && strings.HasSuffix(s, "y") && !strings.ContainsAny(s[len(s)-2:len(s)-1], "aeiou"):
		return s[:len(s)-1] + "ies"
	case strings.HasSuffix(s, "s"), strings.HasSuffix(s, "x"), strings.HasSuffix(s, "z"),
		strings.HasSuffix(s, "ch"), strings.HasSuffix(s, "sh"):
		return s + "es"
	default:
		return s + "s"
	}
}

// singularize mirrors the singular rules sqlc (github.com/jinzhu/inflection) applies to table names
// when naming model structs.
// shortcut: covers regular plurals plus inflection's bus/alias/status cases, not its irregulars
// (people, children, ...); pass the struct name explicitly if a table ever needs one.
func singularize(s string) string {
	for _, suffix := range []string{"buses", "aliases", "statuses", "xes", "ches", "sses", "shes"} {
		if strings.HasSuffix(s, suffix) {
			return s[:len(s)-2]
		}
	}

	switch {
	case len(s) > 3 && strings.HasSuffix(s, "ies") && !strings.ContainsAny(s[len(s)-4:len(s)-3], "aeiouy"):
		return s[:len(s)-3] + "y"
	case strings.HasSuffix(s, "ss"):
		return s
	case strings.HasSuffix(s, "s"):
		return s[:len(s)-1]
	default:
		return s
	}
}

// File represents a single output template file.
type File struct {
	Path     string
	Template string
}

// GenerateOptions configures feature code generation.
type GenerateOptions struct {
	// Data is passed to every entry in Files.
	Data FeatureData

	// Files are the templated files to create.
	Files []File

	// FxFile is the path to the Fx composition root file (e.g. "internal/app/fx.go").
	FxFile string

	// ImportPath is the Go import path added to FxFile's import block.
	ImportPath string

	// Marker is the comment line in FxFile after which ModuleLine is inserted.
	Marker string

	// ModuleLine is the option-list line inserted into FxFile (e.g. "product.Module,").
	ModuleLine string
}

// Generate renders template files and registers the module in the Fx composition root.
func Generate(cfg GenerateOptions) error {
	for _, f := range cfg.Files {
		if _, err := os.Stat(f.Path); err == nil {
			return fmt.Errorf("scaffold: %s already exists — refusing to overwrite", f.Path)
		}
	}

	// Render everything before writing anything, so a bad template leaves no partial feature behind.
	rendered := make([][]byte, len(cfg.Files))
	for i, f := range cfg.Files {
		out, err := renderTemplate(f, cfg.Data)
		if err != nil {
			return fmt.Errorf("scaffold: %w", err)
		}

		rendered[i] = out
	}

	for i, f := range cfg.Files {
		if err := writeFile(f.Path, rendered[i]); err != nil {
			for _, written := range cfg.Files[:i] {
				// Best effort rollback; the write error below is what the caller needs.
				_ = os.Remove(written.Path)
			}

			return fmt.Errorf("scaffold: %w", err)
		}

		console.Successf("Created %s", f.Path)
	}

	if err := gofmtPaths(pathsOf(cfg.Files)); err != nil {
		console.Warnf("gofmt: %v", err)
	}

	console.Stepf("→", "Registering %s in %s", cfg.ImportPath, cfg.FxFile)
	if err := addImportLine(cfg.FxFile, cfg.ImportPath, cfg.Data.Module); err != nil {
		return fmt.Errorf("scaffold: add import: %w", err)
	}

	if err := addModuleLine(cfg.FxFile, cfg.Marker, cfg.ModuleLine); err != nil {
		return fmt.Errorf("scaffold: add module line: %w", err)
	}

	if err := gofmtPaths([]string{cfg.FxFile}); err != nil {
		console.Warnf("gofmt %s: %v", cfg.FxFile, err)
	}

	console.Successf("Registered in %s", cfg.FxFile)

	return nil
}

// renderTemplate renders f's template with data.
func renderTemplate(f File, data FeatureData) ([]byte, error) {
	tmpl, err := template.New(f.Path).Parse(f.Template)
	if err != nil {
		return nil, fmt.Errorf("parse template for %s: %w", f.Path, err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return nil, fmt.Errorf("render %s: %w", f.Path, err)
	}

	return buf.Bytes(), nil
}

// writeFile writes content to path, creating parent directories.
func writeFile(path string, content []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create directory for %s: %w", path, err)
	}

	if err := os.WriteFile(path, content, 0o644); err != nil {
		return fmt.Errorf("create %s: %w", path, err)
	}

	return nil
}

// pathsOf returns the output path of each file.
func pathsOf(files []File) []string {
	paths := make([]string, len(files))
	for i, f := range files {
		paths[i] = f.Path
	}

	return paths
}

// gofmtPaths runs gofmt -w on the .go files among paths.
func gofmtPaths(paths []string) error {
	goFiles := make([]string, 0, len(paths))
	for _, p := range paths {
		if strings.HasSuffix(p, ".go") {
			goFiles = append(goFiles, p)
		}
	}

	if len(goFiles) == 0 {
		return nil
	}

	return exec.Command("gofmt", append([]string{"-w"}, goFiles...)...).Run()
}
