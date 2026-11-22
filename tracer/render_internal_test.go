package tracer

// =============================================================================
// TEST CASE OVERVIEW
// =============================================================================
//
// File: render_internal_test.go
// Purpose: Verify the internal DB summary and how the HTML dashboard shows a failed query.
// Type: Unit
//
// Test functions: 2
// Total scenarios: 2
//
// #  Test Case                             Scenario                                               Expected Result
// 1  TestSummarizeDB_GroupsBySQLNotName    Two distinct "Select" queries and one query run twice  Only the query run twice is repeated; 4 queries, 1 failed
// 2  TestRenderHTML_HighlightsFailedQuery  One failed INSERT                                      Error block with message and origin; no location in JSON, no {} body, no JavaScript
//
// =============================================================================

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

	r := s.Repeated[0]
	if r.Name != "GetItem" {
		t.Errorf("Repeated[0].Name = %q, want %q", r.Name, "GetItem")
	}

	if r.Count != 2 {
		t.Errorf("Repeated[0].Count = %d, want 2", r.Count)
	}

	if r.SQL != "SELECT * FROM items WHERE id = $1" {
		t.Errorf("Repeated[0].SQL = %q, want the unbound SQL", r.SQL)
	}

	if s.Queries != 4 {
		t.Errorf("Queries = %d, want 4", s.Queries)
	}

	if s.Failed != 1 {
		t.Errorf("Failed = %d, want 1", s.Failed)
	}
}

func TestRenderHTML_HighlightsFailedQuery(t *testing.T) {
	d := New()
	d.Append(sqlEntry("CreateUser", "INSERT INTO users VALUES ($1)", "INSERT INTO users VALUES ('jane')", "duplicate key"))
	ctx := WithContext(t.Context(), d)

	page := RenderHTML(ctx)

	if strings.Contains(page, `"location"`) {
		t.Error("page has \"location\" in the JSON body, want it only in the error block")
	}

	if strings.Contains(page, "<pre>{}</pre>") {
		t.Error("page renders an empty {} body, want none for a failed query with nothing else to show")
	}

	for _, want := range []string{`class="card sql failed"`, `class="sql-error">✘ duplicate key`, `<b>1</b><span class="muted">FAILED`,
		`📍 /app/repo/user_repository.go:26 · repo.(*UserRepository).Create`} {
		if !strings.Contains(page, want) {
			t.Errorf("dashboard is missing %q", want)
		}
	}

	if strings.Contains(page, "<script") {
		t.Error("dashboard contains <script, want no JavaScript (Postman's preview does not run it)")
	}
}
