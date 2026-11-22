package tracer_test

// =============================================================================
// TEST CASE OVERVIEW
// =============================================================================
//
// File: tracer_test.go
// Purpose: Verify the request Debugger, its context lookup, and the Comment/Var/Warn/Error/Stop helpers.
// Type: Unit
//
// Test functions: 9
// Total scenarios: 11
//
// #   Test Case                                     Scenario                                             Expected Result
// 1   TestDebugger_NilReceiverIsSafe                Append and Logs on a nil Debugger                    No panic; Logs returns nil
// 2   TestDebugger_AppendKeepsOrder                 Two entries appended                                 Logs returns them in append order
// 3   TestDebugger_LogsReturnsASnapshot             Caller mutates the returned slice                    Internal entries unchanged
// 4   TestFromContext_ReturnsAttachedDebugger       Plain context                                        FromContext returns nil; Enabled is false
// 5   TestFromContext_ReturnsAttachedDebugger       Context with a Debugger                              FromContext returns it; Enabled is true
// 6   TestFromContext_GinContextUsesRequestContext  gin.Context whose request carries a Debugger         Returns that Debugger
// 7   TestFromContext_GinContextUsesRequestContext  gin.Context without a request                        Returns nil
// 8   TestLogHelpers_NoOpWithoutDebugger            Every helper called on a plain context               No panic
// 9   TestCommentAndVar_RecordEntries               One comment, Var with two values, Var with an error  4 entries: the comment, one per value, the error message
// 10  TestWarnAndError_RecordIssues                 Warn with an empty label, then Error                 Warn uses "WARNING" and has no stack; Error has a stack
// 11  TestStop_PanicsWithBreakpointAfterRecording   Stop with a Debugger attached                        Records the value, then panics with BreakpointSignal
//
// =============================================================================

import (
	"context"
	"errors"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/jun-develop/junkit/tracer"
)

// newCtx returns a context carrying a fresh Debugger. Shared by every tracer_test file.
func newCtx() (context.Context, *tracer.Debugger) {
	d := tracer.New()
	return tracer.WithContext(context.Background(), d), d
}

func TestDebugger_NilReceiverIsSafe(t *testing.T) {
	var d *tracer.Debugger

	d.Append(tracer.LogEntry{Type: tracer.TypeComment})

	if logs := d.Logs(); logs != nil {
		t.Fatalf("Logs() on nil Debugger = %v, want nil", logs)
	}
}

func TestDebugger_AppendKeepsOrder(t *testing.T) {
	d := tracer.New()
	d.Append(tracer.LogEntry{Type: tracer.TypeComment, Label: "first"})
	d.Append(tracer.LogEntry{Type: tracer.TypeWarning, Label: "second"})

	logs := d.Logs()

	if len(logs) != 2 {
		t.Fatalf("Logs() = %+v, want 2 entries", logs)
	}

	if logs[0].Label != "first" || logs[1].Label != "second" {
		t.Errorf("Logs() labels = [%s %s], want [first second]", logs[0].Label, logs[1].Label)
	}
}

func TestDebugger_LogsReturnsASnapshot(t *testing.T) {
	d := tracer.New()
	d.Append(tracer.LogEntry{Type: tracer.TypeComment, Label: "original"})

	logs := d.Logs()
	logs[0].Label = "mutated by caller"

	if got := d.Logs()[0].Label; got != "original" {
		t.Fatalf("Logs()[0].Label after an external mutation = %q, want %q", got, "original")
	}
}

func TestFromContext_ReturnsAttachedDebugger(t *testing.T) {
	t.Run("plain context", func(t *testing.T) {
		if got := tracer.FromContext(context.Background()); got != nil {
			t.Errorf("FromContext() = %v, want nil", got)
		}

		if tracer.Enabled(context.Background()) {
			t.Error("Enabled() = true, want false")
		}
	})

	t.Run("context with a debugger", func(t *testing.T) {
		ctx, d := newCtx()

		if got := tracer.FromContext(ctx); got != d {
			t.Errorf("FromContext() = %p, want the Debugger passed to WithContext (%p)", got, d)
		}

		if !tracer.Enabled(ctx) {
			t.Error("Enabled() = false, want true")
		}
	})
}

func TestFromContext_GinContextUsesRequestContext(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("gin context whose request carries a debugger", func(t *testing.T) {
		d := tracer.New()
		req := httptest.NewRequest("GET", "/", nil)
		req = req.WithContext(tracer.WithContext(req.Context(), d))
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = req

		if got := tracer.FromContext(c); got != d {
			t.Errorf("FromContext(*gin.Context) = %p, want the request's Debugger %p", got, d)
		}
	})

	t.Run("gin context without a request", func(t *testing.T) {
		if got := tracer.FromContext(&gin.Context{}); got != nil {
			t.Errorf("FromContext(*gin.Context) = %v, want nil", got)
		}
	})
}

func TestLogHelpers_NoOpWithoutDebugger(t *testing.T) {
	ctx := context.Background()

	// Passes if none of these panics.
	tracer.Comment(ctx, "note")
	tracer.Var(ctx, "value", 1)
	tracer.Warn(ctx, "warn", "boom")
	tracer.Error(ctx, "err", "boom")
	tracer.Stop(ctx, "stop", 1)
}

func TestCommentAndVar_RecordEntries(t *testing.T) {
	ctx, d := newCtx()

	tracer.Comment(ctx, "starting")
	tracer.Var(ctx, "pair", 1, "two")
	tracer.Var(ctx, "failure", errors.New("boom"))

	logs := d.Logs()
	if len(logs) != 4 {
		t.Fatalf("got %d entries, want 4 (1 comment + 2 values + 1 error value)", len(logs))
	}

	if logs[0].Type != tracer.TypeComment || logs[0].Label != "starting" {
		t.Errorf("logs[0] = %+v, want the comment %q", logs[0], "starting")
	}

	if logs[1].Data != 1 {
		t.Errorf("logs[1].Data = %v, want 1 (one entry per value)", logs[1].Data)
	}

	if logs[2].Data != "two" {
		t.Errorf("logs[2].Data = %v, want %q (one entry per value)", logs[2].Data, "two")
	}

	if logs[3].Data != "boom" {
		t.Errorf("Var(error) recorded %#v, want the error message %q", logs[3].Data, "boom")
	}
}

func TestWarnAndError_RecordIssues(t *testing.T) {
	ctx, d := newCtx()

	tracer.Warn(ctx, "", "soft")
	tracer.Error(ctx, "db failed", errors.New("hard"))

	logs := d.Logs()
	if len(logs) != 2 {
		t.Fatalf("got %d entries, want 2", len(logs))
	}

	warn := logs[0].Data.(tracer.IssueData)
	if logs[0].Label != "WARNING" {
		t.Errorf("warning label = %q, want the default %q", logs[0].Label, "WARNING")
	}

	if warn.Error != "soft" {
		t.Errorf("warning message = %q, want %q", warn.Error, "soft")
	}

	if warn.Stack != "" {
		t.Errorf("warning stack = %q, want empty", warn.Stack)
	}

	issue := logs[1].Data.(tracer.IssueData)
	if logs[1].Type != tracer.TypeError {
		t.Errorf("logs[1].Type = %v, want TypeError", logs[1].Type)
	}

	if issue.Error != "hard" {
		t.Errorf("error message = %q, want %q", issue.Error, "hard")
	}

	if issue.Stack == "" {
		t.Error("error stack is empty, want a stack trace")
	}
}

func TestStop_PanicsWithBreakpointAfterRecording(t *testing.T) {
	ctx, d := newCtx()

	defer func() {
		if _, ok := recover().(tracer.BreakpointSignal); !ok {
			t.Fatal("Stop() did not panic with BreakpointSignal, want it to when a Debugger is attached")
		}

		logs := d.Logs()
		if len(logs) != 1 {
			t.Fatalf("Logs() = %+v, want 1 entry recorded before stopping", logs)
		}

		if logs[0].Label != "state" {
			t.Errorf("Logs()[0].Label = %q, want %q", logs[0].Label, "state")
		}
	}()

	tracer.Stop(ctx, "state", 42)
}
