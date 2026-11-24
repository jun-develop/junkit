// Package console prints colored status messages to stdout for CLI tools and application bootstrap.
package console

import (
	"fmt"
	"os"

	"github.com/jun-develop/junkit/stringx"
)

// ANSI escape codes used to style output.
const (
	colorReset  = "\033[0m"
	colorRed    = "\033[31m"
	colorGreen  = "\033[32m"
	colorYellow = "\033[33m"
	colorCyan   = "\033[36m"
	colorBold   = "\033[1m"
)

// Infof prints a cyan informational message to stdout.
//
// Usage:
//
//	console.Infof("starting migration for %s", dbName)
func Infof(format string, a ...any) {
	fmt.Printf(colorCyan+"⚡ "+stringx.UpperFirst(format)+colorReset+"\n", a...)
}

// Successf prints a green success message to stdout.
//
// Usage:
//
//	console.Successf("migration applied successfully")
func Successf(format string, a ...any) {
	fmt.Printf(colorGreen+" ✅ "+stringx.UpperFirst(format)+colorReset+"\n", a...)
}

// Warnf prints a yellow warning message to stdout prefixed with "Warning:".
//
// Usage:
//
//	console.Warnf("no .env file found, using defaults")
func Warnf(format string, a ...any) {
	fmt.Printf(colorYellow+"⚠️  Warning: "+stringx.LowerFirst(format)+colorReset+"\n", a...)
}

// Fatalf prints a red error message to stdout prefixed with "Error:" and exits with code 1.
//
// Usage:
//
//	console.Fatalf("failed to connect to database: %v", err)
func Fatalf(format string, a ...any) {
	fmt.Printf(colorRed+"⛔ Error: "+stringx.LowerFirst(format)+colorReset+"\n", a...)
	os.Exit(1)
}

// Stepf prints a bold message prefixed with an icon.
//
// Usage:
//
//	console.Stepf("🏗️", "generating handler for %s", featureName)
func Stepf(icon, format string, a ...any) {
	fmt.Printf(colorBold+icon+" "+stringx.UpperFirst(format)+colorReset+"\n", a...)
}
