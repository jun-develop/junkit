package tracer_test

import (
	"context"
	"testing"
	"time"

	"github.com/jun-develop/junkit/tracer"
)

// TestSpan_NoOpWithoutDebugger checks Span is safe to use without a Debugger.
func TestSpan_NoOpWithoutDebugger(t *testing.T) {
	// Must not panic.
	tracer.Span(context.Background(), "no debugger")()
}

// TestSpan_RecordsOnStop checks a span entry is recorded with its duration when stopped.
func TestSpan_RecordsOnStop(t *testing.T) {
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
	if e.Type != tracer.TypeSpan || e.Label != "Calculate Discount & Taxes" {
		t.Fatalf("entry = %+v, want a span with the given label", e)
	}

	if e.DurationMS < 1 {
		t.Fatalf("DurationMS = %v, want >= 1 after sleeping 1ms", e.DurationMS)
	}
}

// TestSpan_NestedSpansKeepTheirStartOffsets checks nested spans keep their own start offsets.
func TestSpan_NestedSpansKeepTheirStartOffsets(t *testing.T) {
	ctx, d := newCtx()

	stopOuter := tracer.Span(ctx, "outer")
	time.Sleep(time.Millisecond)
	stopInner := tracer.Span(ctx, "inner")
	stopInner()
	stopOuter()

	logs := d.Logs()
	inner, outer := logs[0], logs[1]
	if inner.AtMS <= outer.AtMS {
		t.Fatalf("inner.AtMS (%v) should be after outer.AtMS (%v)", inner.AtMS, outer.AtMS)
	}

	// Offsets and durations are each truncated to whole microseconds, so allow a few µs of slack.
	if outer.AtMS+outer.DurationMS < inner.AtMS+inner.DurationMS-0.005 {
		t.Fatal("outer span should end after the inner span")
	}
}

// TestMeasure checks Measure runs fn and records it as a span.
func TestMeasure(t *testing.T) {
	t.Run("records a span around fn", func(t *testing.T) {
		ctx, d := newCtx()
		called := false
		tracer.Measure(ctx, "Bcrypt Hashing", func() { called = true })

		if !called {
			t.Fatal("Measure did not run fn")
		}

		if logs := d.Logs(); len(logs) != 1 || logs[0].Label != "Bcrypt Hashing" {
			t.Fatalf("Logs() = %+v, want one span", logs)
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
