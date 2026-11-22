package tracer_test

// =============================================================================
// TEST CASE OVERVIEW
// =============================================================================
//
// File: origin_test.go
// Purpose: Verify that FindErrorOrigin reports the application call site, not tracer internals.
// Type: Unit
//
// Test functions: 2
// Total scenarios: 2
//
// #  Test Case                                         Scenario                             Expected Result
// 1  TestFindErrorOrigin_ReportsTheCallerOfItsWrapper  Called through one wrapper function  Reports this test's file, a positive line and this test function
// 2  TestFindErrorOrigin_ReportsWarnCallSite           tracer.Warn called from this test    Issue location is the exact tracer.Warn line in this test
//
// =============================================================================

import (
	"context"
	"runtime"
	"strings"
	"testing"

	"github.com/jun-develop/junkit/tracer"
)

// oneWrapperDeep calls FindErrorOrigin through one wrapper level.
func oneWrapperDeep() (string, int, string) {
	return tracer.FindErrorOrigin()
}

func TestFindErrorOrigin_ReportsTheCallerOfItsWrapper(t *testing.T) {
	file, line, fn := oneWrapperDeep()

	if !strings.HasSuffix(file, "origin_test.go") {
		t.Fatalf("file = %q, want it to point at this test file", file)
	}

	if line <= 0 {
		t.Fatalf("line = %d, want a positive line number", line)
	}

	if !strings.Contains(fn, "TestFindErrorOrigin_ReportsTheCallerOfItsWrapper") {
		t.Fatalf("function = %q, want it to mention this test function", fn)
	}
}

func TestFindErrorOrigin_ReportsWarnCallSite(t *testing.T) {
	ctx := tracer.WithContext(context.Background(), tracer.New())
	d := tracer.FromContext(ctx)

	_, _, callerLine, _ := runtime.Caller(0)
	tracer.Warn(ctx, "test warning", "boom") // must stay on the line right after runtime.Caller
	wantLine := callerLine + 1

	logs := d.Logs()
	if len(logs) != 1 {
		t.Fatalf("got %d log entries, want 1", len(logs))
	}

	data, ok := logs[0].Data.(tracer.IssueData)
	if !ok {
		t.Fatalf("log entry Data = %#v, want tracer.IssueData", logs[0].Data)
	}

	if !strings.HasSuffix(data.Location, "origin_test.go") {
		t.Fatalf("Location = %q, want it to end in origin_test.go", data.Location)
	}

	if data.Line != wantLine {
		t.Fatalf("Line = %d, want %d (the tracer.Warn call site, not one frame further up)", data.Line, wantLine)
	}

	if !strings.Contains(data.Function, "TestFindErrorOrigin_ReportsWarnCallSite") {
		t.Fatalf("Function = %q, want it to mention this test function", data.Function)
	}
}
