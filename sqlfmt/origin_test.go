// Package sqlfmt_test is an external test package because Origin skips frames of package sqlfmt
// itself.
package sqlfmt_test

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
