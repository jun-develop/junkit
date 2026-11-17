package tracer_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/jun-develop/junkit/tracer"
)

// sampleCtx returns a context whose Debugger holds one entry of every type.
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

// TestNewReport_SummarizesDB checks NewReport totals the SQL entries of the request.
func TestNewReport_SummarizesDB(t *testing.T) {
	if tracer.NewReport(context.Background()) != nil {
		t.Fatal("NewReport without a Debugger should be nil")
	}

	r := tracer.NewReport(sampleCtx())
	if r.DB.Queries != 4 || r.DB.TotalMS != 5 {
		t.Fatalf("DB = %+v, want 4 queries totalling 5ms", r.DB)
	}

	if len(r.DB.Repeated) != 1 || r.DB.Repeated[0] != (tracer.RepeatedQuery{Name: "GetUser", Count: 3, TotalMS: 3}) {
		t.Fatalf("Repeated = %+v, want only GetUser x3", r.DB.Repeated)
	}
}

// TestRenderHTML checks the dashboard shows every entry kind and explains a missing Debugger.
func TestRenderHTML(t *testing.T) {
	if !strings.Contains(tracer.RenderHTML(context.Background()), "no debugger attached") {
		t.Fatal("RenderHTML without a Debugger should explain that none is attached")
	}

	page := tracer.RenderHTML(sampleCtx())

	for _, want := range []string{"<!DOCTYPE html>", "careful", "broken", "Stack Trace", "compute", "SELECT 2", "GetUser</strong> ran 3 times"} {
		if !strings.Contains(page, want) {
			t.Errorf("dashboard is missing %q", want)
		}
	}

	if strings.Contains(page, "<script>alert(1)</script>") {
		t.Error("entry labels must be HTML-escaped")
	}

	if strings.Contains(page, "ZgotmplZ") {
		t.Error("html/template rejected a value (ZgotmplZ) — check the timeline style attribute")
	}
}

// TestRenderJSON checks RenderJSON decodes into a Report holding every entry.
func TestRenderJSON(t *testing.T) {
	var r tracer.Report
	if err := json.Unmarshal(tracer.RenderJSON(sampleCtx()), &r); err != nil {
		t.Fatalf("RenderJSON output is not valid JSON: %v", err)
	}

	if len(r.Entries) != 9 || r.DB.Queries != 4 {
		t.Fatalf("decoded report has %d entries and %d queries, want 9 and 4", len(r.Entries), r.DB.Queries)
	}
}

// TestPrettyPrint checks PrettyPrint indents JSON and falls back to %+v.
func TestPrettyPrint(t *testing.T) {
	type row struct {
		ID   int    `json:"id"`
		Name string `json:"name,omitempty"`
	}
	if got := tracer.PrettyPrint(row{ID: 1}); got != "{\n  \"id\": 1\n}" {
		t.Errorf("PrettyPrint(struct) = %q, want indented JSON honouring tags", got)
	}

	if got := tracer.PrettyPrint(make(chan int)); !strings.HasPrefix(got, "0x") {
		t.Errorf("PrettyPrint(chan) = %q, want the %%+v fallback", got)
	}
}
