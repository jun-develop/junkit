package tracer_test

// =============================================================================
// TEST CASE OVERVIEW
// =============================================================================
//
// File: render_test.go
// Purpose: Verify the debug report (NewReport), its HTML and JSON renderings, and PrettyPrint.
// Type: Unit
//
// Test functions: 5
// Total scenarios: 9
//
// #  Test Case                                  Scenario                                    Expected Result
// 1  TestNewReport_SummarizesDB                 No Debugger                                 Returns nil
// 2  TestNewReport_SummarizesDB                 Sample entries (GetUser x3, ListOrders x1)  4 queries, 5ms total, only GetUser x3 reported as repeated
// 3  TestRenderHTML_ShowsEveryEntryKind         No Debugger                                 Page explains that no debugger is attached
// 4  TestRenderHTML_ShowsEveryEntryKind         Sample entries                              Page shows every entry kind, escapes labels, has no ZgotmplZ
// 5  TestRenderJSON_EncodesEveryEntry           Sample entries                              Valid JSON report with 9 entries and 4 queries
// 6  TestRenderJSON_ReportsProblemsAsJSONError  No Debugger                                 JSON object whose "error" says no debugger is attached
// 7  TestRenderJSON_ReportsProblemsAsJSONError  Var holding a channel (not JSON-encodable)  JSON object whose "error" names the unsupported type
// 8  TestPrettyPrint_IndentsJSONOrFallsBack     Struct with JSON tags                       Indented JSON honouring the tags
// 9  TestPrettyPrint_IndentsJSONOrFallsBack     Channel (not JSON-encodable)                Falls back to %+v (a 0x... pointer)
//
// =============================================================================

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/jun-develop/junkit/tracer"
)

// sampleCtx returns a context whose Debugger holds one entry of every type,
// plus GetUser run 3 times (1ms each) and ListOrders once (2ms): 9 entries, 4 queries, 5ms.
func sampleCtx() context.Context {
	ctx, d := newCtx()
	tracer.Comment(ctx, "<script>alert(1)</script>")
	tracer.Var(ctx, "user", map[string]any{"id": 1})
	tracer.Warn(ctx, "soft", "careful")
	tracer.Error(ctx, "hard", errors.New("broken"))
	tracer.Span(ctx, "compute")()
	for range 3 {
		d.Append(tracer.LogEntry{Type: tracer.TypeSQL, Label: "GetUser", DurationMS: 1, Data: tracer.SQLData{SQL: "SELECT 1"}})
	}

	d.Append(tracer.LogEntry{Type: tracer.TypeSQL, Label: "ListOrders", DurationMS: 2, Data: tracer.SQLData{SQL: "SELECT 2"}})

	return ctx
}

func TestNewReport_SummarizesDB(t *testing.T) {
	t.Run("no debugger", func(t *testing.T) {
		if r := tracer.NewReport(context.Background()); r != nil {
			t.Errorf("NewReport() = %+v, want nil", r)
		}
	})

	t.Run("sample entries", func(t *testing.T) {
		r := tracer.NewReport(sampleCtx())

		if r.DB.Queries != 4 {
			t.Errorf("DB.Queries = %d, want 4", r.DB.Queries)
		}

		if r.DB.TotalMS != 5 {
			t.Errorf("DB.TotalMS = %v, want 5", r.DB.TotalMS)
		}

		want := tracer.RepeatedQuery{Name: "GetUser", Count: 3, TotalMS: 3}
		if len(r.DB.Repeated) != 1 || r.DB.Repeated[0] != want {
			t.Errorf("DB.Repeated = %+v, want only %+v", r.DB.Repeated, want)
		}
	})
}

func TestRenderHTML_ShowsEveryEntryKind(t *testing.T) {
	t.Run("no debugger", func(t *testing.T) {
		if page := tracer.RenderHTML(context.Background()); !strings.Contains(page, "no debugger attached") {
			t.Errorf("RenderHTML() = %q, want it to explain that no debugger is attached", page)
		}
	})

	t.Run("sample entries", func(t *testing.T) {
		page := tracer.RenderHTML(sampleCtx())

		for _, want := range []string{"<!DOCTYPE html>", "careful", "broken", "Stack Trace", "compute", "SELECT 2", "GetUser</strong> ran 3 times"} {
			if !strings.Contains(page, want) {
				t.Errorf("dashboard is missing %q", want)
			}
		}

		if strings.Contains(page, "<script>alert(1)</script>") {
			t.Error("dashboard contains an unescaped entry label, want labels HTML-escaped")
		}

		if strings.Contains(page, "ZgotmplZ") {
			t.Error("dashboard contains ZgotmplZ: html/template rejected a value (check the timeline style attribute)")
		}
	})
}

func TestRenderJSON_EncodesEveryEntry(t *testing.T) {
	var r tracer.Report

	if err := json.Unmarshal(tracer.RenderJSON(sampleCtx()), &r); err != nil {
		t.Fatalf("RenderJSON() output is not valid JSON: %v", err)
	}

	if len(r.Entries) != 9 {
		t.Errorf("decoded report has %d entries, want 9", len(r.Entries))
	}

	if r.DB.Queries != 4 {
		t.Errorf("decoded report has %d queries, want 4", r.DB.Queries)
	}
}

func TestRenderJSON_ReportsProblemsAsJSONError(t *testing.T) {
	channelCtx, _ := newCtx()
	tracer.Var(channelCtx, "channel", make(chan int))

	tests := []struct {
		name      string
		ctx       context.Context
		wantError string
	}{
		{"no debugger", context.Background(), "no debugger attached"},
		{"var holding a channel", channelCtx, "unsupported type"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got struct {
				Error string `json:"error"`
			}

			if err := json.Unmarshal(tracer.RenderJSON(tt.ctx), &got); err != nil {
				t.Fatalf("RenderJSON() output is not valid JSON: %v", err)
			}

			if !strings.Contains(got.Error, tt.wantError) {
				t.Errorf("error = %q, want it to contain %q", got.Error, tt.wantError)
			}
		})
	}
}

func TestPrettyPrint_IndentsJSONOrFallsBack(t *testing.T) {
	t.Run("struct with json tags", func(t *testing.T) {
		type row struct {
			ID   int    `json:"id"`
			Name string `json:"name,omitempty"`
		}

		if got := tracer.PrettyPrint(row{ID: 1}); got != "{\n  \"id\": 1\n}" {
			t.Errorf("PrettyPrint(struct) = %q, want indented JSON honouring tags", got)
		}
	})

	t.Run("channel is not json-encodable", func(t *testing.T) {
		if got := tracer.PrettyPrint(make(chan int)); !strings.HasPrefix(got, "0x") {
			t.Errorf("PrettyPrint(chan) = %q, want the %%+v fallback", got)
		}
	})
}
