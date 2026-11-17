// Package response writes localized JSON API responses in a standard envelope and maps domain
// errors (AppError) to HTTP status codes.
package response

import (
	"fmt"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/jun-develop/junkit/logger"
	"github.com/jun-develop/junkit/pagination"
)

// Gin context keys set by middleware.Trace and read to construct APIInfo. The trace ID is not
// one of them: it is read from the request context via logger.GetTraceID.
const (
	StartTimeContextKey = "start_time"
	VersionContextKey   = "api_version"
	EnvContextKey       = "api_env"
)

// PaginatedData wraps a page of items with Pagination metadata.
type PaginatedData struct {
	Items      any                   `json:"items"`
	Pagination pagination.Pagination `json:"pagination,omitempty"`
}

// Response defines the standard JSON API response envelope.
type Response struct {
	Status  string    `json:"status"`
	Code    ErrorCode `json:"code,omitempty"`
	Message string    `json:"message,omitempty"`
	Data    any       `json:"data,omitempty"`
	Error   any       `json:"error,omitempty"`
	APIInfo *APIInfo  `json:"api_info,omitempty"`
}

// APIInfo carries request diagnostic metadata (trace ID, timestamp, latency, version).
type APIInfo struct {
	TraceID   string `json:"trace_id,omitempty" example:"eb9b3d81-869d-4b62-80b8-5780f2da2f30"`
	Timestamp string `json:"timestamp" example:"2026-01-10T12:44:05Z"`
	Latency   string `json:"latency,omitempty" example:"188.280ms"`
	Version   string `json:"version,omitempty" example:"v1.0.0"`
	Env       string `json:"env,omitempty" example:"production"`
}

// getAPIInfo constructs APIInfo from the request context and values stored in the Gin context.
func getAPIInfo(c *gin.Context) *APIInfo {
	info := &APIInfo{
		Timestamp: time.Now().Format(time.RFC3339),
	}

	if startTime, exists := c.Get(StartTimeContextKey); exists {
		if t, ok := startTime.(time.Time); ok {
			info.Latency = fmt.Sprintf("%.3fms", float64(time.Since(t).Microseconds())/1000.0)
		}
	}

	if c.Request != nil {
		info.TraceID = logger.GetTraceID(c.Request.Context())
	}

	info.Version = c.GetString(VersionContextKey)
	info.Env = c.GetString(EnvContextKey)

	return info
}
