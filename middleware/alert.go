package middleware

import (
	"fmt"
	"html"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/jun-develop/junkit/notification"
)

// sendAlert queues an error alert titled title (trusted HTML) listing fields, name/value pairs,
// then c's method, path and the time. Values are escaped, since Telegram rejects (and so drops)
// messages with stray markup. A nil notifier sends nothing.
func sendAlert(c *gin.Context, notifier notification.Notifier, title string, fields ...string) {
	if notifier == nil {
		return
	}

	fields = append(fields, "Method", c.Request.Method, "Path", c.Request.URL.Path, "Time", time.Now().Format(time.RFC3339))

	var sb strings.Builder
	sb.WriteString(title + "\n")
	for i := 0; i+1 < len(fields); i += 2 {
		fmt.Fprintf(&sb, "\n• <b>%s:</b> <code>%s</code>", fields[i], html.EscapeString(fields[i+1]))
	}

	notifier.Send(c.Request.Context(), notification.LevelError, sb.String())
}
