package tracer

import (
	"context"
	"fmt"
	"runtime/debug"
	"time"
)

// IssueData is the payload of a warning or error entry.
type IssueData struct {
	Error     string `json:"error"`
	Location  string `json:"location"`
	Line      int    `json:"line"`
	Function  string `json:"function"`
	Stack     string `json:"stack,omitempty"`
	Timestamp string `json:"timestamp"`
}

// Comment records a plain text note.
//
// Usage:
//
//	tracer.Comment(ctx, "starting user synchronization")
func Comment(ctx context.Context, message string) {
	FromContext(ctx).record(TypeComment, message, nil)
}

// Var records one or more values under a shared label.
//
// Usage:
//
//	tracer.Var(ctx, "user profile", user)
func Var(ctx context.Context, label string, values ...any) {
	d := FromContext(ctx)
	if d == nil {
		return
	}

	for _, v := range values {
		// Error values usually marshal to an empty JSON object, so record their message.
		if err, ok := v.(error); ok {
			v = err.Error()
		}

		d.record(TypeVariable, label, v)
	}
}

// Warn records a non-fatal problem with its source location.
//
// Usage:
//
//	tracer.Warn(ctx, "user not found", err)
func Warn(ctx context.Context, label string, err any) {
	if d := FromContext(ctx); d != nil {
		d.record(TypeWarning, labelOr(label, "WARNING"), newIssue(err, false))
	}
}

// Error records an error with its source location and full stack trace.
//
// Usage:
//
//	if err != nil {
//	    tracer.Error(ctx, "database query failed", err)
//	}
func Error(ctx context.Context, label string, err any) {
	if d := FromContext(ctx); d != nil {
		d.record(TypeError, labelOr(label, "ERROR"), newIssue(err, true))
	}
}

// Stop records values, then halts the request so the dashboard shows the state at this point.
//
// It panics with BreakpointSignal, which middleware.TracerDebug recovers. Only call it from the
// request's own goroutine: a panic in any other goroutine is not recovered and crashes the process.
//
// Usage:
//
//	tracer.Stop(ctx, "user before save", user)
func Stop(ctx context.Context, label string, values ...any) {
	if !Enabled(ctx) {
		return
	}

	Var(ctx, label, values...)
	panic(BreakpointSignal{})
}

// labelOr returns label, or fallback when label is empty.
func labelOr(label, fallback string) string {
	if label == "" {
		return fallback
	}

	return label
}

// newIssue builds the Warn/Error payload, locating the first caller outside this package.
func newIssue(err any, withStack bool) IssueData {
	file, line, fn := FindErrorOrigin()
	issue := IssueData{
		Error:     fmt.Sprintf("%v", err),
		Location:  file,
		Line:      line,
		Function:  fn,
		Timestamp: time.Now().Format("2006-01-02 15:04:05.000 -0700"),
	}

	if withStack {
		issue.Stack = string(debug.Stack())
	}

	return issue
}
