package console_test

// =============================================================================
// TEST CASE OVERVIEW
// =============================================================================
//
// File: console_test.go
// Purpose: Verify the colored, case-normalized messages the console helpers print to stdout.
// Type: Unit
//
// Test functions: 4
// Total scenarios: 4
//
// #  Test Case                                  Scenario                               Expected Result
// 1  TestInfof_CapitalizesAndColorsCyan         Lower-case formatted message           Prints the message capitalized, in cyan
// 2  TestSuccessf_CapitalizesAndColorsGreen     Lower-case message                     Prints the message capitalized, in green
// 3  TestWarnf_LowercasesAfterPrefix            Capitalized message                    Prints "Warning:" then the message lower-cased
// 4  TestStepf_PrintsIconAndCapitalizedMessage  Icon and lower-case formatted message  Prints the icon and the message capitalized
//
// =============================================================================

import (
	"io"
	"os"
	"strings"
	"testing"

	"github.com/jun-develop/junkit/console"
)

// captureStdout redirects os.Stdout while fn runs and returns what was written.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe() error = %v", err)
	}

	original := os.Stdout
	os.Stdout = w
	defer func() { os.Stdout = original }()

	fn()

	_ = w.Close()
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("failed to read captured stdout: %v", err)
	}

	return string(out)
}

func TestInfof_CapitalizesAndColorsCyan(t *testing.T) {
	out := captureStdout(t, func() {
		console.Infof("starting migration for %s", "users")
	})

	if !strings.Contains(out, "Starting migration for users") {
		t.Fatalf("output = %q, want the message capitalized", out)
	}

	if !strings.Contains(out, "\033[36m") {
		t.Fatalf("output = %q, want the cyan color code", out)
	}
}

func TestSuccessf_CapitalizesAndColorsGreen(t *testing.T) {
	out := captureStdout(t, func() {
		console.Successf("done")
	})

	if !strings.Contains(out, "Done") {
		t.Fatalf("output = %q, want the message capitalized", out)
	}

	if !strings.Contains(out, "\033[32m") {
		t.Fatalf("output = %q, want the green color code", out)
	}
}

func TestWarnf_LowercasesAfterPrefix(t *testing.T) {
	out := captureStdout(t, func() {
		console.Warnf("Something looks off")
	})

	if !strings.Contains(out, "something looks off") {
		t.Fatalf("output = %q, want the message lowercased", out)
	}

	if !strings.Contains(out, "Warning:") {
		t.Fatalf("output = %q, want the \"Warning:\" prefix", out)
	}
}

func TestStepf_PrintsIconAndCapitalizedMessage(t *testing.T) {
	out := captureStdout(t, func() {
		console.Stepf("🏗️", "generating handler for %s", "product")
	})

	if !strings.Contains(out, "Generating handler for product") {
		t.Fatalf("output = %q, want the formatted, capitalized message present", out)
	}

	if !strings.Contains(out, "🏗️") {
		t.Fatalf("output = %q, want the caller-supplied icon", out)
	}
}
