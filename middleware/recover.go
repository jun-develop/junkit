package middleware

import (
	"fmt"
	"runtime/debug"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/jun-develop/junkit/notification"
	"github.com/jun-develop/junkit/response"
	"github.com/jun-develop/junkit/tracer"
)

// RecoverOptions configures panic recovery behavior and debug bypass settings.
type RecoverOptions struct {
	// Debug selects requests whose panic goes to the tracer instead of the 500 response. Use the
	// same options as TracerDebug.
	Debug TracerDebugOptions
}

// Recover creates a Gin middleware that catches panics, logs stack traces, returns HTTP 500 and then
// queues an error report on notifier (which may be nil). tracer.BreakpointSignal panics are
// re-panicked.
//
// Usage:
//
//	router.Use(middleware.Recover(middleware.RecoverOptions{}, zapLogger, notifier, responder))
func Recover(opts RecoverOptions, logger *zap.Logger, notifier notification.Notifier, responder response.Responder) gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			r := recover()
			if r == nil {
				return
			}

			if _, ok := r.(tracer.BreakpointSignal); ok {
				panic(r)
			}

			if opts.Debug.Matches(c) {
				tracer.Error(c.Request.Context(), "PANIC RECOVERED", r)
				return
			}

			handlePanic(c, logger, notifier, responder, r)
		}()

		c.Next()
	}
}

// handlePanic logs a recovered panic, aborts with a 500 response and then reports it, so the client
// is answered before the report is built.
func handlePanic(c *gin.Context, logger *zap.Logger, notifier notification.Notifier, responder response.Responder, r any) {
	file, line, fn := tracer.FindErrorOrigin()
	stack := debug.Stack()

	logger.Error("middleware: panic recovered",
		zap.Any("error", r),
		zap.String("location", file),
		zap.Int("line", line),
		zap.String("function", fn),
		zap.ByteString("stack", stack),
	)

	responder.Error(c, response.ErrInternalServer)
	c.Abort()

	if notifier != nil {
		notifier.ReportError(c.Request.Context(), c.Request, notification.ErrorReport{
			Error:      fmt.Sprintf("%v", r),
			Location:   file,
			Line:       line,
			Function:   fn,
			Stack:      string(stack),
			StatusCode: c.Writer.Status(),
			ClientIP:   c.ClientIP(),
		})
	}
}
