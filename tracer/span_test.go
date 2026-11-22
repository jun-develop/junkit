package tracer_test

// =============================================================================
// TEST CASE OVERVIEW
// =============================================================================
//
// File: span_test.go
// Purpose: Verify timed spans (Span, Measure) recorded on the request Debugger.
// Type: Unit
//
// Test functions: 4
// Total scenarios: 5
//
// #  Test Case                                  Scenario                                     Expected Result
// 1  TestSpan_NoOpWithoutDebugger               Span started and stopped on a plain context  No panic
// 2  TestSpan_RecordsEntryWhenStopped           Span stopped after 1ms                       Nothing before stop; then one span entry with its label and >= 1ms
// 3  TestSpan_NestedSpansKeepTheirStartOffsets  Inner span inside an outer span              Inner starts after outer; outer ends after inner
// 4  TestMeasure_RunsFnAndRecordsSpan           Debugger attached                            Runs fn and records one span with the label
// 5  TestMeasure_RunsFnAndRecordsSpan           No Debugger                                  Still runs fn
//
// =============================================================================

import (
	"context"
	"testing"
	"time"

	"github.com/jun-develop/junkit/tracer"
)

func TestSpan_NoOpWithoutDebugger(t *testing.T) {
	// Passes if this does not panic.
	tracer.Span(context.Background(), "no debugger")()
}

func TestSpan_RecordsEntryWhenStopped(t *testing.T) {
	ctx, d := newCtx()

	stop := tracer.Span(ctx, "Calculate Discount & Taxes")
	if logs := d.Logs(); len(logs) != 0 {
		t.Fatalf("Logs() = %v before stop() is called, want no entries yet", logs)
	}

	time.Sleep(time.Millisecond)
	stop()

	logs := d.Logs()
	if len(logs) != 1 {
		t.Fatalf("Logs() returned %d entries, want 1", len(logs))
	}

	e := logs[0]
	if e.Type != tracer.TypeSpan {
		t.Errorf("entry Type = %v, want TypeSpan", e.Type)
	}

	if e.Label != "Calculate Discount & Taxes" {
		t.Errorf("entry Label = %q, want %q", e.Label, "Calculate Discount & Taxes")
	}

	if e.DurationMS < 1 {
		t.Errorf("DurationMS = %v, want >= 1 after sleeping 1ms", e.DurationMS)
	}
}

func TestSpan_NestedSpansKeepTheirStartOffsets(t *testing.T) {
	ctx, d := newCtx()

	stopOuter := tracer.Span(ctx, "outer")
	time.Sleep(time.Millisecond)
	stopInner := tracer.Span(ctx, "inner")
	stopInner()
	stopOuter()

	logs := d.Logs()
	if len(logs) != 2 {
		t.Fatalf("Logs() returned %d entries, want 2 (inner, then outer)", len(logs))
	}

	inner, outer := logs[0], logs[1]
	if inner.AtMS <= outer.AtMS {
		t.Fatalf("inner.AtMS (%v) should be after outer.AtMS (%v)", inner.AtMS, outer.AtMS)
	}

	// Offsets and durations are each truncated to whole microseconds, so allow a few µs of slack.
	if outer.AtMS+outer.DurationMS < inner.AtMS+inner.DurationMS-0.005 {
		t.Errorf("outer span ends at %vms, want at or after the inner span end %vms", outer.AtMS+outer.DurationMS, inner.AtMS+inner.DurationMS)
	}
}

func TestMeasure_RunsFnAndRecordsSpan(t *testing.T) {
	t.Run("records a span around fn", func(t *testing.T) {
		ctx, d := newCtx()
		called := false
		tracer.Measure(ctx, "Bcrypt Hashing", func() { called = true })

		if !called {
			t.Fatal("Measure did not run fn")
		}

		logs := d.Logs()
		if len(logs) != 1 {
			t.Fatalf("Logs() = %+v, want one span", logs)
		}

		if logs[0].Label != "Bcrypt Hashing" {
			t.Errorf("span Label = %q, want %q", logs[0].Label, "Bcrypt Hashing")
		}
	})

	t.Run("still runs fn without a Debugger", func(t *testing.T) {
		called := false
		tracer.Measure(context.Background(), "skipped", func() { called = true })
		if !called {
			t.Fatal("Measure must run fn even without a Debugger")
		}
	})
}
