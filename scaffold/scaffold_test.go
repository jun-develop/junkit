package scaffold_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jun-develop/junkit/scaffold"
)

func TestNewFeatureData(t *testing.T) {
	tests := []struct {
		name       string
		featName   string
		table      string
		wantPascal string
		wantCamel  string
		wantTable  string
	}{
		{name: "simple word", featName: "product", table: "", wantPascal: "Product", wantCamel: "product", wantTable: "products"},
		{name: "snake_case with multiple words", featName: "order_item", table: "", wantPascal: "OrderItem", wantCamel: "orderItem", wantTable: "order_items"},
		{name: "explicit table overrides the default", featName: "category", table: "product_categories", wantPascal: "Category", wantCamel: "category", wantTable: "product_categories"},
		{name: "consonant + y pluralizes to ies", featName: "category", table: "", wantPascal: "Category", wantCamel: "category", wantTable: "categories"},
		{name: "vowel + y just adds s", featName: "key", table: "", wantPascal: "Key", wantCamel: "key", wantTable: "keys"},
		{name: "x pluralizes to es", featName: "box", table: "", wantPascal: "Box", wantCamel: "box", wantTable: "boxes"},
		{name: "s pluralizes to es", featName: "status", table: "", wantPascal: "Status", wantCamel: "status", wantTable: "statuses"},
		{name: "ch pluralizes to es", featName: "branch", table: "", wantPascal: "Branch", wantCamel: "branch", wantTable: "branches"},
		{name: "sh pluralizes to es", featName: "dish", table: "", wantPascal: "Dish", wantCamel: "dish", wantTable: "dishes"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := scaffold.NewFeatureData(tt.featName, "jungo", tt.table)

			if data.Name != tt.featName {
				t.Errorf("Name = %q, want %q", data.Name, tt.featName)
			}

			if data.Pascal != tt.wantPascal {
				t.Errorf("Pascal = %q, want %q", data.Pascal, tt.wantPascal)
			}

			if data.Camel != tt.wantCamel {
				t.Errorf("Camel = %q, want %q", data.Camel, tt.wantCamel)
			}

			if data.Table != tt.wantTable {
				t.Errorf("Table = %q, want %q", data.Table, tt.wantTable)
			}

			if data.Module != "jungo" {
				t.Errorf("Module = %q, want %q", data.Module, "jungo")
			}
		})
	}
}

func TestNewFeatureData_PluralAndModel(t *testing.T) {
	tests := []struct {
		featName, table, wantPlural, wantModel string
	}{
		{"product", "", "products", "Product"},
		{"product", "items", "products", "Item"},
		{"user", "users", "users", "User"},
		{"order_item", "", "order_items", "OrderItem"},
		{"category", "", "categories", "Category"},
		{"box", "", "boxes", "Box"},
		{"status", "", "statuses", "Status"},
		{"bus", "", "buses", "Bus"},
		{"class", "", "classes", "Class"},
		{"branch", "", "branches", "Branch"},
		{"key", "", "keys", "Key"},
		{"house", "houses", "houses", "House"},
	}

	for _, tt := range tests {
		t.Run(tt.featName+"/"+tt.table, func(t *testing.T) {
			data := scaffold.NewFeatureData(tt.featName, "jungo", tt.table)

			if data.Plural != tt.wantPlural {
				t.Errorf("Plural = %q, want %q", data.Plural, tt.wantPlural)
			}

			if data.Model != tt.wantModel {
				t.Errorf("Model = %q, want %q", data.Model, tt.wantModel)
			}
		})
	}
}

func TestNextMigrationSeq(t *testing.T) {
	t.Run("missing directory returns 000001", func(t *testing.T) {
		seq, err := scaffold.NextMigrationSeq(filepath.Join(t.TempDir(), "does-not-exist"))
		if err != nil {
			t.Fatalf("NextMigrationSeq() error = %v", err)
		}

		if seq != "000001" {
			t.Fatalf("seq = %q, want %q", seq, "000001")
		}
	})

	t.Run("empty directory returns 000001", func(t *testing.T) {
		seq, err := scaffold.NextMigrationSeq(t.TempDir())
		if err != nil {
			t.Fatalf("NextMigrationSeq() error = %v", err)
		}

		if seq != "000001" {
			t.Fatalf("seq = %q, want %q", seq, "000001")
		}
	})

	t.Run("continues from the highest existing sequence", func(t *testing.T) {
		dir := t.TempDir()
		for _, name := range []string{"000001_users.up.sql", "000001_users.down.sql", "000003_products.up.sql", "not_a_migration.txt"} {
			if err := os.WriteFile(filepath.Join(dir, name), nil, 0o644); err != nil {
				t.Fatalf("test setup: %v", err)
			}
		}

		seq, err := scaffold.NextMigrationSeq(dir)
		if err != nil {
			t.Fatalf("NextMigrationSeq() error = %v", err)
		}

		if seq != "000004" {
			t.Fatalf("seq = %q, want %q (one past the highest existing, 000003)", seq, "000004")
		}
	})
}

func TestGenerate(t *testing.T) {
	dir := t.TempDir()
	fxFile := filepath.Join(dir, "fx.go")
	fxContent := `package app

import (
	"go.uber.org/fx"

	"jungo/internal/config"
	"jungo/internal/router"
)

func GetFxOptions() []fx.Option {
	return []fx.Option{
		// =====================================================================
		// FEATURES
		// =====================================================================

		fx.Provide(router.Module),
	}
}
`
	if err := os.WriteFile(fxFile, []byte(fxContent), 0o644); err != nil {
		t.Fatalf("test setup: %v", err)
	}

	data := scaffold.NewFeatureData("product", "jungo", "")
	cfg := scaffold.GenerateOptions{
		Data: data,
		Files: []scaffold.File{
			{Path: filepath.Join(dir, "domain", "product.go"), Template: "package domain\n\ntype {{.Pascal}} struct{}\n"},
			{Path: filepath.Join(dir, "queries", "product.sql"), Template: "SELECT * FROM {{.Table}};\n"},
		},
		FxFile:     fxFile,
		ImportPath: "jungo/internal/features/product",
		Marker:     "// FEATURES",
		ModuleLine: "product.Module,",
	}

	if err := scaffold.Generate(cfg); err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	domainContent, err := os.ReadFile(filepath.Join(dir, "domain", "product.go"))
	if err != nil {
		t.Fatalf("domain file not created: %v", err)
	}

	if !strings.Contains(string(domainContent), "type Product struct{}") {
		t.Fatalf("domain file content = %q, want the rendered template", domainContent)
	}

	sqlContent, err := os.ReadFile(filepath.Join(dir, "queries", "product.sql"))
	if err != nil {
		t.Fatalf("sql file not created: %v", err)
	}

	if !strings.Contains(string(sqlContent), "products") {
		t.Fatalf("sql file content = %q, want the rendered table name", sqlContent)
	}

	fxOut, err := os.ReadFile(fxFile)
	if err != nil {
		t.Fatalf("failed to read fx.go: %v", err)
	}

	fxStr := string(fxOut)
	if !strings.Contains(fxStr, `"jungo/internal/features/product"`) {
		t.Fatalf("fx.go = %q, want the new import added", fxStr)
	}

	if !strings.Contains(fxStr, "product.Module,") {
		t.Fatalf("fx.go = %q, want the new module line added", fxStr)
	}
}

func TestGenerate_RefusesToOverwriteExistingFile(t *testing.T) {
	dir := t.TempDir()
	existing := filepath.Join(dir, "already-there.go")
	if err := os.WriteFile(existing, []byte("package x\n"), 0o644); err != nil {
		t.Fatalf("test setup: %v", err)
	}

	cfg := scaffold.GenerateOptions{
		Data:  scaffold.NewFeatureData("product", "jungo", ""),
		Files: []scaffold.File{{Path: existing, Template: "package x\n// overwritten\n"}},
	}

	if err := scaffold.Generate(cfg); err == nil {
		t.Fatal("Generate() error = nil, want it to refuse overwriting an existing file")
	}

	content, _ := os.ReadFile(existing)
	if string(content) != "package x\n" {
		t.Fatalf("existing file was modified despite the refusal: %q", content)
	}
}

func TestGenerate_RenderErrorWritesNothing(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "a", "first.go")

	cfg := scaffold.GenerateOptions{
		Data: scaffold.NewFeatureData("product", "jungo", ""),
		Files: []scaffold.File{
			{Path: first, Template: "package a\n"},
			{Path: filepath.Join(dir, "b", "second.go"), Template: "{{.NoSuchField}}"},
		},
	}

	if err := scaffold.Generate(cfg); err == nil {
		t.Fatal("Generate() error = nil, want the render error")
	}

	if _, err := os.Stat(first); !os.IsNotExist(err) {
		t.Fatalf("%s exists after a failed Generate, want nothing written (stat err = %v)", first, err)
	}

	// A rerun with a fixed template must not be blocked by leftovers.
	cfg.Files[1].Template = "package b\n"
	cfg.FxFile = filepath.Join(dir, "missing-fx.go")
	if err := scaffold.Generate(cfg); err != nil && strings.Contains(err.Error(), "refusing to overwrite") {
		t.Fatalf("rerun refused to overwrite: %v", err)
	}
}

func TestRemove(t *testing.T) {
	dir := t.TempDir()
	fxFile := filepath.Join(dir, "fx.go")
	fxContent := "package app\n\nimport (\n\t\"jungo/internal/features/product\"\n)\n\nvar _ = []any{\n\tproduct.Module,\n}\n"
	if err := os.WriteFile(fxFile, []byte(fxContent), 0o644); err != nil {
		t.Fatalf("test setup: %v", err)
	}

	featureDir := filepath.Join(dir, "features", "product")
	if err := os.MkdirAll(featureDir, 0o755); err != nil {
		t.Fatalf("test setup: %v", err)
	}

	if err := os.WriteFile(filepath.Join(featureDir, "module.go"), []byte("package product\n"), 0o644); err != nil {
		t.Fatalf("test setup: %v", err)
	}

	extraFile := filepath.Join(dir, "product.sql")
	if err := os.WriteFile(extraFile, []byte("SELECT 1;\n"), 0o644); err != nil {
		t.Fatalf("test setup: %v", err)
	}

	cfg := scaffold.RemoveOptions{
		Dirs:       []string{featureDir},
		ExtraFiles: []string{extraFile},
		FxFile:     fxFile,
		ImportPath: "jungo/internal/features/product",
		ModuleLine: "product.Module,",
	}

	if err := scaffold.Remove(cfg); err != nil {
		t.Fatalf("Remove() error = %v", err)
	}

	if _, err := os.Stat(featureDir); !os.IsNotExist(err) {
		t.Error("feature directory should have been removed")
	}

	if _, err := os.Stat(extraFile); !os.IsNotExist(err) {
		t.Error("extra file should have been removed")
	}

	fxOut, _ := os.ReadFile(fxFile)
	if strings.Contains(string(fxOut), "product") {
		t.Fatalf("fx.go still references product after removal: %q", fxOut)
	}
}

func TestRemove_MissingExtraFileIsNotAnError(t *testing.T) {
	dir := t.TempDir()
	fxFile := filepath.Join(dir, "fx.go")
	if err := os.WriteFile(fxFile, []byte("package app\n"), 0o644); err != nil {
		t.Fatalf("test setup: %v", err)
	}

	cfg := scaffold.RemoveOptions{
		ExtraFiles: []string{filepath.Join(dir, "does-not-exist.sql")},
		FxFile:     fxFile,
		ImportPath: "jungo/internal/features/product",
		ModuleLine: "product.Module,",
	}

	if err := scaffold.Remove(cfg); err != nil {
		t.Fatalf("Remove() error = %v, want nil when an ExtraFile is already absent", err)
	}
}
