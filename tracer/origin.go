package tracer

import (
	"runtime"
	"strings"
)

// tracerPackagePrefix identifies call frames belonging to this package.
const tracerPackagePrefix = "github.com/jun-develop/junkit/tracer."

// FindErrorOrigin returns the file, line and function of the first caller outside this package
// and the Go runtime, i.e. the application code that triggered an error. Called while panicking
// (e.g. from a deferred recover handler), it returns the frame that panicked instead.
func FindErrorOrigin() (string, int, string) {
	pc := make([]uintptr, 64)
	// skip=3 skips runtime.Callers, FindErrorOrigin and its direct caller (a tracer helper or a
	// deferred recover handler), whose own location is never the origin.
	n := runtime.Callers(3, pc)
	frames := runtime.CallersFrames(pc[:n])

	file, line, fn, found := "unknown", 0, "unknown", false
	for {
		frame, more := frames.Next()
		switch {
		case frame.Function == "runtime.gopanic":
			// Frames above gopanic are deferred handlers; the panic site lies below it.
			found = false
		case !found &&
			!strings.Contains(frame.File, "/runtime/") &&
			!strings.HasPrefix(frame.Function, tracerPackagePrefix):
			file, line, fn, found = frame.File, frame.Line, frame.Function, true
		}

		if !more {
			break
		}
	}

	return file, line, fn
}
