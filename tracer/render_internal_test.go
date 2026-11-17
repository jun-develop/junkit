package tracer

import (
	"strings"
	"testing"
)

// sqlEntry builds a traced SQL entry the way PgxWrapper records it.
func sqlEntry(name, query, bound, errMsg string) LogEntry {
	data := SQLData{SQL: bound, query: query, Error: errMsg}
	if errMsg != "" {
		data.Location, data.Function = "/app/repo/user_repository.go:26", "repo.(*UserRepository).Create"
	}

	return LogEntry{Type: TypeSQL, Label: name, DurationMS: 1, Data: data}
}

// TestSummarizeDB_GroupsBySQLNotName checks repeated queries are grouped by SQL rather than label.
func TestSummarizeDB_GroupsBySQLNotName(t *testing.T) {
	entries := []LogEntry{
		// Two different unannotated queries share the guessed name "Select".
		sqlEntry("Select", "SELECT * FROM users WHERE id = $1", "SELECT * FROM users WHERE id = 1", ""),
		sqlEntry("Select", "SELECT * FROM orders", "SELECT * FROM orders", ""),
		// The same query run twice with different args is a repeat.
		sqlEntry("GetItem", "SELECT * FROM items WHERE id = $1", "SELECT * FROM items WHERE id = 1", ""),
		sqlEntry("GetItem", "SELECT * FROM items WHERE id = $1", "SELECT * FROM items WHERE id = 2", "boom"),
	}

	s := summarizeDB(entries, 10)

	if len(s.Repeated) != 1 {
		t.Fatalf("Repeated = %+v, want only GetItem (distinct Select queries are not a repeat)", s.Repeated)
	}

	if r := s.Repeated[0]; r.Name != "GetItem" || r.Count != 2 || r.SQL != "SELECT * FROM items WHERE id = $1" {
		t.Fatalf("Repeated[0] = %+v, want GetItem x2 with its unbound SQL", r)
	}

	if s.Queries != 4 || s.Failed != 1 {
		t.Fatalf("Queries, Failed = %d, %d, want 4, 1", s.Queries, s.Failed)
	}
}

// TestRenderHTML_HighlightsFailedQuery checks the failed-query error block.
func TestRenderHTML_HighlightsFailedQuery(t *testing.T) {
	d := New()
	d.Append(sqlEntry("CreateUser", "INSERT INTO users VALUES ($1)", "INSERT INTO users VALUES ('jane')", "duplicate key"))
	ctx := WithContext(t.Context(), d)

	page := RenderHTML(ctx)

	if strings.Contains(page, `"location"`) {
		t.Error("location belongs in the error block, not repeated in the JSON body")
	}

	if strings.Contains(page, "<pre>{}</pre>") {
		t.Error("a failed query with nothing else to show should not render an empty {} body")
	}

	for _, want := range []string{`class="card sql failed"`, `class="sql-error">✘ duplicate key`, `<b>1</b><span class="muted">FAILED`,
		`📍 /app/repo/user_repository.go:26 · repo.(*UserRepository).Create`} {
		if !strings.Contains(page, want) {
			t.Errorf("dashboard is missing %q", want)
		}
	}

	if strings.Contains(page, "<script") {
		t.Error("dashboard should not ship JavaScript (Postman's preview doesn't run it)")
	}
}
