package tracer_test

import (
	"context"
	"errors"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/jun-develop/junkit/tracer"
)

// newCtx returns a context carrying a fresh Debugger.
func newCtx() (context.Context, *tracer.Debugger) {
	d := tracer.New()
	return tracer.WithContext(context.Background(), d), d
}

// TestDebugger_NilReceiverIsSafe checks a nil Debugger ignores Append and returns no logs.
func TestDebugger_NilReceiverIsSafe(t *testing.T) {
	var d *tracer.Debugger
	// Must not panic.
	d.Append(tracer.LogEntry{Type: tracer.TypeComment})
	if logs := d.Logs(); logs != nil {
		t.Fatalf("Logs() on nil Debugger = %v, want nil", logs)
	}
}

// TestDebugger_AppendKeepsOrder checks entries are returned in append order.
func TestDebugger_AppendKeepsOrder(t *testing.T) {
	d := tracer.New()
	d.Append(tracer.LogEntry{Type: tracer.TypeComment, Label: "first"})
	d.Append(tracer.LogEntry{Type: tracer.TypeWarning, Label: "second"})

	logs := d.Logs()
	if len(logs) != 2 || logs[0].Label != "first" || logs[1].Label != "second" {
		t.Fatalf("Logs() = %+v, want [first second] in append order", logs)
	}
}

// TestDebugger_LogsReturnsASnapshot checks Logs returns a copy unaffected by later appends.
func TestDebugger_LogsReturnsASnapshot(t *testing.T) {
	d := tracer.New()
	d.Append(tracer.LogEntry{Type: tracer.TypeComment, Label: "original"})

	logs := d.Logs()
	logs[0].Label = "mutated by caller"

	if got := d.Logs()[0].Label; got != "original" {
		t.Fatalf("Logs() leaked internal state: got %q after an external mutation, want %q", got, "original")
	}
}

// TestFromContext checks FromContext and Enabled with and without an attached Debugger.
func TestFromContext(t *testing.T) {
	if got := tracer.FromContext(context.Background()); got != nil {
		t.Fatalf("FromContext() on a plain context = %v, want nil", got)
	}

	if tracer.Enabled(context.Background()) {
		t.Fatal("Enabled() on a plain context should be false")
	}

	ctx, d := newCtx()
	if got := tracer.FromContext(ctx); got != d {
		t.Fatal("FromContext() did not return the Debugger passed to WithContext()")
	}

	if !tracer.Enabled(ctx) {
		t.Fatal("Enabled() should be true once a Debugger is attached")
	}
}

// TestFromContext_GinContextUsesRequestContext checks gin.Context lookup.
func TestFromContext_GinContextUsesRequestContext(t *testing.T) {
	gin.SetMode(gin.TestMode)
	d := tracer.New()
	req := httptest.NewRequest("GET", "/", nil)
	req = req.WithContext(tracer.WithContext(req.Context(), d))

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = req

	if got := tracer.FromContext(c); got != d {
		t.Fatal("FromContext(*gin.Context) did not find the Debugger on the request context")
	}

	if got := tracer.FromContext(&gin.Context{}); got != nil {
		t.Fatalf("FromContext on a gin.Context without a request = %v, want nil", got)
	}
}

// TestLogHelpers_NoOpWithoutDebugger checks every log helper is a no-op without a Debugger.
func TestLogHelpers_NoOpWithoutDebugger(t *testing.T) {
	ctx := context.Background()
	// None of these may panic or record anywhere.
	tracer.Comment(ctx, "note")
	tracer.Var(ctx, "value", 1)
	tracer.Warn(ctx, "warn", "boom")
	tracer.Error(ctx, "err", "boom")
	tracer.Stop(ctx, "stop", 1)
}

// TestComment_Var checks Comment records a note and Var one entry per value.
func TestComment_Var(t *testing.T) {
	ctx, d := newCtx()

	tracer.Comment(ctx, "starting")
	tracer.Var(ctx, "pair", 1, "two")
	tracer.Var(ctx, "failure", errors.New("boom"))

	logs := d.Logs()
	if len(logs) != 4 {
		t.Fatalf("got %d entries, want 4 (1 comment + 2 values + 1 error value)", len(logs))
	}

	if logs[0].Type != tracer.TypeComment || logs[0].Label != "starting" {
		t.Errorf("logs[0] = %+v, want the comment", logs[0])
	}

	if logs[1].Data != 1 || logs[2].Data != "two" {
		t.Errorf("Var recorded %v, %v, want one entry per value", logs[1].Data, logs[2].Data)
	}

	if logs[3].Data != "boom" {
		t.Errorf("Var(error) recorded %#v, want the error message", logs[3].Data)
	}
}

// TestWarn_Error checks Warn and Error record issues, only Error with a stack.
func TestWarn_Error(t *testing.T) {
	ctx, d := newCtx()

	tracer.Warn(ctx, "", "soft")
	tracer.Error(ctx, "db failed", errors.New("hard"))

	logs := d.Logs()
	if len(logs) != 2 {
		t.Fatalf("got %d entries, want 2", len(logs))
	}

	warn := logs[0].Data.(tracer.IssueData)
	if logs[0].Label != "WARNING" || warn.Error != "soft" || warn.Stack != "" {
		t.Errorf("warning = %q %+v, want default label, message and no stack", logs[0].Label, warn)
	}

	e := logs[1].Data.(tracer.IssueData)
	if logs[1].Type != tracer.TypeError || e.Error != "hard" || e.Stack == "" {
		t.Errorf("error = %+v, want message and a stack trace", e)
	}
}

// TestStop_PanicsWithBreakpointAfterRecording checks record-then-panic.
func TestStop_PanicsWithBreakpointAfterRecording(t *testing.T) {
	ctx, d := newCtx()

	defer func() {
		if _, ok := recover().(tracer.BreakpointSignal); !ok {
			t.Fatal("Stop should panic with BreakpointSignal when a Debugger is attached")
		}

		if logs := d.Logs(); len(logs) != 1 || logs[0].Label != "state" {
			t.Fatalf("Logs() = %+v, want the value recorded before stopping", logs)
		}
	}()

	tracer.Stop(ctx, "state", 42)
}
