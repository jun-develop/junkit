package tracer

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"html/template"
	"sort"
)

// dashboardHTML is the html/template source of the debug dashboard.
//
//go:embed dashboard.html
var dashboardHTML string

// dashboardTmpl is the parsed dashboard template.
var dashboardTmpl = template.Must(template.New("dashboard").Parse(dashboardHTML))

// Report is the rendered view of a request's debug entries.
type Report struct {
	TotalMS float64    `json:"total_ms"`
	DB      DBSummary  `json:"db"`
	Entries []LogEntry `json:"entries"`
}

// DBSummary aggregates the traced SQL queries of a request.
type DBSummary struct {
	Queries int     `json:"queries"`
	Failed  int     `json:"failed"`
	TotalMS float64 `json:"total_ms"`

	// Percent is the share of the request's total time spent in queries.
	Percent float64 `json:"percent"`

	// Repeated lists queries run more than once, a possible N+1.
	Repeated []RepeatedQuery `json:"repeated,omitempty"`
}

// RepeatedQuery counts executions of one query (same SQL, any arguments).
type RepeatedQuery struct {
	Name string `json:"name"`

	// SQL is the query before binding args.
	SQL     string  `json:"sql,omitempty"`
	Count   int     `json:"count"`
	TotalMS float64 `json:"total_ms"`
}

// NewReport builds the Report for the Debugger attached to ctx, or nil if none is.
func NewReport(ctx context.Context) *Report {
	d := FromContext(ctx)
	if d == nil {
		return nil
	}

	r := &Report{TotalMS: d.sinceStartMS(), Entries: d.Logs()}
	r.DB = summarizeDB(r.Entries, r.TotalMS)

	return r
}

// summarizeDB totals SQL entries and lists queries executed more than once.
// Queries are grouped by their SQL before binding args, not by name, since
// every unannotated query shares a guessed name like "Select".
func summarizeDB(entries []LogEntry, totalMS float64) DBSummary {
	var s DBSummary
	groups := map[string]*RepeatedQuery{}

	for _, e := range entries {
		if e.Type != TypeSQL {
			continue
		}

		s.Queries++
		s.TotalMS += e.DurationMS

		data, _ := e.Data.(SQLData)
		if data.Error != "" {
			s.Failed++
		}

		// Entries appended by hand carry no query, so they group by label.
		key := data.query
		if key == "" {
			key = e.Label
		}

		q := groups[key]
		if q == nil {
			q = &RepeatedQuery{Name: e.Label, SQL: data.query}
			groups[key] = q
		}

		q.Count++
		q.TotalMS += e.DurationMS
	}

	if totalMS > 0 {
		s.Percent = s.TotalMS / totalMS * 100
	}

	for _, q := range groups {
		if q.Count > 1 {
			s.Repeated = append(s.Repeated, *q)
		}
	}

	sort.Slice(s.Repeated, func(i, j int) bool {
		if s.Repeated[i].Count != s.Repeated[j].Count {
			return s.Repeated[i].Count > s.Repeated[j].Count
		}

		return s.Repeated[i].Name < s.Repeated[j].Name
	})

	return s
}

// RenderJSON renders the debug report from ctx as indented JSON, for CLI clients.
//
// Usage:
//
//	c.Data(http.StatusOK, "application/json", tracer.RenderJSON(ctx))
func RenderJSON(ctx context.Context) []byte {
	r := NewReport(ctx)
	if r == nil {
		return []byte(`{"error":"tracer: no debugger attached to the request context"}`)
	}

	b, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return fmt.Appendf(nil, `{"error":%q}`, err.Error())
	}

	return b
}

// RenderHTML renders the debug report from ctx as an interactive HTML dashboard.
//
// Usage:
//
//	c.String(http.StatusOK, tracer.RenderHTML(ctx))
func RenderHTML(ctx context.Context) string {
	r := NewReport(ctx)
	if r == nil {
		return "<p>tracer: no debugger attached to the request context. Is middleware.TracerDebug active?</p>"
	}

	var buf bytes.Buffer
	if err := dashboardTmpl.Execute(&buf, newDashboardView(r)); err != nil {
		return "<pre>tracer: render dashboard: " + template.HTMLEscapeString(err.Error()) + "</pre>"
	}

	return buf.String()
}

// dashboardView is the template data for dashboard.html.
type dashboardView struct {
	*Report
	Timeline []timelineBar
	Items    []dashboardItem
}

// timelineBar positions one span or query on the waterfall, as percentages of the request.
type timelineBar struct {
	Type, Label       string
	AtMS, DurationMS  float64
	LeftPct, WidthPct float64
}

// dashboardItem is one entry prepared for display.
type dashboardItem struct {
	LogEntry
	Index int

	// Body is the pretty-printed Data (or SQL result).
	Body string

	// SQL is the bound SQL of a SQL entry.
	SQL string

	// SQLError is set for a failed SQL query, for its Error, Location and Function.
	SQLError *SQLData

	// Issue is set for warnings and errors.
	Issue *IssueData
}

// newDashboardView prepares r for the HTML template.
func newDashboardView(r *Report) dashboardView {
	v := dashboardView{Report: r}

	for i, e := range r.Entries {
		item := dashboardItem{LogEntry: e, Index: i + 1}
		switch data := e.Data.(type) {
		case IssueData:
			item.Issue = &data
		case SQLData:
			// SQL and error get their own blocks; the body shows the rest.
			item.SQL = data.SQL
			if data.Error != "" {
				failed := data
				item.SQLError = &failed
			}

			data.SQL, data.Error, data.Location, data.Function = "", "", "", ""
			if body := PrettyPrint(data); body != "{}" {
				item.Body = body
			}
		default:
			if e.Data != nil {
				item.Body = PrettyPrint(e.Data)
			}
		}

		v.Items = append(v.Items, item)

		if (e.Type == TypeSpan || e.Type == TypeSQL) && r.TotalMS > 0 {
			v.Timeline = append(v.Timeline, timelineBar{
				Type: e.Type, Label: e.Label, AtMS: e.AtMS, DurationMS: e.DurationMS,
				LeftPct:  e.AtMS / r.TotalMS * 100,
				WidthPct: e.DurationMS / r.TotalMS * 100,
			})
		}
	}

	sort.SliceStable(v.Timeline, func(i, j int) bool { return v.Timeline[i].AtMS < v.Timeline[j].AtMS })

	return v
}

// PrettyPrint formats v as indented JSON, falling back to %+v for values JSON can't encode.
//
// Usage:
//
//	s := tracer.PrettyPrint(myStruct)
func PrettyPrint(v any) string {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Sprintf("%+v", v)
	}

	return string(b)
}
