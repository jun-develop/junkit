// Package sqlfmt_test is an external test package because Origin skips frames of package sqlfmt
// itself.
package sqlfmt_test

// =============================================================================
// TEST CASE OVERVIEW
// =============================================================================
//
// File: origin_test.go
// Purpose: Verify that Origin reports the caller's file and function, not sqlfmt's own frames.
// Type: Unit
//
// Test functions: 1
// Total scenarios: 1
//
// #  Test Case                     Scenario               Expected Result
// 1  TestOrigin_PointsAtTheCaller  Called from this test  Location is origin_test.go, function is this test
//
// =============================================================================

import (
	"strings"
	"testing"

	"github.com/jun-develop/junkit/sqlfmt"
)

func TestOrigin_PointsAtTheCaller(t *testing.T) {
	location, function := sqlfmt.Origin()

	if !strings.Contains(location, "origin_test.go:") {
		t.Errorf("location = %q, want this test file", location)
	}

	if !strings.HasSuffix(function, "TestOrigin_PointsAtTheCaller") {
		t.Errorf("function = %q, want this test function", function)
	}
}
